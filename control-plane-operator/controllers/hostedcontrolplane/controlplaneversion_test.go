package hostedcontrolplane

import (
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
	testingclock "k8s.io/utils/clock/testing"
)

// newHCP creates a minimal HostedControlPlane for testing.
func newHCP(releaseImage string, controlPlaneReleaseImage *string, generation int64) *hyperv1.HostedControlPlane {
	hcp := &hyperv1.HostedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-hcp",
			Namespace:  "test-ns",
			Generation: generation,
		},
		Spec: hyperv1.HostedControlPlaneSpec{
			ReleaseImage: releaseImage,
		},
	}
	if controlPlaneReleaseImage != nil {
		hcp.Spec.ControlPlaneReleaseImage = controlPlaneReleaseImage
	}
	return hcp
}

// newComponent creates a ControlPlaneComponent with the given version and rollout status.
func newComponent(name, version string, rolloutComplete bool) hyperv1.ControlPlaneComponent {
	conditions := []metav1.Condition{
		{
			Type:               string(hyperv1.ControlPlaneComponentAvailable),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
		},
	}
	if rolloutComplete {
		conditions = append(conditions, metav1.Condition{
			Type:               string(hyperv1.ControlPlaneComponentRolloutComplete),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
		})
	} else {
		conditions = append(conditions, metav1.Condition{
			Type:               string(hyperv1.ControlPlaneComponentRolloutComplete),
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
		})
	}
	return hyperv1.ControlPlaneComponent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
		},
		Status: hyperv1.ControlPlaneComponentStatus{
			Version:    version,
			Conditions: conditions,
		},
	}
}

func strPtr(s string) *string { return &s }

// TestReconcileControlPlaneVersion_AllComponentsComplete verifies AC1:
// When all ControlPlaneComponent resources report the target version with
// RolloutComplete=True, controlPlaneVersion.history[0].State transitions
// to Completed and completionTime is set.
func TestReconcileControlPlaneVersion_AllComponentsComplete(t *testing.T) {
	fakeClock := testingclock.NewFakeClock(time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC))
	hcp := newHCP("quay.io/ocp/release@sha256:aaa", nil, 1)
	hcp.Status.ControlPlaneVersion = &hyperv1.ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/ocp/release@sha256:aaa",
		},
		History: []hyperv1.ControlPlaneUpdateHistory{
			{
				State:       configv1.PartialUpdate,
				StartedTime: metav1.NewTime(fakeClock.Now().Add(-10 * time.Minute)),
				Version:     "4.17.0",
				Image:       "quay.io/ocp/release@sha256:aaa",
			},
		},
		ObservedGeneration: 1,
	}

	components := []hyperv1.ControlPlaneComponent{
		newComponent("kube-apiserver", "4.17.0", true),
		newComponent("kube-controller-manager", "4.17.0", true),
		newComponent("openshift-apiserver", "4.17.0", true),
	}

	result := reconcileControlPlaneVersion(hcp, components, fakeClock)

	if result.History[0].State != configv1.CompletedUpdate {
		t.Errorf("When all components are complete, it should transition to Completed, got %s", result.History[0].State)
	}
	if result.History[0].CompletionTime == nil {
		t.Error("When all components are complete, it should set completionTime")
	}
}

// TestReconcileControlPlaneVersion_NewDesiredRelease verifies AC3:
// When the desired release changes, a new Partial history entry is prepended.
func TestReconcileControlPlaneVersion_NewDesiredRelease(t *testing.T) {
	fakeClock := testingclock.NewFakeClock(time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC))
	hcp := newHCP("quay.io/ocp/release@sha256:bbb", nil, 2)
	hcp.Status.ControlPlaneVersion = &hyperv1.ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/ocp/release@sha256:aaa",
		},
		History: []hyperv1.ControlPlaneUpdateHistory{
			{
				State:          configv1.CompletedUpdate,
				StartedTime:    metav1.NewTime(fakeClock.Now().Add(-1 * time.Hour)),
				CompletionTime: &metav1.Time{Time: fakeClock.Now().Add(-30 * time.Minute)},
				Version:        "4.17.0",
				Image:          "quay.io/ocp/release@sha256:aaa",
			},
		},
		ObservedGeneration: 1,
	}

	components := []hyperv1.ControlPlaneComponent{
		newComponent("kube-apiserver", "4.17.0", true),
	}

	result := reconcileControlPlaneVersion(hcp, components, fakeClock)

	if len(result.History) < 2 {
		t.Fatalf("When desired release changes, it should prepend new entry, got %d entries", len(result.History))
	}
	if result.History[0].State != configv1.PartialUpdate {
		t.Errorf("When desired release changes, it should prepend Partial entry, got %s", result.History[0].State)
	}
	if result.History[0].Image != "quay.io/ocp/release@sha256:bbb" {
		t.Errorf("When desired release changes, it should use new image, got %s", result.History[0].Image)
	}
}

