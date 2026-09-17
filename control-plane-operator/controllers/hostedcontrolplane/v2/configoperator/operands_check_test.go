package configoperator

import (
	"testing"

	hyperv1 "github.com/openshift/hypershift/api/hypershift/v1beta1"
	component "github.com/openshift/hypershift/support/controlplane-component"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCheckHCCOReconciliationSucceeded(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		want       bool
	}{
		{
			name:       "When condition is absent, it should return false",
			conditions: nil,
			want:       false,
		},
		{
			name: "When condition is False, it should return false",
			conditions: []metav1.Condition{
				{
					Type:   string(hyperv1.ConfigOperatorReconciliationSucceeded),
					Status: metav1.ConditionFalse,
					Reason: hyperv1.ReconcileErrorReason,
				},
			},
			want: false,
		},
		{
			name: "When condition is True, it should return true",
			conditions: []metav1.Condition{
				{
					Type:   string(hyperv1.ConfigOperatorReconciliationSucceeded),
					Status: metav1.ConditionTrue,
					Reason: hyperv1.AsExpectedReason,
				},
			},
			want: true,
		},
		{
			name: "When only unrelated conditions are present, it should return false",
			conditions: []metav1.Condition{
				{
					Type:   "SomeOtherCondition",
					Status: metav1.ConditionTrue,
					Reason: "Whatever",
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cpContext := component.WorkloadContext{
				HCP: &hyperv1.HostedControlPlane{
					Status: hyperv1.HostedControlPlaneStatus{
						Conditions: tt.conditions,
					},
				},
			}
			got, err := checkHCCOReconciliationSucceeded(cpContext)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("checkHCCOReconciliationSucceeded() = %v, want %v", got, tt.want)
			}
		})
	}
}
