package releaseinfo

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/openshift/hypershift/support/thirdparty/library-go/pkg/image/reference"

	imagev1 "github.com/openshift/api/image/v1"

	"github.com/coreos/stream-metadata-go/stream"
	"github.com/docker/distribution"
)

func newTestProvider(overrides map[string][]string, cache map[string]*ReleaseImage, repoSetupFn func(context.Context, string, []byte) (distribution.Repository, *reference.DockerImageReference, error)) *ProviderWithOpenShiftImageRegistryOverridesDecorator {
	return &ProviderWithOpenShiftImageRegistryOverridesDecorator{
		Delegate: &RegistryMirrorProviderDecorator{
			Delegate: &CachedProvider{
				Inner: &RegistryClientProvider{},
				Cache: cache,
			},
			RegistryOverrides: map[string]string{},
		},
		OpenShiftImageRegistryOverrides: overrides,
		repoSetupFn:                     repoSetupFn,
	}
}

func successRepoSetup(_ context.Context, imageRef string, _ []byte) (distribution.Repository, *reference.DockerImageReference, error) {
	ref, err := reference.Parse(imageRef)
	if err != nil {
		return nil, nil, err
	}
	return nil, &ref, nil
}

func TestProviderWithOpenShiftImageRegistryOverridesDecorator_Lookup(t *testing.T) {
	g := NewWithT(t)

	mirroredReleaseImage := "quay.io/openshift-release-dev/ocp-release:4.16.13-x86_64"
	canonicalReleaseImage := "canonical-release-image"
	releaseImage := &ReleaseImage{
		ImageStream:    &imagev1.ImageStream{},
		StreamMetadata: &stream.Stream{},
	}

	provider := newTestProvider(
		map[string][]string{canonicalReleaseImage: {mirroredReleaseImage}},
		map[string]*ReleaseImage{mirroredReleaseImage: releaseImage},
		successRepoSetup,
	)

	pullSecret := []byte(`{"auths":{}}`)
	_, err := provider.Lookup(t.Context(), canonicalReleaseImage, pullSecret)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(provider.GetMirroredReleaseImage()).To(Equal(mirroredReleaseImage))
}

func TestProviderWithOpenShiftImageRegistryOverridesDecorator_LookupWithNilRepoSetupFn(t *testing.T) {
	g := NewWithT(t)

	directImage := "quay.io/openshift-release-dev/ocp-release:4.16.13-x86_64"
	releaseImage := &ReleaseImage{
		ImageStream:    &imagev1.ImageStream{},
		StreamMetadata: &stream.Stream{},
	}

	provider := newTestProvider(
		map[string][]string{"no-match-source": {"no-match-mirror"}},
		map[string]*ReleaseImage{directImage: releaseImage},
		nil, // nil exercises default registryclient.GetRepoSetup but no override matches so it's never called
	)

	pullSecret := []byte(`{"auths":{}}`)
	result, err := provider.Lookup(t.Context(), directImage, pullSecret)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result).To(Equal(releaseImage))
	g.Expect(provider.GetMirroredReleaseImage()).To(BeEmpty())
}