// TestReconcileControlPlaneVersion_ImageOnlyChange verifies AC4:
// When the image digest changes but the semver is the same, a new Partial
// entry is prepended (CVE patch scenario).
func TestReconcileControlPlaneVersion_ImageOnlyChange(t *testing.T) {
	fakeClock := testingclock.NewFakeClock(time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC))
	hcp := newHCP("quay.io/ocp/release@sha256:bbb", nil, 2)
	hcp.Status.ControlPlaneVersion = &hyperv1.ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/ocp/release@sha256:aaa",
		},
		History: []hyperv1.ControlPlaneUpdateHistory{
			{
				State:          configv1.CompletedUpdate,
				StartedTime:    metav1.NewTime(fakeClock.Now().Add(-1 * time.Hour)),
				CompletionTime: &metav1.Time{Time: fakeClock.Now().Add(-30 * time.Minute)},
				Version:        "4.17.0",
				Image:          "quay.io/ocp/release@sha256:aaa",
			},
		},
		ObservedGeneration: 1,
	}

	components := []hyperv1.ControlPlaneComponent{
		newComponent("kube-apiserver", "4.17.0", true),
	}

	result := reconcileControlPlaneVersion(hcp, components, fakeClock)

	if len(result.History) < 2 {
		t.Fatalf("When image changes (same version), it should prepend new entry, got %d entries", len(result.History))
	}
	if result.History[0].State != configv1.PartialUpdate {
		t.Errorf("When image changes (same version), it should prepend Partial entry, got %s", result.History[0].State)
	}
	if result.History[0].Image != "quay.io/ocp/release@sha256:bbb" {
		t.Errorf("When image changes (same version), it should use new image, got %s", result.History[0].Image)
	}
}

// TestReconcileControlPlaneVersion_NewComponentMidUpgrade verifies AC6:
// When a new ControlPlaneComponent is created mid-upgrade that hasn't reached
// the desired version, controlPlaneVersion remains Partial.
func TestReconcileControlPlaneVersion_NewComponentMidUpgrade(t *testing.T) {
	fakeClock := testingclock.NewFakeClock(time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC))
	hcp := newHCP("quay.io/ocp/release@sha256:aaa", nil, 1)
	hcp.Status.ControlPlaneVersion = &hyperv1.ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/ocp/release@sha256:aaa",
		},
		History: []hyperv1.ControlPlaneUpdateHistory{
			{
				State:       configv1.PartialUpdate,
				StartedTime: metav1.NewTime(fakeClock.Now().Add(-5 * time.Minute)),
				Version:     "4.17.0",
				Image:       "quay.io/ocp/release@sha256:aaa",
			},
		},
		ObservedGeneration: 1,
	}

	components := []hyperv1.ControlPlaneComponent{
		newComponent("kube-apiserver", "4.17.0", true),
		newComponent("kube-controller-manager", "4.17.0", true),
		newComponent("new-component", "4.16.0", false), // mid-upgrade, not at desired version
	}

	result := reconcileControlPlaneVersion(hcp, components, fakeClock)

	if result.History[0].State != configv1.PartialUpdate {
		t.Errorf("When a component mid-upgrade hasn't reached desired version, it should remain Partial, got %s", result.History[0].State)
	}
	if result.History[0].CompletionTime != nil {
		t.Error("When a component mid-upgrade hasn't reached desired version, it should not set completionTime")
	}
}

