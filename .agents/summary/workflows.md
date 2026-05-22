# Workflows

## Hosted Cluster Lifecycle

### Cluster Creation

```mermaid
sequenceDiagram
    actor User
    participant CLI as hypershift CLI
    participant MgmtK8s as Management Cluster
    participant HO as hypershift-operator
    participant CPO as control-plane-operator
    participant HCCO as hosted-cluster-config-operator
    participant Guest as Guest Cluster

    User->>CLI: hypershift create cluster
    CLI->>MgmtK8s: Create HostedCluster CR
    CLI->>MgmtK8s: Create NodePool CR
    HO->>MgmtK8s: Create HCP namespace
    HO->>MgmtK8s: Create HostedControlPlane CR
    HO->>MgmtK8s: Deploy CAPI resources
    HO->>MgmtK8s: Deploy CPO Deployment
    HO->>MgmtK8s: Deploy PKI Operator
    CPO->>MgmtK8s: Bootstrap PKI
    CPO->>MgmtK8s: Deploy etcd StatefulSet
    CPO->>MgmtK8s: Deploy KAS
    CPO->>MgmtK8s: Deploy KCM, Scheduler
    CPO->>MgmtK8s: Deploy ~40 components
    CPO->>Guest: Deploy HCCO
    HCCO->>Guest: Reconcile guest resources
    HO->>MgmtK8s: Create CAPI MachineDeployments
    MgmtK8s->>Guest: Provision worker nodes
```

### Cluster Deletion

1. User deletes HostedCluster CR (or runs `hypershift destroy cluster`)
2. hypershift-operator finalizer triggers cleanup:
   - Deletes NodePools and waits for machines to drain
   - Deletes HostedControlPlane
   - Cleans up CAPI resources
   - Removes platform-specific infrastructure (endpoint services, etc.)
   - Deletes HCP namespace
3. Platform.DeleteCredentials() cleans cloud credentials

## Control Plane Component Reconciliation (v2 Framework)

```mermaid
flowchart TD
    HCP[HostedControlPlane CR] --> Reconciler[HostedControlPlaneReconciler]
    Reconciler --> Register[registerComponents ~40 components]
    Register --> DepCheck{Dependencies met?}
    DepCheck -->|No| Wait[Wait for deps Available + RolloutComplete]
    DepCheck -->|Yes| LoadManifest[Load embedded YAML manifest]
    LoadManifest --> Adapt[Run AdaptFunction]
    Adapt --> InjectSidecars[Inject Konnectivity/token-minter]
    InjectSidecars --> ApplyDefaults[Apply security context/resources defaults]
    ApplyDefaults --> Upsert[CreateOrUpdate workload]
    Upsert --> UpdateStatus[Update ControlPlaneComponent status]
    UpdateStatus --> Conditions[Set Available/RolloutComplete conditions]
```

## NodePool Lifecycle

```mermaid
stateDiagram-v2
    [*] --> Creating: NodePool CR created
    Creating --> Ready: Machines provisioned
    Ready --> Upgrading: Release image changed
    Upgrading --> Ready: Rollout complete
    Ready --> Scaling: Replicas changed
    Scaling --> Ready: Scale complete
    Ready --> Deleting: NodePool deleted
    Deleting --> [*]: Machines drained
```

**NodePoolReconciler workflow:**
1. Reads NodePool spec
2. Generates ignition config (via ignition-server)
3. Creates/updates platform-specific MachineTemplate (AWSMachineTemplate, AzureMachineTemplate, etc.)
4. Reconciles CAPI MachineDeployment/MachineSet
5. For upgrades: performs rolling update with configurable strategy (Replace or InPlace)
6. Reports status (replicas, version, conditions)

## Certificate Management

### Break-Glass Certificate Flow

```mermaid
sequenceDiagram
    actor SRE
    participant K8s as Management Cluster
    participant PKI as control-plane-pki-operator
    participant KAS as kube-apiserver

    SRE->>K8s: Create CertificateSigningRequest
    PKI->>K8s: Approve CSR (Customer or SRE signer)
    PKI->>K8s: Sign certificate
    SRE->>KAS: Authenticate with signed cert
```

### Certificate Revocation

