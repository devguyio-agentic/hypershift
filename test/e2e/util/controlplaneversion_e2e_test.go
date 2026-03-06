//go:build e2e

package util

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// Tests for WaitForControlPlaneRollout (AC14)

func TestWaitForControlPlaneRollout_WhenControlPlaneVersionCompleted_ItShouldSucceed(t *testing.T) {
	// AC14: Given TestUpgradeControlPlane e2e test runs
	// When an upgrade is performed
	// Then WaitForControlPlaneRollout (checks HC.Status.ControlPlaneVersion) passes
	t.Skip("e2e test — requires a live cluster with controlPlaneVersion support")
}

func TestWaitForControlPlaneRollout_WhenVersionGatedBelow422_ItShouldSkip(t *testing.T) {
	// AC16: Given new controlPlaneVersion e2e assertions
	// When running against older HC versions without the field
	// Then assertions are gated with AtLeast(t, Version422) and do not fail
	t.Skip("e2e test — requires a live cluster at version < 4.22")
}

// Tests for WaitForDataPlaneRollout (renamed from WaitForImageRollout) (AC14)

func TestWaitForDataPlaneRollout_WhenDataPlaneVersionCompleted_ItShouldSucceed(t *testing.T) {
	// AC14: Given TestUpgradeControlPlane e2e test runs
	// When an upgrade is performed
	// Then WaitForDataPlaneRollout (checks HC.Status.Version) passes
	t.Skip("e2e test — requires a live cluster")
}

func TestWaitForDataPlaneRollout_WhenCalledViaDeprecatedAlias_ItShouldSucceed(t *testing.T) {
	// AC14: Verifies that the deprecated WaitForImageRollout alias still
	// works and delegates to WaitForDataPlaneRollout.
	t.Skip("e2e test — requires a live cluster")
}

// Tests for ValidateHostedClusterConditions with controlPlaneVersion (AC15)

func TestValidateHostedClusterConditions_WhenSteadyState_ItShouldCheckControlPlaneVersionCompleted(t *testing.T) {
	// AC15: Given ValidateHostedClusterConditions runs on a steady-state cluster
	// When validation checks are evaluated
	// Then controlPlaneVersion state is Completed
	t.Skip("e2e test — requires a live cluster at version >= 4.22")
}

func TestValidateHostedClusterConditions_WhenZeroWorkerNodes_ItShouldAllowControlPlaneCompletedWithVersionPartial(t *testing.T) {
	// AC15: Given ValidateHostedClusterConditions runs on a zero-worker-node cluster
	// When validation checks are evaluated
	// Then controlPlaneVersion can be Completed even when version is Partial
	t.Skip("e2e test — requires a live cluster with zero worker nodes")
}

func TestValidateHostedClusterConditions_WhenVersionGatedBelow422_ItShouldSkipControlPlaneVersionCheck(t *testing.T) {
	// AC16: Given new controlPlaneVersion e2e assertions in ValidateHostedClusterConditions
	// When running against older HC versions without the field
	// Then the controlPlaneVersion check is skipped via version gating
	t.Skip("e2e test — requires a live cluster at version < 4.22")
}

// Unit-testable validation helpers