// TestReconcileControlPlaneVersion_FirstPopulation verifies AC7:
// When controlPlaneVersion is first populated on an existing cluster with all
// components at the desired version, history initializes with Partial on the
// first reconciliation, then transitions to Completed on the second.
func TestReconcileControlPlaneVersion_FirstPopulation(t *testing.T) {
	fakeClock := testingclock.NewFakeClock(time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC))
	hcp := newHCP("quay.io/ocp/release@sha256:aaa", nil, 1)
	// No existing ControlPlaneVersion status (first population)

	components := []hyperv1.ControlPlaneComponent{
		newComponent("kube-apiserver", "4.17.0", true),
		newComponent("kube-controller-manager", "4.17.0", true),
	}

	// First reconciliation: should create Partial entry
	result := reconcileControlPlaneVersion(hcp, components, fakeClock)

	if len(result.History) != 1 {
		t.Fatalf("When first populated, it should create 1 history entry, got %d", len(result.History))
	}
	if result.History[0].State != configv1.PartialUpdate {
		t.Errorf("When first populated, it should initialize with Partial, got %s", result.History[0].State)
	}

	// Second reconciliation: should transition to Completed
	hcp.Status.ControlPlaneVersion = result
	fakeClock.Step(1 * time.Minute)
	result2 := reconcileControlPlaneVersion(hcp, components, fakeClock)

	if result2.History[0].State != configv1.CompletedUpdate {
		t.Errorf("When first populated and all components ready, second reconcile should transition to Completed, got %s", result2.History[0].State)
	}
	if result2.History[0].CompletionTime == nil {
		t.Error("When first populated and all components ready, second reconcile should set completionTime")
	}
}

// TestReconcileControlPlaneVersion_ControlPlaneReleaseImage verifies AC9:
// When HCP.Spec.ControlPlaneReleaseImage is set, controlPlaneVersion.desired
// reflects ControlPlaneReleaseImage, not Spec.ReleaseImage.
func TestReconcileControlPlaneVersion_ControlPlaneReleaseImage(t *testing.T) {
	fakeClock := testingclock.NewFakeClock(time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC))
	cpReleaseImage := "quay.io/ocp/release@sha256:cponly"
	hcp := newHCP("quay.io/ocp/release@sha256:dataplane", strPtr(cpReleaseImage), 1)

	components := []hyperv1.ControlPlaneComponent{
		newComponent("kube-apiserver", "4.17.0", true),
	}

	result := reconcileControlPlaneVersion(hcp, components, fakeClock)

	if result.Desired.Image != cpReleaseImage {
		t.Errorf("When ControlPlaneReleaseImage is set, it should use it as desired, got %s", result.Desired.Image)
	}
}

// TestReconcileControlPlaneVersion_ObservedGeneration verifies AC10:
// When the CPO reconciles successfully, observedGeneration reflects the
// generation that was actually processed.
func TestReconcileControlPlaneVersion_ObservedGeneration(t *testing.T) {
	fakeClock := testingclock.NewFakeClock(time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC))
	hcp := newHCP("quay.io/ocp/release@sha256:aaa", nil, 7)

	components := []hyperv1.ControlPlaneComponent{
		newComponent("kube-apiserver", "4.17.0", true),
	}

	result := reconcileControlPlaneVersion(hcp, components, fakeClock)

	if result.ObservedGeneration != 7 {
		t.Errorf("When reconciliation succeeds, it should set observedGeneration to current generation (7), got %d", result.ObservedGeneration)
	}
}

