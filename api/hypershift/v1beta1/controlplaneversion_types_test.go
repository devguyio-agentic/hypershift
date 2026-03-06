package v1beta1

import (
	"encoding/json"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestControlPlaneVersionStatus_FieldTypes(t *testing.T) {
	// Verify ControlPlaneVersionStatus has the correct fields with correct types
	now := metav1.Now()
	completionTime := metav1.NewTime(now.Add(10 * time.Minute))

	status := ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
		},
		History: []ControlPlaneUpdateHistory{
			{
				State:          configv1.CompletedUpdate,
				StartedTime:    now,
				CompletionTime: &completionTime,
				Version:        "4.17.0",
				Image:          "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
			},
		},
		ObservedGeneration: 5,
	}

	if status.Desired.Version != "4.17.0" {
		t.Errorf("expected Desired.Version=4.17.0, got %s", status.Desired.Version)
	}
	if len(status.History) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(status.History))
	}
	if status.ObservedGeneration != 5 {
		t.Errorf("expected ObservedGeneration=5, got %d", status.ObservedGeneration)
	}
}

func TestControlPlaneUpdateHistory_OptionalCompletionTime(t *testing.T) {
	// Verify CompletionTime is an optional pointer field
	now := metav1.Now()

	// Partial entry: no completion time
	partial := ControlPlaneUpdateHistory{
		State:       configv1.PartialUpdate,
		StartedTime: now,
		Version:     "4.17.0",
		Image:       "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
	}
	if partial.CompletionTime != nil {
		t.Error("expected nil CompletionTime for partial entry")
	}

	// Completed entry: has completion time
	completionTime := metav1.NewTime(now.Add(5 * time.Minute))
	completed := ControlPlaneUpdateHistory{
		State:          configv1.CompletedUpdate,
		StartedTime:    now,
		CompletionTime: &completionTime,
		Version:        "4.17.0",
		Image:          "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
	}
	if completed.CompletionTime == nil {
		t.Error("expected non-nil CompletionTime for completed entry")
	}
}

func TestControlPlaneVersionStatus_JSONRoundTrip(t *testing.T) {
	// Verify JSON serialization round-trip with omitempty behavior
	now := metav1.Now()
	completionTime := metav1.NewTime(now.Add(10 * time.Minute))

	original := ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
		},
		History: []ControlPlaneUpdateHistory{
			{
				State:          configv1.CompletedUpdate,
				StartedTime:    now,
				CompletionTime: &completionTime,
				Version:        "4.17.0",
				Image:          "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
			},
			{
				State:       configv1.PartialUpdate,
				StartedTime: metav1.NewTime(now.Add(-1 * time.Hour)),
				Version:     "4.16.0",
				Image:       "quay.io/openshift-release-dev/ocp-release@sha256:def456",
			},
		},
		ObservedGeneration: 3,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var restored ControlPlaneVersionStatus
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if restored.Desired.Version != original.Desired.Version {
		t.Errorf("Desired.Version mismatch: got %s, want %s", restored.Desired.Version, original.Desired.Version)
	}
	if restored.Desired.Image != original.Desired.Image {
		t.Errorf("Desired.Image mismatch: got %s, want %s", restored.Desired.Image, original.Desired.Image)
	}
	if len(restored.History) != len(original.History) {
		t.Fatalf("History length mismatch: got %d, want %d", len(restored.History), len(original.History))
	}
	if restored.History[0].State != original.History[0].State {
		t.Errorf("History[0].State mismatch: got %s, want %s", restored.History[0].State, original.History[0].State)
	}
	if restored.History[1].CompletionTime != nil {
		t.Error("expected History[1].CompletionTime to be nil after round-trip (omitempty)")
	}
	if restored.ObservedGeneration != original.ObservedGeneration {
		t.Errorf("ObservedGeneration mismatch: got %d, want %d", restored.ObservedGeneration, original.ObservedGeneration)
	}
}

func TestControlPlaneVersionStatus_EmptyHistoryOmitted(t *testing.T) {
	// Verify empty/nil history is omitted from JSON (omitempty)
	status := ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
		},
		ObservedGeneration: 1,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	if _, exists := raw["history"]; exists {
		t.Error("expected 'history' field to be omitted when nil/empty")
	}
}

func TestHostedControlPlaneStatus_ControlPlaneVersionOptional(t *testing.T) {
	// Verify ControlPlaneVersion is optional on HostedControlPlaneStatus
	// When nil, should be omitted from JSON
	status := HostedControlPlaneStatus{
		Ready: true,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	if _, exists := raw["controlPlaneVersion"]; exists {
		t.Error("expected 'controlPlaneVersion' to be omitted when nil")
	}

	// When populated, should be present
	status.ControlPlaneVersion = &ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
		},
		ObservedGeneration: 1,
	}

	data, err = json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal with ControlPlaneVersion: %v", err)
	}

	raw = nil
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	if _, exists := raw["controlPlaneVersion"]; !exists {
		t.Error("expected 'controlPlaneVersion' to be present when populated")
	}
}

func TestHostedClusterStatus_ControlPlaneVersionOptional(t *testing.T) {
	// Verify ControlPlaneVersion is optional on HostedClusterStatus
	status := HostedClusterStatus{}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	if _, exists := raw["controlPlaneVersion"]; exists {
		t.Error("expected 'controlPlaneVersion' to be omitted when nil")
	}

	// When populated, should be present
	status.ControlPlaneVersion = &ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
		},
		ObservedGeneration: 1,
	}

	data, err = json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal with ControlPlaneVersion: %v", err)
	}

	raw = nil
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal to map: %v", err)
	}

	if _, exists := raw["controlPlaneVersion"]; !exists {
		t.Error("expected 'controlPlaneVersion' to be present when populated")
	}
}

func TestControlPlaneVersionStatus_DeepCopy(t *testing.T) {
	// Verify DeepCopy produces independent copies
	now := metav1.Now()
	completionTime := metav1.NewTime(now.Add(10 * time.Minute))

	original := &ControlPlaneVersionStatus{
		Desired: configv1.Release{
			Version: "4.17.0",
			Image:   "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
		},
		History: []ControlPlaneUpdateHistory{
			{
				State:          configv1.CompletedUpdate,
				StartedTime:    now,
				CompletionTime: &completionTime,
				Version:        "4.17.0",
				Image:          "quay.io/openshift-release-dev/ocp-release@sha256:abc123",
			},
		},
		ObservedGeneration: 5,
	}

	copied := original.DeepCopy()

	// Mutate the copy
	copied.Desired.Version = "4.18.0"
	copied.ObservedGeneration = 10
	copied.History[0].Version = "4.18.0"
	newTime := metav1.NewTime(now.Add(20 * time.Minute))
	copied.History[0].CompletionTime = &newTime

	// Verify original is unchanged
	if original.Desired.Version != "4.17.0" {
		t.Errorf("original Desired.Version mutated: got %s", original.Desired.Version)
	}
	if original.ObservedGeneration != 5 {
		t.Errorf("original ObservedGeneration mutated: got %d", original.ObservedGeneration)
	}
	if original.History[0].Version != "4.17.0" {
		t.Errorf("original History[0].Version mutated: got %s", original.History[0].Version)
	}
	if !original.History[0].CompletionTime.Equal(&completionTime) {
		t.Error("original History[0].CompletionTime mutated")
	}
}