```mermaid
sequenceDiagram
    actor SRE
    participant K8s as Management Cluster
    participant PKI as control-plane-pki-operator

    SRE->>K8s: Create CertificateRevocationRequest
    PKI->>K8s: Record revocation timestamp
    PKI->>K8s: Rotate signer CA
    PKI->>K8s: Invalidate previous certificates
```

## etcd Operations

### Backup Flow

```mermaid
flowchart LR
    User[User/Automation] -->|Create| Backup[HCPEtcdBackup CR]
    Backup --> Controller[HCPEtcdBackupReconciler]
    Controller -->|Create| Job[Backup Job]
    Job -->|etcdctl snapshot| Snapshot[etcd Snapshot]
    Snapshot -->|Upload| Storage[S3 / Azure Blob]
    Controller -->|Update| Status[BackupCompleted condition + snapshotURL]
```

### Recovery Flow (etcd-recovery)

1. Scale down etcd StatefulSet to 0
2. Clear etcd data directories
3. Download snapshot from storage
4. Restore etcd from snapshot
5. Scale etcd StatefulSet back up

## Development Workflow

### Build and Test

```mermaid
flowchart TD
    Code[Write Code] --> Build[make build]
    Build --> Test[make test]
    Test --> Lint[make lint-fix]
    Lint --> Verify[make verify]
    Verify --> Commit[git commit]
    Commit --> Restructure[restructure-hypershift-commits skill]
    Restructure --> PreCommit[make pre-commit]
    PreCommit --> PR[Create PR in draft mode]
    PR --> CI[Run CI: /test job-name]
    CI --> Review[Mark Ready for Review]
```

### API Change Workflow

```mermaid
flowchart TD
    ModifyAPI[Modify types in api/] --> RunUpdate[make update]
    RunUpdate --> APIDepsBuild[api-deps: revendor API module deps]
    APIDepsBuild --> WorkspaceSync[workspace-sync: sync go.work]
    WorkspaceSync --> Deps[deps: revendor root module]
    Deps --> API[api: regenerate CRDs for all providers]
    API --> APIDocs[api-docs: regenerate API documentation]
    APIDocs --> Clients[clients: regenerate clientsets/listers/informers]
    Clients --> DocsAggregate[docs-aggregate: update published docs]
```

## CI Pipeline

### Pull Request Checks

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| test | PR (code changes) | Sharded unit tests with race detection |
| verify | PR | Full verification (generate, fmt, vet, lint) |
| lint | PR | golangci-lint + kube-api-linter |
| envtest-ocp | PR (API changes) | CRD validation against OCP K8s versions |
| envtest-kube | PR (API changes) | CRD validation against vanilla K8s versions |
| codespell | PR | Spell checking |
| gitlint | PR | Commit message format |
| docs-build | PR (docs changes) | MkDocs build verification |
| cpo-container-sync | PR | CPO Containerfile sync check |

### Konflux/Tekton

Builds container images for:
- hypershift-operator (main branch + tags)
- control-plane-operator (main branch)
- hypershift-cli (mce-50 branch)
- hypershift-release (mce-50 branch)
- shared-ingress (main branch)
- GitHub Actions runner
- gomaxprocs-webhook

## Private Cluster Networking

### AWS PrivateLink Flow

```mermaid
sequenceDiagram
    participant HO as hypershift-operator
    participant CPO as control-plane-operator
    participant AWS as AWS API

    HO->>AWS: Create VPC Endpoint Service
    HO->>AWS: Create VPC Endpoint
    CPO->>HO: Watch for endpoint changes (PrivateServiceObserver)
    HO->>AWS: Configure DNS (Route53)
    Note over HO,AWS: KAS accessible only via PrivateLink
```

### Azure Private Link Flow

Similar pattern with Azure Private Link Services, Private Endpoints, and Private DNS Zones.

### GCP PSC Flow

Similar pattern with GCP Private Service Connect and service attachments.

## Karpenter Auto-Scaling

```mermaid
flowchart TD
    HC[HostedCluster with autoNode enabled] --> KO[karpenter-operator]
    KO -->|Deploy| KarpDeploy[Karpenter in HCP namespace]
    KO -->|Sync| EC2NC[OpenshiftEC2NodeClass guest→mgmt]
    KO -->|Generate| Ignition[Ignition configs for new nodes]
    KO -->|Approve| CSR[Machine CSRs in guest cluster]
    KarpDeploy -->|Auto-provision| Nodes[Worker Nodes]
```
