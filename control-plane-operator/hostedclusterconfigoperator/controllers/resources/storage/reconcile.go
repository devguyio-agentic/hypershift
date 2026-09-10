package storage

import (
	operatorv1 "github.com/openshift/api/operator/v1"
)

func ReconcileOperatorSpec(spec *operatorv1.OperatorSpec) {
	spec.LogLevel = operatorv1.Normal
	spec.OperatorLogLevel = operatorv1.Normal
	spec.ManagementState = operatorv1.Managed
}

func ReconcileCSISnapshotController(csi *operatorv1.CSISnapshotController) {
	ReconcileOperatorSpec(&csi.Spec.OperatorSpec)
}

func ReconcileStorage(storage *operatorv1.Storage) {
	ReconcileOperatorSpec(&storage.Spec.OperatorSpec)
}

func ReconcileClusterCSIDriver(driver *operatorv1.ClusterCSIDriver) {
	ReconcileOperatorSpec(&driver.Spec.OperatorSpec)
}

const StorageDriverConfigAppliedAnnotation = "hypershift.openshift.io/storage-driver-config-applied"

// ReconcileClusterCSIDriverKMSKey configures the KMS key ARN on the ClusterCSIDriver
// for AWS EBS encryption. This follows a set-once pattern using an annotation on the
// ClusterCSIDriver resource. On the first storage reconciliation pass, the HCCO
// writes the KMS key (if configured) and sets the annotation. On subsequent
// reconciles, the annotation's presence causes the HCCO to skip DriverConfig
// entirely, preserving any in-cluster modifications made by the administrator.
//
// If kmsKeyARN is empty, the annotation is still set to commit that the initial
// storage configuration pass has completed, preventing future writes even if
// kmsKeyARN is later added to the HCP spec.
func ReconcileClusterCSIDriverKMSKey(driver *operatorv1.ClusterCSIDriver, kmsKeyARN string) {
	// Set-once: if the annotation is present, the initial pass is done.
	if driver.Annotations != nil {
		if _, exists := driver.Annotations[StorageDriverConfigAppliedAnnotation]; exists {
			return
		}
	}

	// Set the annotation to commit the initial pass.
	if driver.Annotations == nil {
		driver.Annotations = make(map[string]string)
	}
	driver.Annotations[StorageDriverConfigAppliedAnnotation] = "true"

	// Write the KMS key only if configured.
	if kmsKeyARN == "" {
		return
	}
	driver.Spec.DriverConfig = operatorv1.CSIDriverConfigSpec{
		DriverType: operatorv1.AWSDriverType,
		AWS: &operatorv1.AWSCSIDriverConfigSpec{
			KMSKeyARN: kmsKeyARN,
		},
	}
}
