package v1beta1

import (
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ControlPlaneVersionStatus reports the status of the control plane version,
// tracking management-side component versions independently from CVO.
// +k8s:deepcopy-gen=true
type ControlPlaneVersionStatus struct {
	// desired is the release that the control plane is reconciling towards.
	// +required
	Desired configv1.Release `json:"desired"`

	// history contains a list of the most recent versions applied to the control plane.
	// Objects in the list are ordered by startedTime with the newest first.
	// +optional
	// +listType=atomic
	History []ControlPlaneUpdateHistory `json:"history,omitempty"`

	// observedGeneration reports which version of the spec is being reconciled.
	// If this value is not equal to metadata.generation, the desired release
	// has not yet been applied.
	// +required
	ObservedGeneration int64 `json:"observedGeneration"`
}

// ControlPlaneUpdateHistory is a single attempted update to the control plane.
// +k8s:deepcopy-gen=true
type ControlPlaneUpdateHistory struct {
	// state reflects whether the update was fully applied. The Partial state
	// indicates the update is not yet complete.
	// +required
	State configv1.UpdateState `json:"state"`

	// startedTime is the time at which the update was started.
	// +required
	StartedTime metav1.Time `json:"startedTime"`

	// completionTime is the time at which the update completed successfully.
	// It is set for Completed updates and nil for Partial updates.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// version is the semantic version of this update. When the requested
	// image does not define a version, or when a failure occurs retrieving
	// the image, this value may be empty.
	// +optional
	Version string `json:"version"`

	// image is a container image location that contains the update.
	// +required
	Image string `json:"image"`
}
