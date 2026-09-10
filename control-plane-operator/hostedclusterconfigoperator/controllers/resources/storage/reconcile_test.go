package storage

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReconcileClusterCSIDriverKMSKey(t *testing.T) {
	tests := []struct {
		name             string
		driver           *operatorv1.ClusterCSIDriver
		kmsKeyARN        string
		expectKMSKey     string
		expectAnnotation bool
	}{
		{
			name: "When kmsKeyARN is set and annotation is absent, it should write the key and set the annotation",
			driver: &operatorv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: "ebs.csi.aws.com"},
			},
			kmsKeyARN:        "arn:aws:kms:us-east-1:123456789012:key/test-key",
			expectKMSKey:     "arn:aws:kms:us-east-1:123456789012:key/test-key",
			expectAnnotation: true,
		},
		{
			name: "When kmsKeyARN is empty and annotation is absent, it should set the annotation without writing DriverConfig",
			driver: &operatorv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{Name: "ebs.csi.aws.com"},
			},
			kmsKeyARN:        "",
			expectKMSKey:     "",
			expectAnnotation: true,
		},
		{
			name: "When annotation is present, it should skip even if kmsKeyARN is set",
			driver: &operatorv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "ebs.csi.aws.com",
					Annotations: map[string]string{StorageDriverConfigAppliedAnnotation: "true"},
				},
			},
			kmsKeyARN:        "arn:aws:kms:us-east-1:123456789012:key/test-key",
			expectKMSKey:     "",
			expectAnnotation: true,
		},
		{
			name: "When annotation is present and admin cleared DriverConfig, it should not re-populate",
			driver: &operatorv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "ebs.csi.aws.com",
					Annotations: map[string]string{StorageDriverConfigAppliedAnnotation: "true"},
				},
				Spec: operatorv1.ClusterCSIDriverSpec{},
			},
			kmsKeyARN:        "arn:aws:kms:us-east-1:123456789012:key/test-key",
			expectKMSKey:     "",
			expectAnnotation: true,
		},
		{
			name: "When annotation is present and DriverConfig has admin key, it should preserve admin key",
			driver: &operatorv1.ClusterCSIDriver{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "ebs.csi.aws.com",
					Annotations: map[string]string{StorageDriverConfigAppliedAnnotation: "true"},
				},
				Spec: operatorv1.ClusterCSIDriverSpec{
					DriverConfig: operatorv1.CSIDriverConfigSpec{
						DriverType: operatorv1.AWSDriverType,
						AWS: &operatorv1.AWSCSIDriverConfigSpec{
							KMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/admin-rotated-key",
						},
					},
				},
			},
			kmsKeyARN:        "arn:aws:kms:us-east-1:123456789012:key/original-key",
			expectKMSKey:     "arn:aws:kms:us-east-1:123456789012:key/admin-rotated-key",
			expectAnnotation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ReconcileClusterCSIDriverKMSKey(tt.driver, tt.kmsKeyARN)

			// Check annotation
			_, hasAnnotation := tt.driver.Annotations[StorageDriverConfigAppliedAnnotation]
			if hasAnnotation != tt.expectAnnotation {
				t.Errorf("expected annotation present=%v, got %v", tt.expectAnnotation, hasAnnotation)
			}

			// Check KMS key
			actualKey := ""
			if tt.driver.Spec.DriverConfig.AWS != nil {
				actualKey = tt.driver.Spec.DriverConfig.AWS.KMSKeyARN
			}
			if actualKey != tt.expectKMSKey {
				t.Errorf("expected KMSKeyARN=%q, got %q", tt.expectKMSKey, actualKey)
			}
		})
	}
}
