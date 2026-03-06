package hostedcontrolplane

import (
	configv1 "github.com/openshift/api/config/v1"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
)

// reconcileControlPlaneVersion aggregates ControlPlaneComponent status into
// hcp.Status.ControlPlaneVersion, implementing CVO-ported mergeEqualVersions
// semantics, version transition logic (Partial/Completed), image-only change
// detection, first-population behavior, and observedGeneration updates.
func reconcileControlPlaneVersion(
	hcp *hyperv1.HostedControlPlane,
	components []hyperv1.ControlPlaneComponent,
	clk clock.Clock,
) *hyperv1.ControlPlaneVersionStatus {
	desiredImage := controlPlaneDesiredImage(hcp)
	version := mergeEqualVersions(components)
	now := metav1.NewTime(clk.Now())

	existing := hcp.Status.ControlPlaneVersion
	if existing == nil {
		// First population: create initial Partial entry.
		return &hyperv1.ControlPlaneVersionStatus{
			Desired: configv1.Release{Version: version, Image: desiredImage},
			History: []hyperv1.ControlPlaneUpdateHistory{
				{
					State:       configv1.PartialUpdate,
					StartedTime: now,
					Version:     version,
					Image:       desiredImage,
				},
			},
			ObservedGeneration: hcp.Generation,
		}
	}

	result := existing.DeepCopy()
	result.Desired = configv1.Release{Version: version, Image: desiredImage}
	result.ObservedGeneration = hcp.Generation

	// Detect desired release change (version string or image).
	if existing.Desired.Image != desiredImage {
		// Stamp CompletionTime on superseded Partial entry before prepending.
		if len(result.History) > 0 && result.History[0].CompletionTime == nil {
			result.History[0].CompletionTime = &now
		}
		// Prepend new Partial entry for the new desired release.
		entry := hyperv1.ControlPlaneUpdateHistory{
			State:       configv1.PartialUpdate,
			StartedTime: now,
			Version:     version,
			Image:       desiredImage,
		}
		result.History = append([]hyperv1.ControlPlaneUpdateHistory{entry}, result.History...)
		return result
	}

	// No desired change — check if all components completed rollout.
	if allComponentsRolloutComplete(components) &&
		len(result.History) > 0 &&
		result.History[0].State == configv1.PartialUpdate {
		result.History[0].State = configv1.CompletedUpdate
		result.History[0].CompletionTime = &now
	}

	return result
}

// controlPlaneDesiredImage returns the release image that the control plane
// is reconciling towards, preferring ControlPlaneReleaseImage when set.
func controlPlaneDesiredImage(hcp *hyperv1.HostedControlPlane) string {
	if hcp.Spec.ControlPlaneReleaseImage != nil {
		return *hcp.Spec.ControlPlaneReleaseImage
	}
	return hcp.Spec.ReleaseImage
}

// allComponentsRolloutComplete returns true when every component has the
// RolloutComplete condition set to True. Returns false when there are no
// components (nothing to complete).
func allComponentsRolloutComplete(components []hyperv1.ControlPlaneComponent) bool {
	if len(components) == 0 {
		return false
	}
	for i := range components {
		cond := meta.FindStatusCondition(components[i].Status.Conditions, string(hyperv1.ControlPlaneComponentRolloutComplete))
		if cond == nil || cond.Status != metav1.ConditionTrue {
			return false
		}
	}
	return true
}

// mergeEqualVersions returns the most-reported version string across all
// components, implementing CVO-ported semantics. Returns "" when there are
// no components or no versions reported.
func mergeEqualVersions(components []hyperv1.ControlPlaneComponent) string {
	counts := make(map[string]int)
	for _, c := range components {
		if c.Status.Version != "" {
			counts[c.Status.Version]++
		}
	}
	var best string
	var bestCount int
	for v, count := range counts {
		if count > bestCount || (count == bestCount && v > best) {
			best = v
			bestCount = count
		}
	}
	return best
}
