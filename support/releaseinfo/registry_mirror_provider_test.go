package releaseinfo

import (
	"context"
	"fmt"
	"sync"
	"testing"

	. "github.com/onsi/gomega"

	imagev1 "github.com/openshift/api/image/v1"
	corev1 "k8s.io/api/core/v1"
)

type fakeProvider struct {
	lookupImage string
	result      *ReleaseImage
	err         error
}

func (f *fakeProvider) Lookup(_ context.Context, image string, _ []byte) (*ReleaseImage, error) {
	f.lookupImage = image
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func newReleaseImage(tagImages ...string) *ReleaseImage {
	tags := make([]imagev1.TagReference, len(tagImages))
	for i, img := range tagImages {
		tags[i] = imagev1.TagReference{
			Name: fmt.Sprintf("component-%d", i),
			From: &corev1.ObjectReference{Name: img},
		}
	}
	return &ReleaseImage{
		ImageStream: &imagev1.ImageStream{
			Spec: imagev1.ImageStreamSpec{Tags: tags},
		},
		StreamMetadata: &CoreOSStreamMetadata{},
	}
}

func TestRegistryMirrorProviderDecorator_Lookup(t *testing.T) {
	t.Run("When a matching registry override is configured it should apply override to the delegate lookup image", func(t *testing.T) {
		g := NewWithT(t)
		delegate := &fakeProvider{
			result: newReleaseImage("quay.io/openshift-release-dev/component@sha256:aaa"),
		}
		provider := &RegistryMirrorProviderDecorator{
			Delegate: delegate,
			RegistryOverrides: map[string]string{
				"quay.io/openshift-release-dev/ocp-release-nightly": "mirror.example.com/openshift-release-dev/ocp-release-nightly",
			},
			lock: sync.Mutex{},
		}

		image := "quay.io/openshift-release-dev/ocp-release-nightly@sha256:abc123"
		_, err := provider.Lookup(context.Background(), image, []byte("{}"))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(delegate.lookupImage).To(Equal("mirror.example.com/openshift-release-dev/ocp-release-nightly@sha256:abc123"))
	})

	t.Run("When no matching registry override exists it should pass the original image to the delegate", func(t *testing.T) {
		g := NewWithT(t)
		delegate := &fakeProvider{
			result: newReleaseImage(),
		}
		provider := &RegistryMirrorProviderDecorator{
			Delegate: delegate,
			RegistryOverrides: map[string]string{
				"quay.io/openshift-release-dev/ocp-release-nightly": "mirror.example.com/openshift-release-dev/ocp-release-nightly",
			},
			lock: sync.Mutex{},
		}

		image := "quay.io/openshift-release-dev/ocp-release@sha256:def456"
		_, err := provider.Lookup(context.Background(), image, []byte("{}"))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(delegate.lookupImage).To(Equal(image))
	})

	t.Run("When registry overrides are empty it should pass the original image to the delegate", func(t *testing.T) {
		g := NewWithT(t)
		delegate := &fakeProvider{
			result: newReleaseImage(),
		}
		provider := &RegistryMirrorProviderDecorator{
			Delegate:          delegate,
			RegistryOverrides: map[string]string{},
			lock:              sync.Mutex{},
		}

		image := "quay.io/openshift-release-dev/ocp-release-nightly@sha256:abc123"
		_, err := provider.Lookup(context.Background(), image, []byte("{}"))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(delegate.lookupImage).To(Equal(image))
	})

	t.Run("When registry overrides are configured it should still apply overrides to component image tags", func(t *testing.T) {
		g := NewWithT(t)
		delegate := &fakeProvider{
			result: newReleaseImage(
				"quay.io/openshift-release-dev/component-a@sha256:aaa",
				"quay.io/openshift-release-dev/component-b@sha256:bbb",
			),
		}
		provider := &RegistryMirrorProviderDecorator{
			Delegate: delegate,
			RegistryOverrides: map[string]string{
				"quay.io/openshift-release-dev": "mirror.example.com/openshift-release-dev",
			},
			lock: sync.Mutex{},
		}

		image := "quay.io/openshift-release-dev/ocp-release-nightly@sha256:abc123"
		result, err := provider.Lookup(context.Background(), image, []byte("{}"))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.ImageStream.Spec.Tags[0].From.Name).To(Equal("mirror.example.com/openshift-release-dev/component-a@sha256:aaa"))
		g.Expect(result.ImageStream.Spec.Tags[1].From.Name).To(Equal("mirror.example.com/openshift-release-dev/component-b@sha256:bbb"))
	})

	t.Run("When the delegate returns an error it should propagate the error", func(t *testing.T) {
		g := NewWithT(t)
		delegate := &fakeProvider{
			err: fmt.Errorf("unauthorized: access denied"),
		}
		provider := &RegistryMirrorProviderDecorator{
			Delegate: delegate,
			RegistryOverrides: map[string]string{
				"quay.io": "mirror.example.com",
			},
			lock: sync.Mutex{},
		}

		_, err := provider.Lookup(context.Background(), "quay.io/image@sha256:abc", []byte("{}"))
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("unauthorized"))
	})

	t.Run("When multiple non-overlapping overrides are configured it should apply all matching overrides", func(t *testing.T) {
		g := NewWithT(t)
		delegate := &fakeProvider{
			result: newReleaseImage(
				"quay.io/openshift-release-dev/component@sha256:aaa",
				"registry.redhat.io/other-component@sha256:bbb",
			),
		}
		provider := &RegistryMirrorProviderDecorator{
			Delegate: delegate,
			RegistryOverrides: map[string]string{
				"quay.io":             "mirror.example.com",
				"registry.redhat.io":  "mirror.example.com",
			},
			lock: sync.Mutex{},
		}

		image := "quay.io/openshift-release-dev/ocp-release-nightly@sha256:abc123"
		result, err := provider.Lookup(context.Background(), image, []byte("{}"))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(delegate.lookupImage).To(Equal("mirror.example.com/openshift-release-dev/ocp-release-nightly@sha256:abc123"))
		g.Expect(result.ImageStream.Spec.Tags[0].From.Name).To(Equal("mirror.example.com/openshift-release-dev/component@sha256:aaa"))
		g.Expect(result.ImageStream.Spec.Tags[1].From.Name).To(Equal("mirror.example.com/other-component@sha256:bbb"))
	})

	t.Run("When the image digest is present it should preserve the digest after override", func(t *testing.T) {
		g := NewWithT(t)
		delegate := &fakeProvider{
			result: newReleaseImage(),
		}
		provider := &RegistryMirrorProviderDecorator{
			Delegate: delegate,
			RegistryOverrides: map[string]string{
				"quay.io/openshift-release-dev": "acr.azurecr.io/openshift-release-dev",
			},
			lock: sync.Mutex{},
		}

		digest := "sha256:5a93a121eca8a32cff7155717c822a85214edc1dca0542fcbc4dc87e7c0a1aa0"
		image := "quay.io/openshift-release-dev/ocp-release-nightly@" + digest
		_, err := provider.Lookup(context.Background(), image, []byte("{}"))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(delegate.lookupImage).To(Equal("acr.azurecr.io/openshift-release-dev/ocp-release-nightly@" + digest))
	})
}