// TestReconcileControlPlaneVersion_ComponentFailure verifies AC11:
// When a component fails to reconcile, controlPlaneVersion maintains a
// Partial history entry.
func TestReconcileControlPlaneVersion_ComponentFailure(t *testing.T) {
	fakeClock := testingclock.NewFakeClock(time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC))
	hcp := newHCP("quay.io/ocp/release@sha256:aaa", nil, 1)
	hcp.Status.ControlPlaneVersion = &hyperv1.ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/ocp/release@sha256:aaa",
		},
		History: []hyperv1.ControlPlaneUpdateHistory{
			{
				State:       configv1.PartialUpdate,
				StartedTime: metav1.NewTime(fakeClock.Now().Add(-5 * time.Minute)),
				Version:     "4.17.0",
				Image:       "quay.io/ocp/release@sha256:aaa",
			},
		},
		ObservedGeneration: 1,
	}

	components := []hyperv1.ControlPlaneComponent{
		newComponent("kube-apiserver", "4.17.0", true),
		newComponent("kube-controller-manager", "4.16.0", false), // failed to reconcile
	}

	result := reconcileControlPlaneVersion(hcp, components, fakeClock)

	if result.History[0].State != configv1.PartialUpdate {
		t.Errorf("When a component fails, it should maintain Partial state, got %s", result.History[0].State)
	}
	if result.History[0].CompletionTime != nil {
		t.Error("When a component fails, it should not set completionTime")
	}
}

// TestReconcileControlPlaneVersion_SupersededPartial verifies AC17:
// When controlPlaneVersion is Partial for version X and a new desired release
// Y is detected, the Partial entry for X receives a CompletionTime stamp
// before Y's entry is prepended.
func TestReconcileControlPlaneVersion_SupersededPartial(t *testing.T) {
	fakeClock := testingclock.NewFakeClock(time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC))
	hcp := newHCP("quay.io/ocp/release@sha256:bbb", nil, 2)
	startTime := metav1.NewTime(fakeClock.Now().Add(-10 * time.Minute))
	hcp.Status.ControlPlaneVersion = &hyperv1.ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/ocp/release@sha256:aaa",
		},
		History: []hyperv1.ControlPlaneUpdateHistory{
			{
				State:       configv1.PartialUpdate,
				StartedTime: startTime,
				Version:     "4.17.0",
				Image:       "quay.io/ocp/release@sha256:aaa",
			},
		},
		ObservedGeneration: 1,
	}

	components := []hyperv1.ControlPlaneComponent{
		newComponent("kube-apiserver", "4.17.0", false),
	}

	result := reconcileControlPlaneVersion(hcp, components, fakeClock)

	if len(result.History) < 2 {
		t.Fatalf("When superseding a Partial entry, it should prepend new entry, got %d entries", len(result.History))
	}
	// New entry at [0]
	if result.History[0].State != configv1.PartialUpdate {
		t.Errorf("When superseding, it should prepend new Partial entry, got %s", result.History[0].State)
	}
	if result.History[0].Image != "quay.io/ocp/release@sha256:bbb" {
		t.Errorf("When superseding, it should use new image for prepended entry, got %s", result.History[0].Image)
	}
	// Superseded entry at [1] should have CompletionTime stamped
	if result.History[1].CompletionTime == nil {
		t.Error("When superseding a Partial entry, it should stamp CompletionTime on the old entry")
	}
	if result.History[1].State != configv1.PartialUpdate {
		t.Errorf("When superseding, the old entry should remain Partial (not switch to Completed), got %s", result.History[1].State)
	}
}

// TestReconcileControlPlaneVersion_NoComponents verifies behavior when no
// ControlPlaneComponent resources exist yet — should create initial Partial entry.
func TestReconcileControlPlaneVersion_NoComponents(t *testing.T) {
	fakeClock := testingclock.NewFakeClock(time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC))
	hcp := newHCP("quay.io/ocp/release@sha256:aaa", nil, 1)

	var components []hyperv1.ControlPlaneComponent

	result := reconcileControlPlaneVersion(hcp, components, fakeClock)

	if result == nil {
		t.Fatal("When no components exist, it should still return a status")
	}
	if len(result.History) != 1 {
		t.Fatalf("When no components exist, it should create initial Partial entry, got %d entries", len(result.History))
	}
	if result.History[0].State != configv1.PartialUpdate {
		t.Errorf("When no components exist, it should create Partial entry, got %s", result.History[0].State)
	}
}

// Ensure the clock.Clock interface is used for testability.
var _ clock.Clock = &testingclock.FakeClock{}
