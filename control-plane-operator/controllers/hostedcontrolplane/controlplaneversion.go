package hostedcontrolplane

import (
	"strings"

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
		result.History = pruneHistory(result.History)
		return result
	}

	// No desired change — check if all components completed rollout.
	if allComponentsRolloutComplete(components) &&
		len(result.History) > 0 &&
		result.History[0].State == configv1.PartialUpdate {
		result.History[0].State = configv1.CompletedUpdate
		result.History[0].CompletionTime = &now
	}

	result.History = pruneHistory(result.History)
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

// Pruning constants ported from CVO (pkg/cvo/status_history.go).
const (
	maxHistory           = 100
	maxFinalEntryIndex   = 4
	mostImportantWeight  = 1000.0
	interestingWeight    = 30.0
	partialMinorWeight   = 20.0
	partialZStreamWeight = -20.0
	sliceIndexWeight     = -1.01
)

// pruneHistory caps the history at maxHistory entries by repeatedly removing
// the lowest-ranked entry using CVO's weighted ranking algorithm.
func pruneHistory(history []hyperv1.ControlPlaneUpdateHistory) []hyperv1.ControlPlaneUpdateHistory {
	for len(history) > maxHistory {
		history = pruneLowestRanked(history)
	}
	return history
}

// pruneLowestRanked removes the single entry with the lowest computed rank.
func pruneLowestRanked(history []hyperv1.ControlPlaneUpdateHistory) []hyperv1.ControlPlaneUpdateHistory {
	n := len(history)

	// Find the most recently completed entry (first Completed scanning from index 0).
	mostRecentCompletedIdx := -1
	for i := range history {
		if history[i].State == configv1.CompletedUpdate {
			mostRecentCompletedIdx = i
			break
		}
	}

	// Find first and last Completed entry per minor version.
	type minorBounds struct{ first, last int }
	minors := make(map[string]*minorBounds)
	for i := range history {
		if history[i].State != configv1.CompletedUpdate {
			continue
		}
		minor := extractMinor(history[i].Version)
		if minor == "" {
			continue
		}
		if b, ok := minors[minor]; ok {
			if i > b.last {
				b.last = i
			}
		} else {
			minors[minor] = &minorBounds{first: i, last: i}
		}
	}

	// Compute ranks and find the entry with the lowest rank.
	lowestIdx := 0
	var lowestWeight float64
	for i := range history {
		var w float64

		protected := i <= maxFinalEntryIndex || i == n-1 || i == mostRecentCompletedIdx
		if protected {
			w = mostImportantWeight
		} else if history[i].State == configv1.CompletedUpdate {
			minor := extractMinor(history[i].Version)
			if b, ok := minors[minor]; ok && (i == b.first || i == b.last) {
				w = interestingWeight
			}
		} else if history[i].State == configv1.PartialUpdate {
			var prevMinor, nextMinor string
			if i > 0 {
				prevMinor = extractMinor(history[i-1].Version)
			}
			if i < n-1 {
				nextMinor = extractMinor(history[i+1].Version)
			}
			if prevMinor != "" && nextMinor != "" && prevMinor != nextMinor {
				w = partialMinorWeight
			} else {
				w = partialZStreamWeight
			}
		}

		// Index penalty: entries closer to the start (newest) receive more penalty,
		// matching CVO's behavior of thinning out middle history while preserving
		// the endpoints. The fractional component breaks ties deterministically.
		w += sliceIndexWeight * float64(n-1-i)

		if i == 0 || w < lowestWeight {
			lowestWeight = w
			lowestIdx = i
		}
	}

	result := make([]hyperv1.ControlPlaneUpdateHistory, 0, n-1)
	result = append(result, history[:lowestIdx]...)
	result = append(result, history[lowestIdx+1:]...)
	return result
}

// extractMinor returns the major.minor portion of a semver string (e.g., "4.17" from "4.17.2").
func extractMinor(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}
