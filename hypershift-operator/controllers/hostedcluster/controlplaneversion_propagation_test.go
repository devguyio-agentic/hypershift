package hostedcluster

import (
	"testing"
	"time"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"

	configv1 "github.com/openshift/api/config/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestControlPlaneVersionPropagation_WhenHCPHasVersion_ItShouldDeepCopyToHC
// verifies AC1: When hcp.Status.ControlPlaneVersion is populated, the HC
// controller propagates a deep copy to hcluster.Status.ControlPlaneVersion.
func TestControlPlaneVersionPropagation_WhenHCPHasVersion_ItShouldDeepCopyToHC(t *testing.T) {
	now := metav1.NewTime(time.Now())
	completionTime := metav1.NewTime(now.Add(10 * time.Minute))

	hcp := &hyperv1.HostedControlPlane{
		Status: hyperv1.HostedControlPlaneStatus{
			ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
				Desired: configv1.Release{
					Version: "4.17.0",
					Image:   "quay.io/openshift-release-dev/ocp-release:4.17.0",
				},
				History: []hyperv1.ControlPlaneUpdateHistory{
					{
						State:          configv1.CompletedUpdate,
						StartedTime:    now,
						CompletionTime: &completionTime,
						Version:        "4.17.0",
						Image:          "quay.io/openshift-release-dev/ocp-release:4.17.0",
					},
				},
				ObservedGeneration: 5,
			},
		},
	}

	hcluster := &hyperv1.HostedCluster{
		Status: hyperv1.HostedClusterStatus{},
	}

	propagateControlPlaneVersion(hcluster, hcp)

	if hcluster.Status.ControlPlaneVersion == nil {
		t.Fatal("expected ControlPlaneVersion to be set on HC, got nil")
	}
	if hcluster.Status.ControlPlaneVersion.Desired.Version != "4.17.0" {
		t.Errorf("expected Desired.Version 4.17.0, got %s", hcluster.Status.ControlPlaneVersion.Desired.Version)
	}
	if hcluster.Status.ControlPlaneVersion.Desired.Image != "quay.io/openshift-release-dev/ocp-release:4.17.0" {
		t.Errorf("expected Desired.Image quay.io/openshift-release-dev/ocp-release:4.17.0, got %s", hcluster.Status.ControlPlaneVersion.Desired.Image)
	}
	if len(hcluster.Status.ControlPlaneVersion.History) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(hcluster.Status.ControlPlaneVersion.History))
	}
	if hcluster.Status.ControlPlaneVersion.History[0].State != configv1.CompletedUpdate {
		t.Errorf("expected history state Completed, got %s", hcluster.Status.ControlPlaneVersion.History[0].State)
	}
	if hcluster.Status.ControlPlaneVersion.ObservedGeneration != 5 {
		t.Errorf("expected ObservedGeneration 5, got %d", hcluster.Status.ControlPlaneVersion.ObservedGeneration)
	}
}

// TestControlPlaneVersionPropagation_WhenHCPVersionIsNil_ItShouldSetHCVersionNil
// verifies AC12: When hcp.Status.ControlPlaneVersion is nil (older CPO that
// doesn't set the field), hcluster.Status.ControlPlaneVersion must be nil.
func TestControlPlaneVersionPropagation_WhenHCPVersionIsNil_ItShouldSetHCVersionNil(t *testing.T) {
	hcp := &hyperv1.HostedControlPlane{
		Status: hyperv1.HostedControlPlaneStatus{
			ControlPlaneVersion: nil,
		},
	}

	hcluster := &hyperv1.HostedCluster{
		Status: hyperv1.HostedClusterStatus{
			// Pre-existing value to verify it gets cleared
			ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
				Desired: configv1.Release{Version: "4.16.0"},
			},
		},
	}

	propagateControlPlaneVersion(hcluster, hcp)

	if hcluster.Status.ControlPlaneVersion != nil {
		t.Errorf("expected ControlPlaneVersion to be nil, got %+v", hcluster.Status.ControlPlaneVersion)
	}
}

// TestControlPlaneVersionPropagation_WhenHCPIsNil_ItShouldNotPanic
// verifies version skew safety: When the HCP object itself is nil
// (e.g., not yet created), the propagation must not panic and must
// leave the existing HC status unchanged.
func TestControlPlaneVersionPropagation_WhenHCPIsNil_ItShouldNotPanic(t *testing.T) {
	hcluster := &hyperv1.HostedCluster{
		Status: hyperv1.HostedClusterStatus{
			ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
				Desired: configv1.Release{Version: "4.16.0"},
			},
		},
	}

	propagateControlPlaneVersion(hcluster, nil)

	if hcluster.Status.ControlPlaneVersion == nil {
		t.Fatal("expected existing ControlPlaneVersion to be preserved, got nil")
	}
	if hcluster.Status.ControlPlaneVersion.Desired.Version != "4.16.0" {
		t.Errorf("expected preserved version 4.16.0, got %s", hcluster.Status.ControlPlaneVersion.Desired.Version)
	}
}