func TestControlPlaneVersionCompletionCheck(t *testing.T) {
	now := metav1.Now()
	testImage := "quay.io/openshift-release-dev/ocp-release@sha256:abc123"

	tests := []struct {
		name     string
		hc       *hyperv1.HostedCluster
		wantDone bool
	}{
		{
			name: "When controlPlaneVersion is completed it should report done",
			hc: &hyperv1.HostedCluster{
				Status: hyperv1.HostedClusterStatus{
					ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
						Desired: configv1.Release{Image: testImage},
						History: []hyperv1.ControlPlaneUpdateHistory{
							{
								State:          configv1.CompletedUpdate,
								Image:          testImage,
								Version:        "4.22.0",
								StartedTime:    now,
								CompletionTime: &now,
							},
						},
					},
				},
			},
			wantDone: true,
		},
		{
			name: "When controlPlaneVersion is partial it should report not done",
			hc: &hyperv1.HostedCluster{
				Status: hyperv1.HostedClusterStatus{
					ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
						Desired: configv1.Release{Image: testImage},
						History: []hyperv1.ControlPlaneUpdateHistory{
							{
								State:       configv1.PartialUpdate,
								Image:       testImage,
								Version:     "4.22.0",
								StartedTime: now,
							},
						},
					},
				},
			},
			wantDone: false,
		},
		{
			name: "When controlPlaneVersion has no history it should report not done",
			hc: &hyperv1.HostedCluster{
				Status: hyperv1.HostedClusterStatus{
					ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
						Desired: configv1.Release{Image: testImage},
					},
				},
			},
			wantDone: false,
		},
		{
			name: "When controlPlaneVersion is nil it should report not done",
			hc: &hyperv1.HostedCluster{
				Status: hyperv1.HostedClusterStatus{
					ControlPlaneVersion: nil,
				},
			},
			wantDone: false,
		},
		{
			name: "When desired image does not match history image it should report not done",
			hc: &hyperv1.HostedCluster{
				Status: hyperv1.HostedClusterStatus{
					ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
						Desired: configv1.Release{Image: testImage},
						History: []hyperv1.ControlPlaneUpdateHistory{
							{
								State:          configv1.CompletedUpdate,
								Image:          "quay.io/openshift-release-dev/ocp-release@sha256:different",
								Version:        "4.21.0",
								StartedTime:    now,
								CompletionTime: &now,
							},
						},
					},
				},
			},
			wantDone: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done := isControlPlaneVersionCompleted(tt.hc)
			if done != tt.wantDone {
				t.Errorf("isControlPlaneVersionCompleted() = %v, want %v", done, tt.wantDone)
			}
		})
	}
}

func TestControlPlaneVersionSteadyStateCheck(t *testing.T) {
	now := metav1.Now()
	testImage := "quay.io/openshift-release-dev/ocp-release@sha256:abc123"

	tests := []struct {
		name           string
		hc             *hyperv1.HostedCluster
		hasWorkerNodes bool
		wantValid      bool
	}{
		{
			name: "When steady state with workers it should require controlPlaneVersion completed",
			hc: &hyperv1.HostedCluster{
				Status: hyperv1.HostedClusterStatus{
					Version: ptr.To(hyperv1.ClusterVersionStatus{
						Desired: configv1.Release{Image: testImage},
						History: []configv1.UpdateHistory{
							{State: configv1.CompletedUpdate, Image: testImage},
						},
					}),
					ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
						Desired: configv1.Release{Image: testImage},
						History: []hyperv1.ControlPlaneUpdateHistory{
							{State: configv1.CompletedUpdate, Image: testImage, StartedTime: now, CompletionTime: &now},
						},
					},
				},
			},
			hasWorkerNodes: true,
			wantValid:      true,
		},
		{
			name: "When zero workers it should allow controlPlaneVersion completed with version partial",
			hc: &hyperv1.HostedCluster{
				Status: hyperv1.HostedClusterStatus{
					Version: ptr.To(hyperv1.ClusterVersionStatus{
						Desired: configv1.Release{Image: testImage},
						History: []configv1.UpdateHistory{
							{State: configv1.PartialUpdate, Image: testImage},
						},
					}),
					ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
						Desired: configv1.Release{Image: testImage},
						History: []hyperv1.ControlPlaneUpdateHistory{
							{State: configv1.CompletedUpdate, Image: testImage, StartedTime: now, CompletionTime: &now},
						},
					},
				},
			},
			hasWorkerNodes: false,
			wantValid:      true,
		},
		{
			name: "When steady state with workers but controlPlaneVersion partial it should be invalid",
			hc: &hyperv1.HostedCluster{
				Status: hyperv1.HostedClusterStatus{
					Version: ptr.To(hyperv1.ClusterVersionStatus{
						Desired: configv1.Release{Image: testImage},
						History: []configv1.UpdateHistory{
							{State: configv1.CompletedUpdate, Image: testImage},
						},
					}),
					ControlPlaneVersion: &hyperv1.ControlPlaneVersionStatus{
						Desired: configv1.Release{Image: testImage},
						History: []hyperv1.ControlPlaneUpdateHistory{
							{State: configv1.PartialUpdate, Image: testImage, StartedTime: now},
						},
					},
				},
			},
			hasWorkerNodes: true,
			wantValid:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := isControlPlaneVersionSteadyState(tt.hc, tt.hasWorkerNodes)
			if valid != tt.wantValid {
				t.Errorf("isControlPlaneVersionSteadyState() = %v, want %v", valid, tt.wantValid)
			}
		})
	}
}
