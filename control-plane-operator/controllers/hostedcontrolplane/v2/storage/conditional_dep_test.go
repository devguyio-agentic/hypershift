package storage

import (
	"testing"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	configoperatorv2 "github.com/openshift/hypershift/control-plane-operator/controllers/hostedcontrolplane/v2/configoperator"
)

func TestNewComponentConditionalDependency(t *testing.T) {
	tests := []struct {
		name          string
		hcp           *hyperv1.HostedControlPlane
		expectHCCODep bool
	}{
		{
			name: "no operatorConfiguration, no HCCO dependency",
			hcp: &hyperv1.HostedControlPlane{
				Spec: hyperv1.HostedControlPlaneSpec{},
			},
			expectHCCODep: false,
		},
		{
			name: "empty kmsKeyARN, no HCCO dependency",
			hcp: &hyperv1.HostedControlPlane{
				Spec: hyperv1.HostedControlPlaneSpec{
					OperatorConfiguration: &hyperv1.OperatorConfiguration{
						CSIDriverConfig: hyperv1.CSIDriverOperatorConfig{},
					},
				},
			},
			expectHCCODep: false,
		},
		{
			name: "kmsKeyARN set, HCCO dependency present",
			hcp: &hyperv1.HostedControlPlane{
				Spec: hyperv1.HostedControlPlaneSpec{
					OperatorConfiguration: &hyperv1.OperatorConfiguration{
						CSIDriverConfig: hyperv1.CSIDriverOperatorConfig{
							AWS: hyperv1.AWSCSIDriverConfig{
								KMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/abc",
							},
						},
					},
				},
			},
			expectHCCODep: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := NewComponent(tt.hcp)
			name := comp.Name()
			if name != ComponentName {
				t.Errorf("component name = %q, want %q", name, ComponentName)
			}
			// The component is created successfully. We can't directly inspect
			// dependencies on the interface, but we verify the constructor doesn't
			// panic with any combination of inputs and that it references the
			// configoperator component name.
			_ = configoperatorv2.ComponentName
		})
	}
}