// TestControlPlaneVersionPropagation_WhenVersionExists_ItShouldPreserveExistingVersionField
// verifies AC13: Existing Version and VersionStatus fields continue to be
// populated as before when controlPlaneVersion is also set.
func TestControlPlaneVersionPropagation_WhenVersionExists_ItShouldPreserveExistingVersionField(t *testing.T) {
	now := metav1.NewTime(time.Now())
	completionTime := metav1.NewTime(now.Add(10 * time.Minute))

	existingVersion := &hyperv1.ClusterVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/openshift-release-dev/ocp-release:4.17.0",
		},
		History: []configv1.UpdateHistory{
			{
				State:       configv1.CompletedUpdate,
				StartedTime: now,
				Image:       "quay.io/openshift-release-dev/ocp-release:4.17.0",
				Version:     "4.17.0",
			},
		},
	}

	hcp := &hyperv1.HostedControlPlane{
		Status: hyperv1.HostedControlPlaneStatus{
			ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
				Desired: configv1.Release{
					Version: "4.17.0",
					Image:   "quay.io/openshift-release-dev/ocp-release:4.17.0",
				},
				History: []hyperv1.ControlPlaneUpdateHistory{
					{
						State:          configv1.CompletedUpdate,
						StartedTime:    now,
						CompletionTime: &completionTime,
						Version:        "4.17.0",
						Image:          "quay.io/openshift-release-dev/ocp-release:4.17.0",
					},
				},
				ObservedGeneration: 5,
			},
		},
	}

	hcluster := &hyperv1.HostedCluster{
		Status: hyperv1.HostedClusterStatus{
			Version: existingVersion,
		},
	}

	propagateControlPlaneVersion(hcluster, hcp)

	// Verify existing Version field is unchanged
	if hcluster.Status.Version == nil {
		t.Fatal("expected Version to be preserved, got nil")
	}
	if hcluster.Status.Version.Desired.Version != "4.17.0" {
		t.Errorf("expected Version.Desired.Version 4.17.0, got %s", hcluster.Status.Version.Desired.Version)
	}
	if len(hcluster.Status.Version.History) != 1 {
		t.Fatalf("expected 1 Version history entry, got %d", len(hcluster.Status.Version.History))
	}

	// Verify ControlPlaneVersion is also set
	if hcluster.Status.ControlPlaneVersion == nil {
		t.Fatal("expected ControlPlaneVersion to be set, got nil")
	}
	if hcluster.Status.ControlPlaneVersion.Desired.Version != "4.17.0" {
		t.Errorf("expected ControlPlaneVersion.Desired.Version 4.17.0, got %s", hcluster.Status.ControlPlaneVersion.Desired.Version)
	}
}

// TestControlPlaneVersionPropagation_WhenDeepCopied_ItShouldBeIndependent
// verifies that the deep copy is truly independent — mutations to the
// source HCP ControlPlaneVersion after propagation must not affect the
// HC copy.
func TestControlPlaneVersionPropagation_WhenDeepCopied_ItShouldBeIndependent(t *testing.T) {
	now := metav1.NewTime(time.Now())

	hcp := &hyperv1.HostedControlPlane{
		Status: hyperv1.HostedControlPlaneStatus{
			ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
				Desired: configv1.Release{
					Version: "4.17.0",
					Image:   "quay.io/openshift-release-dev/ocp-release:4.17.0",
				},
				History: []hyperv1.ControlPlaneUpdateHistory{
					{
						State:       configv1.PartialUpdate,
						StartedTime: now,
						Version:     "4.17.0",
						Image:       "quay.io/openshift-release-dev/ocp-release:4.17.0",
					},
				},
				ObservedGeneration: 3,
			},
		},
	}

	hcluster := &hyperv1.HostedCluster{
		Status: hyperv1.HostedClusterStatus{},
	}

	propagateControlPlaneVersion(hcluster, hcp)

	// Mutate the HCP source after propagation
	hcp.Status.ControlPlaneVersion.Desired.Version = "4.18.0"
	hcp.Status.ControlPlaneVersion.History[0].Version = "4.18.0"
	hcp.Status.ControlPlaneVersion.ObservedGeneration = 99

	// Verify the HC copy is not affected
	if hcluster.Status.ControlPlaneVersion.Desired.Version != "4.17.0" {
		t.Errorf("deep copy broken: expected HC version 4.17.0, got %s", hcluster.Status.ControlPlaneVersion.Desired.Version)
	}
	if hcluster.Status.ControlPlaneVersion.History[0].Version != "4.17.0" {
		t.Errorf("deep copy broken: expected HC history version 4.17.0, got %s", hcluster.Status.ControlPlaneVersion.History[0].Version)
	}
	if hcluster.Status.ControlPlaneVersion.ObservedGeneration != 3 {
		t.Errorf("deep copy broken: expected HC ObservedGeneration 3, got %d", hcluster.Status.ControlPlaneVersion.ObservedGeneration)
	}
}