// When-it-should style: when repoSetup blocks past the 15s timeout, it should return promptly.
func TestProviderWithOpenShiftImageRegistryOverridesDecorator_WhenMirrorProbeTimesOut_ItShouldFallBackToOriginal(t *testing.T) {
	t.Parallel() // this test sleeps 15s; parallelize so it doesn't serialize the whole package
	g := NewWithT(t)

	mirroredImage := "mirror.example.com/ocp-release:4.16.13-x86_64"
	canonicalImage := "canonical-release-image"
	releaseImage := &ReleaseImage{
		ImageStream:    &imagev1.ImageStream{},
		StreamMetadata: &stream.Stream{},
	}

	// repoSetupFn blocks until its context is cancelled (simulates an unreachable mirror).
	var cancelCallCount int
	blockingRepoSetup := func(ctx context.Context, _ string, _ []byte) (distribution.Repository, *reference.DockerImageReference, error) {
		<-ctx.Done()
		cancelCallCount++
		return nil, nil, ctx.Err()
	}

	provider := newTestProvider(
		map[string][]string{canonicalImage: {mirroredImage}},
		map[string]*ReleaseImage{
			mirroredImage: releaseImage,
			canonicalImage: {
				ImageStream:    &imagev1.ImageStream{},
				StreamMetadata: &stream.Stream{},
			},
		},
		blockingRepoSetup,
	)

	start := time.Now()
	_, err := provider.Lookup(t.Context(), canonicalImage, []byte(`{"auths":{}}`))
	elapsed := time.Since(start)

	// Should return (falling back to canonical) after the 15s probe timeout, not hang.
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(elapsed).To(BeNumerically(">=", 15*time.Second))
	g.Expect(elapsed).To(BeNumerically("<", 20*time.Second))
	// cancel() must have been called for the blocked probe context.
	g.Expect(cancelCallCount).To(Equal(1))
	// Fell back to original — mirrored image is empty.
	g.Expect(provider.GetMirroredReleaseImage()).To(BeEmpty())
}

// When the cache is enabled, a second Lookup() for the same image should not call repoSetupFn again.
func TestProviderWithOpenShiftImageRegistryOverridesDecorator_WhenCacheEnabled_ItShouldServeSubsequentLookupsFromCache(t *testing.T) {
	g := NewWithT(t)

	mirroredImage := "mirror.example.com/ocp-release:4.16.13-x86_64"
	canonicalImage := "canonical-release-image"
	releaseImage := &ReleaseImage{
		ImageStream:    &imagev1.ImageStream{},
		StreamMetadata: &stream.Stream{},
	}

	repoSetupCallCount := 0
	countingRepoSetup := func(ctx context.Context, imageRef string, _ []byte) (distribution.Repository, *reference.DockerImageReference, error) {
		repoSetupCallCount++
		ref, err := reference.Parse(imageRef)
		if err != nil {
			return nil, nil, err
		}
		return nil, &ref, nil
	}

	provider := newTestProvider(
		map[string][]string{canonicalImage: {mirroredImage}},
		map[string]*ReleaseImage{mirroredImage: releaseImage},
		countingRepoSetup,
	)
	provider.ResultCacheEnabled = true

	pullSecret := []byte(`{"auths":{}}`)

	_, err := provider.Lookup(t.Context(), canonicalImage, pullSecret)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(repoSetupCallCount).To(Equal(1))

	// Second call: should be a cache hit — repoSetup must not be called again.
	_, err = provider.Lookup(t.Context(), canonicalImage, pullSecret)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(repoSetupCallCount).To(Equal(1))
	g.Expect(provider.GetMirroredReleaseImage()).To(Equal(mirroredImage))
}

// When the cache is enabled, concurrent Lookup() calls for the same key should not
// all perform the full I/O (thundering herd).
func TestProviderWithOpenShiftImageRegistryOverridesDecorator_WhenCacheEnabled_ItShouldPreventThunderingHerd(t *testing.T) {
	g := NewWithT(t)

	mirroredImage := "mirror.example.com/ocp-release:4.16.13-x86_64"
	canonicalImage := "canonical-release-image"
	releaseImage := &ReleaseImage{
		ImageStream:    &imagev1.ImageStream{},
		StreamMetadata: &stream.Stream{},
	}

	var mu sync.Mutex
	repoSetupCallCount := 0
	slowRepoSetup := func(ctx context.Context, imageRef string, _ []byte) (distribution.Repository, *reference.DockerImageReference, error) {
		time.Sleep(50 * time.Millisecond) // simulate slow probe
		mu.Lock()
		repoSetupCallCount++
		mu.Unlock()
		ref, err := reference.Parse(imageRef)
		if err != nil {
			return nil, nil, err
		}
		return nil, &ref, nil
	}

	provider := newTestProvider(
		map[string][]string{canonicalImage: {mirroredImage}},
		map[string]*ReleaseImage{mirroredImage: releaseImage},
		slowRepoSetup,
	)
	provider.ResultCacheEnabled = true

	pullSecret := []byte(`{"auths":{}}`)
	errs := make([]error, 10)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Capture error rather than asserting inside goroutine:
			// t.Fatalf called from a non-test goroutine panics in Go 1.16+.
			_, errs[i] = provider.Lookup(t.Context(), canonicalImage, pullSecret)
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		g.Expect(err).ToNot(HaveOccurred())
	}
	// With double-check locking, only one goroutine should have done the live probe.
	g.Expect(repoSetupCallCount).To(Equal(1))
}

