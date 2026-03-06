package util

import (
	configv1 "github.com/openshift/api/config/v1"
	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
)

// isControlPlaneVersionCompleted checks if the control plane version has
// reached Completed state with the desired image. Used by WaitForControlPlaneRollout.
func isControlPlaneVersionCompleted(hc *hyperv1.HostedCluster) bool {
	if hc.Status.ControlPlaneVersion == nil {
		return false
	}
	if len(hc.Status.ControlPlaneVersion.History) == 0 {
		return false
	}
	entry := hc.Status.ControlPlaneVersion.History[0]
	if entry.State != configv1.CompletedUpdate {
		return false
	}
	if entry.Image != hc.Status.ControlPlaneVersion.Desired.Image {
		return false
	}
	return true
}

// isControlPlaneVersionSteadyState checks if the control plane version is in a
// valid steady state for ValidateHostedClusterConditions. For clusters with
// worker nodes, controlPlaneVersion must be Completed. For zero-worker-node
// clusters, controlPlaneVersion can be Completed even when version is Partial.
func isControlPlaneVersionSteadyState(hc *hyperv1.HostedCluster, hasWorkerNodes bool) bool {
	if hc.Status.ControlPlaneVersion == nil {
		return false
	}
	if len(hc.Status.ControlPlaneVersion.History) == 0 {
		return false
	}
	cpState := hc.Status.ControlPlaneVersion.History[0].State
	if cpState != configv1.CompletedUpdate {
		return false
	}
	return true
}
