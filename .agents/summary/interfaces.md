# Interfaces and APIs

## CRD API Surface

### API Groups

| Group | Version | Module |
|-------|---------|--------|
| `hypershift.openshift.io` | v1beta1 | `api/hypershift/v1beta1/` |
| `certificates.hypershift.openshift.io` | v1alpha1 | `api/certificates/v1alpha1/` |
| `scheduling.hypershift.openshift.io` | v1alpha1 | `api/scheduling/v1alpha1/` |
| `karpenter.hypershift.openshift.io` | v1 | `api/karpenter/v1/` |
| `auditlogpersistence.hypershift.openshift.io` | v1alpha1 | `api/auditlogpersistence/v1alpha1/` |

### User-Facing CRDs

| Kind | Group | Scope | Purpose |
|------|-------|-------|---------|
| HostedCluster | hypershift | Namespaced | Top-level hosted cluster resource |
| NodePool | hypershift | Namespaced | Worker node pool configuration (references HostedCluster via `spec.clusterName`) |
| ClusterSizingConfiguration | scheduling | Cluster | T-shirt sizing for hosted control planes (singleton: "cluster") |
| AuditLogPersistenceConfig | auditlogpersistence | Cluster | Audit log persistence settings (singleton: "cluster") |

### Internal/Operational CRDs

| Kind | Group | Scope | Purpose |
|------|-------|-------|---------|
| HostedControlPlane | hypershift | Namespaced | Internal control plane representation (created by hypershift-operator) |
| ControlPlaneComponent | hypershift | Namespaced | Per-component status tracking (v2 framework) |
| AWSEndpointService | hypershift | Namespaced | AWS VPC PrivateLink endpoint service |
| AzurePrivateLinkService | hypershift | Namespaced | Azure Private Link Service |
| GCPPrivateServiceConnect | hypershift | Namespaced | GCP PSC (feature-gated: GCPPlatform) |
| HCPEtcdBackup | hypershift | Namespaced | One-shot etcd backup request (feature-gated: HCPEtcdBackup) |
| CertificateRevocationRequest | certificates | Namespaced | Signer certificate revocation |
| CertificateSigningRequestApproval | certificates, hypershift | Namespaced | CSR approval marker |
| OpenshiftEC2NodeClass | karpenter | Cluster | AWS EC2 node class for Karpenter |

## Internal Go Interfaces

### Platform Interface

```
hypershift-operator/controllers/hostedcluster/internal/platform/platform.go
```

The primary abstraction for multi-cloud support:

| Method | Purpose |
|--------|---------|
| `ReconcileCAPIInfraCR()` | Reconcile CAPI infrastructure custom resource |
| `CAPIProviderDeploymentSpec()` | Generate CAPI provider deployment specification |
| `ReconcileCredentials()` | Copy cloud credentials to HCP namespace |
| `ReconcileSecretEncryption()` | Set up KMS/encryption resources |
| `CAPIProviderPolicyRules()` | Generate RBAC rules for CAPI provider |
| `DeleteCredentials()` | Clean up credentials on deletion |
| `DeleteOrphanedMachines()` | Remove finalizers from provider machines that have been deleted and are no longer associated with a node |

Implementations: `platform/{aws,azure,gcp,ibmcloud,kubevirt,openstack,agent,powervs,none}/`

### ControlPlaneComponent Interface

```
support/controlplane-component/controlplane-component.go
```

| Method | Purpose |
|--------|---------|
| `Name()` | Component identifier |
| `Reconcile(cpContext ControlPlaneContext)` | Reconcile component resources |

Built via fluent API:
```
NewDeploymentComponent("name", opts).WithAdaptFunction(fn).InjectKonnectivityContainer(...).Build()
```

### CreateOrUpdateProvider Interface

```
support/upsert/upsert.go
```

| Method | Purpose |
|--------|---------|
| `CreateOrUpdate(ctx, client, obj, mutate)` | Idempotent resource creation/update |