// When the cache entry is expired, GetMirroredReleaseImage should reflect the fresh result.
func TestProviderWithOpenShiftImageRegistryOverridesDecorator_WhenCacheExpires_ItShouldRefetch(t *testing.T) {
	g := NewWithT(t)

	mirroredImage := "mirror.example.com/ocp-release:4.16.13-x86_64"
	canonicalImage := "canonical-release-image"
	releaseImage := &ReleaseImage{
		ImageStream:    &imagev1.ImageStream{},
		StreamMetadata: &stream.Stream{},
	}

	repoSetupCallCount := 0
	countingRepoSetup := func(ctx context.Context, imageRef string, _ []byte) (distribution.Repository, *reference.DockerImageReference, error) {
		repoSetupCallCount++
		ref, err := reference.Parse(imageRef)
		if err != nil {
			return nil, nil, err
		}
		return nil, &ref, nil
	}

	provider := newTestProvider(
		map[string][]string{canonicalImage: {mirroredImage}},
		map[string]*ReleaseImage{mirroredImage: releaseImage},
		countingRepoSetup,
	)
	provider.ResultCacheEnabled = true

	pullSecret := []byte(`{"auths":{}}`)

	// Seed the cache with an already-expired entry.
	provider.resultCache.Store(lookupCacheKey(canonicalImage, pullSecret), &lookupCacheEntry{
		releaseImage:  releaseImage,
		mirroredImage: mirroredImage,
		cachedAt:      time.Now().Add(-lookupCacheTTL - time.Second),
	})

	_, err := provider.Lookup(t.Context(), canonicalImage, pullSecret)
	g.Expect(err).ToNot(HaveOccurred())
	// Expired entry should have triggered a fresh probe.
	g.Expect(repoSetupCallCount).To(Equal(1))
}

// When repoSetup returns an error, the result should NOT be cached.
func TestProviderWithOpenShiftImageRegistryOverridesDecorator_WhenMirrorUnavailable_ItShouldNotCache(t *testing.T) {
	g := NewWithT(t)

	mirroredImage := "mirror.example.com/ocp-release:4.16.13-x86_64"
	canonicalImage := "canonical-release-image"
	releaseImage := &ReleaseImage{
		ImageStream:    &imagev1.ImageStream{},
		StreamMetadata: &stream.Stream{},
	}

	failingRepoSetup := func(_ context.Context, _ string, _ []byte) (distribution.Repository, *reference.DockerImageReference, error) {
		return nil, nil, errors.New("mirror unavailable")
	}

	provider := newTestProvider(
		map[string][]string{canonicalImage: {mirroredImage}},
		map[string]*ReleaseImage{
			mirroredImage:  releaseImage,
			canonicalImage: releaseImage,
		},
		failingRepoSetup,
	)
	provider.ResultCacheEnabled = true

	pullSecret := []byte(`{"auths":{}}`)
	_, err := provider.Lookup(t.Context(), canonicalImage, pullSecret)
	g.Expect(err).ToNot(HaveOccurred())

	// Nothing should be in the cache for the failed mirror path.
	_, cached := provider.resultCache.Load(lookupCacheKey(canonicalImage, pullSecret))
	g.Expect(cached).To(BeFalse())
}
