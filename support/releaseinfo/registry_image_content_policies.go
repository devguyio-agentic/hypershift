package releaseinfo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openshift/hypershift/support/releaseinfo/registryclient"
	"github.com/openshift/hypershift/support/thirdparty/library-go/pkg/image/reference"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/docker/distribution"
)

const lookupCacheTTL = 5 * time.Minute

// lookupCacheEntry holds a cached Lookup() result.
type lookupCacheEntry struct {
	releaseImage  *ReleaseImage
	mirroredImage string
	cachedAt      time.Time
}

var _ ProviderWithOpenShiftImageRegistryOverrides = (*ProviderWithOpenShiftImageRegistryOverridesDecorator)(nil)

type ProviderWithOpenShiftImageRegistryOverridesDecorator struct {
	Delegate                        ProviderWithRegistryOverrides
	OpenShiftImageRegistryOverrides map[string][]string

	// mirroredReleaseImage is the mirror URL chosen by the last successful Lookup().
	// It is accessed concurrently so we use atomic.Pointer[string] to avoid data races.
	// The zero value (nil pointer) means no mirror was chosen; Load() returns "" in that case.
	mirroredReleaseImage atomic.Pointer[string]

	// repoSetupFn is an injectable function for verifying mirror availability.
	// When nil, defaults to registryclient.GetRepoSetup.
	repoSetupFn func(ctx context.Context, imageRef string, pullSecret []byte) (distribution.Repository, *reference.DockerImageReference, error)

	lock sync.Mutex

	// ResultCacheEnabled enables Lookup() result caching. Set once at construction
	// before the struct is shared across goroutines; never written again.
	// Safe for concurrent reads without a mutex (Go happens-before on goroutine creation).
	ResultCacheEnabled bool
	// resultCache stores *lookupCacheEntry values keyed by lookupCacheKey().
	// sync.Map is safe for concurrent reads; expired entries are lazily evicted in loadCacheEntry.
	resultCache sync.Map
}

func (p *ProviderWithOpenShiftImageRegistryOverridesDecorator) Lookup(ctx context.Context, image string, pullSecret []byte) (*ReleaseImage, error) {
	// Fast path: return cached result without acquiring p.lock.
	if p.ResultCacheEnabled {
		if entry := p.loadCacheEntry(image, pullSecret); entry != nil {
			p.storeMirroredReleaseImage(entry.mirroredImage)
			return entry.releaseImage, nil
		}
	}

	// Lock ordering: p.lock → RegistryMirrorProviderDecorator.lock → CachedProvider.mu.
	// p.Delegate.Lookup() acquires downstream locks while p.lock is held.
	// Never acquire those downstream locks before p.lock from any other code path.
	p.lock.Lock()
	defer p.lock.Unlock()

	// Double-check inside lock to prevent thundering herd: two goroutines
	// simultaneously missing the cache would both do the full I/O otherwise.
	if p.ResultCacheEnabled {
		if entry := p.loadCacheEntry(image, pullSecret); entry != nil {
			p.storeMirroredReleaseImage(entry.mirroredImage)
			return entry.releaseImage, nil
		}
	}

	repoSetup := p.repoSetupFn
	if repoSetup == nil {
		repoSetup = registryclient.GetRepoSetup
	}

	logger := ctrl.LoggerFrom(ctx)

	for registrySource, registryDest := range p.OpenShiftImageRegistryOverrides {
		if strings.Contains(image, registrySource) {
			for _, registryReplacement := range registryDest {
				replacedImage := strings.Replace(image, registrySource, registryReplacement, 1)

				// Attempt to lookup image with mirror registry destination
				releaseImage, err := p.Delegate.Lookup(ctx, replacedImage, pullSecret)
				if releaseImage != nil {
					// Verify mirror availability with a bounded timeout — mirrors in remote
					// regions can otherwise stall this call (and hold p.lock) indefinitely.
					probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
					_, _, err = repoSetup(probeCtx, replacedImage, pullSecret)
					cancel()
					if err == nil {
						p.storeMirroredReleaseImage(replacedImage)
						if p.ResultCacheEnabled {
							p.storeCacheEntry(image, pullSecret, releaseImage, replacedImage)
						}
						return releaseImage, nil
					}
					logger.Info("WARNING: The current mirrors image is unavailable, continue Scanning multiple mirrors", "error", err.Error(), "mirror image", image)
					continue
				}

				logger.Error(err, "Failed to look up release image using registry mirror", "registry mirror", registryReplacement)
			}
		}
	}

	// Reset mirrored release image when falling back to original.
	// Do not cache the fallback result: if mirrors were tried but temporarily
	// unavailable, we want subsequent calls to retry them rather than serving
	// a stale "no mirror" result for the full TTL window.
	p.storeMirroredReleaseImage("")
	return p.Delegate.Lookup(ctx, image, pullSecret)
}

func (p *ProviderWithOpenShiftImageRegistryOverridesDecorator) GetRegistryOverrides() map[string]string {
	return p.Delegate.GetRegistryOverrides()
}

func (p *ProviderWithOpenShiftImageRegistryOverridesDecorator) GetOpenShiftImageRegistryOverrides() map[string][]string {
	return p.OpenShiftImageRegistryOverrides
}

func (p *ProviderWithOpenShiftImageRegistryOverridesDecorator) GetMirroredReleaseImage() string {
	if s := p.mirroredReleaseImage.Load(); s != nil {
		return *s
	}
	return ""
}

func (p *ProviderWithOpenShiftImageRegistryOverridesDecorator) storeMirroredReleaseImage(s string) {
	p.mirroredReleaseImage.Store(&s)
}

// lookupCacheKey returns a cache key combining the image reference and a hash of
// the pull secret. Mirrors the pattern used by generateCacheKey in support/util/imagemetadata.go.
func lookupCacheKey(image string, pullSecret []byte) string {
	h := sha256.Sum256(pullSecret)
	return fmt.Sprintf("%s|%x", image, h[:8])
}

func (p *ProviderWithOpenShiftImageRegistryOverridesDecorator) loadCacheEntry(image string, pullSecret []byte) *lookupCacheEntry {
	key := lookupCacheKey(image, pullSecret)
	v, ok := p.resultCache.Load(key)
	if !ok {
		return nil
	}
	entry := v.(*lookupCacheEntry)
	if time.Since(entry.cachedAt) >= lookupCacheTTL {
		// Lazily evict the expired entry so the map does not grow unbounded.
		// Two goroutines may both evict the same expired key concurrently (one on
		// the fast path outside p.lock, one inside). sync.Map.Delete is safe for
		// concurrent calls; the second Delete is a no-op.
		p.resultCache.Delete(key)
		return nil
	}
	return entry
}

func (p *ProviderWithOpenShiftImageRegistryOverridesDecorator) storeCacheEntry(image string, pullSecret []byte, releaseImage *ReleaseImage, mirroredImage string) {
	key := lookupCacheKey(image, pullSecret)
	p.resultCache.Store(key, &lookupCacheEntry{
		releaseImage:  releaseImage,
		mirroredImage: mirroredImage,
		cachedAt:      time.Now(),
	})
}