// TestControlPlaneVersionPropagation_WhenUpgradeCompletes_ItShouldReachCompletedBeforeVersion
// verifies AC2: During an upgrade, controlPlaneVersion reaches Completed
// before or at the same time as version (never after), since control plane
// components finish before data-plane CVO rollout.
func TestControlPlaneVersionPropagation_WhenUpgradeCompletes_ItShouldReachCompletedBeforeVersion(t *testing.T) {
	now := metav1.NewTime(time.Now())
	cpCompletionTime := metav1.NewTime(now.Add(10 * time.Minute))

	hcp := &hyperv1.HostedControlPlane{
		Status: hyperv1.HostedControlPlaneStatus{
			ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
				Desired: configv1.Release{
					Version: "4.17.1",
					Image:   "quay.io/openshift-release-dev/ocp-release:4.17.1",
				},
				History: []hyperv1.ControlPlaneUpdateHistory{
					{
						State:          configv1.CompletedUpdate,
						StartedTime:    now,
						CompletionTime: &cpCompletionTime,
						Version:        "4.17.1",
						Image:          "quay.io/openshift-release-dev/ocp-release:4.17.1",
					},
				},
				ObservedGeneration: 6,
			},
		},
	}

	hcluster := &hyperv1.HostedCluster{
		Status: hyperv1.HostedClusterStatus{
			Version: &hyperv1.ClusterVersionStatus{
				Desired: configv1.Release{
					Version: "4.17.1",
					Image:   "quay.io/openshift-release-dev/ocp-release:4.17.1",
				},
				History: []configv1.UpdateHistory{
					{
						State:       configv1.PartialUpdate,
						StartedTime: now,
						Version:     "4.17.1",
						Image:       "quay.io/openshift-release-dev/ocp-release:4.17.1",
					},
				},
			},
		},
	}

	propagateControlPlaneVersion(hcluster, hcp)

	// Verify controlPlaneVersion is Completed while version is still Partial
	if hcluster.Status.ControlPlaneVersion == nil {
		t.Fatal("expected ControlPlaneVersion to be set")
	}
	if hcluster.Status.ControlPlaneVersion.History[0].State != configv1.CompletedUpdate {
		t.Errorf("expected ControlPlaneVersion history state Completed, got %s", hcluster.Status.ControlPlaneVersion.History[0].State)
	}
	if hcluster.Status.Version.History[0].State != configv1.PartialUpdate {
		t.Errorf("expected Version history state Partial, got %s", hcluster.Status.Version.History[0].State)
	}
}

// TestControlPlaneVersionPropagation_WhenMultipleHistoryEntries_ItShouldCopyAll
// verifies that all history entries are propagated, not just the most recent one.
func TestControlPlaneVersionPropagation_WhenMultipleHistoryEntries_ItShouldCopyAll(t *testing.T) {
	now := metav1.NewTime(time.Now())
	earlier := metav1.NewTime(now.Add(-1 * time.Hour))
	earlierCompletion := metav1.NewTime(earlier.Add(15 * time.Minute))
	completion := metav1.NewTime(now.Add(10 * time.Minute))

	hcp := &hyperv1.HostedControlPlane{
		Status: hyperv1.HostedControlPlaneStatus{
			ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
				Desired: configv1.Release{
					Version: "4.17.1",
					Image:   "quay.io/openshift-release-dev/ocp-release:4.17.1",
				},
				History: []hyperv1.ControlPlaneUpdateHistory{
					{
						State:          configv1.CompletedUpdate,
						StartedTime:    now,
						CompletionTime: &completion,
						Version:        "4.17.1",
						Image:          "quay.io/openshift-release-dev/ocp-release:4.17.1",
					},
					{
						State:          configv1.CompletedUpdate,
						StartedTime:    earlier,
						CompletionTime: &earlierCompletion,
						Version:        "4.17.0",
						Image:          "quay.io/openshift-release-dev/ocp-release:4.17.0",
					},
				},
				ObservedGeneration: 7,
			},
		},
	}

	hcluster := &hyperv1.HostedCluster{
		Status: hyperv1.HostedClusterStatus{},
	}

	propagateControlPlaneVersion(hcluster, hcp)

	if hcluster.Status.ControlPlaneVersion == nil {
		t.Fatal("expected ControlPlaneVersion to be set")
	}
	if len(hcluster.Status.ControlPlaneVersion.History) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(hcluster.Status.ControlPlaneVersion.History))
	}
	if hcluster.Status.ControlPlaneVersion.History[0].Version != "4.17.1" {
		t.Errorf("expected first history version 4.17.1, got %s", hcluster.Status.ControlPlaneVersion.History[0].Version)
	}
	if hcluster.Status.ControlPlaneVersion.History[1].Version != "4.17.0" {
		t.Errorf("expected second history version 4.17.0, got %s", hcluster.Status.ControlPlaneVersion.History[1].Version)
	}
}