Implementations: `CreateOrUpdateProvider` (standard), `ApplyProvider` (server-side apply).

### ReleaseProvider Interface

```
support/releaseinfo/
```

| Method | Purpose |
|--------|---------|
| `Lookup(ctx, image, pullSecret)` | Look up release image metadata |

Implementations: `RegistryClientProvider` (registry extraction), `CachedProvider` (caching wrapper), `ProviderWithOpenShiftImageRegistryOverrides` (mirror support).

### ManagementClusterCapabilities Interface

```
support/capabilities/management_cluster_capabilities.go
```

Detects management cluster features: SCC, Routes, Proxy, ImageStreams, etc. Used to conditionally enable controllers and resources.

## Generated Client Packages

All in `client/`:

| Package | Path | Purpose |
|---------|------|---------|
| Typed Clientset | `client/clientset/clientset/` | CRUD operations for all CRD types |
| Informers | `client/informers/externalversions/` | SharedInformerFactory for watch/list caching |
| Listers | `client/listers/` | Read-only cached listers per API group |
| Apply Configs | `client/applyconfiguration/` | Server-side apply configurations |

## CLI Interface

### hypershift CLI (`main.go`)

| Command | Subcommands | Purpose |
|---------|-------------|---------|
| `install` | - | Render manifests to install HyperShift operator |
| `create` | cluster, nodepool, bastion | Create hosted cluster resources |
| `destroy` | cluster, nodepool, bastion | Destroy hosted cluster resources |
| `dump` | cluster | Dump cluster state for debugging |
| `fix` | - | Fix common issues |
| `consolelogs` | - | Stream console logs |
| `kubeconfig` | - | Generate kubeconfig |
| `version` | - | Show version |

### Infrastructure Commands (`cmd/infra/`)

| Command | Platform | Purpose |
|---------|----------|---------|
| `create infra` | AWS, Azure, GCP, PowerVS | Create cloud infrastructure (VPCs, subnets, etc.) |
| `destroy infra` | AWS, Azure, GCP, PowerVS | Destroy cloud infrastructure |
| `create iam` | AWS, Azure, GCP, PowerVS | Create IAM resources |
| `destroy iam` | AWS, Azure, GCP, PowerVS | Destroy IAM resources |

## Validation

### CEL Validation Rules

Primary validation mechanism — no new webhook validation allowed. CEL rules enforce:
- **Immutability**: `self == oldSelf` on fields like `spec.clusterName`, `spec.platform.type`
- **Format validation**: Regex patterns for UUIDs, ARNs, CIDRs, DNS names
- **Cross-field validation**: Mutually exclusive fields, conditional requirements
- **Semantic rules**: Business logic constraints (e.g., "Ingress capability requires Console capability")

Heaviest CEL usage is in `hostedcluster_types.go`, followed by `etcdbackup_types.go`, `karpenter_types.go`, `azureprivatelinkservice_types.go`, and `nodepool_types.go`.

### Feature Gates

Active feature gates controlling CRD types and fields:
- `HCPEtcdBackup`: Gates HCPEtcdBackup CRD and etcd backup fields on HostedCluster
- `GCPPlatform`: Gates GCPPrivateServiceConnect CRD and GCP platform type
- `OpenStack`: Gates OpenStack platform type in NodePoolPlatform
- `ClusterVersionOperatorConfiguration`: Gates CVO configuration fields

Feature-gated enum values use `+openshift:validation:FeatureGateAwareEnum` markers.

## Webhook Endpoints

The hypershift-operator serves admission webhooks:
- Validating webhook for HostedCluster
- Validating webhook for NodePool
- Mutating webhook for HostedCluster defaults

Managed by WebhookCertReconciler. Note: CEL is preferred over webhooks for new validation.

## Metrics

Prometheus metrics exposed via `/metrics` endpoints on all operators. Organized into metric sets:
- **SRE**: Operational metrics for SRE teams
- **Telemetry**: Usage telemetry metrics
- **All**: Complete metric set

ServiceMonitor resources managed by the v2 component framework.
