# Components

## Core Operators

### hypershift-operator
- **Path**: `hypershift-operator/`
- **Binary**: `hypershift-operator`
- **Cluster**: Management cluster (singleton with leader election)
- **Responsibility**: Manages HostedCluster and NodePool lifecycle, creates HCP namespaces, deploys per-cluster CPO instances, reconciles CAPI resources, handles platform-specific infrastructure
- **Controllers**: `hypershift-operator/controllers/` — HostedClusterReconciler, NodePoolReconciler, platform-specific endpoint/link service controllers, scheduling controllers, sizing controllers
- **Entry point**: `hypershift-operator/main.go` with `run` subcommand

### control-plane-operator (CPO)
- **Path**: `control-plane-operator/`
- **Binary**: `control-plane-operator` (multi-binary image)
- **Cluster**: Management cluster (one per HCP namespace)
- **Responsibility**: Orchestrates all control plane components for a single hosted cluster. Uses v2 component framework to manage ~40 components (etcd, KAS, KCM, scheduler, OAuth, CCMs, etc.)
- **Controllers**: `control-plane-operator/controllers/` — HostedControlPlaneReconciler, HealthCheckUpdater, platform-specific private service controllers
- **Entry point**: `control-plane-operator/main.go` with argv[0] dispatch

### control-plane-pki-operator
- **Path**: `control-plane-pki-operator/`
- **Binary**: `control-plane-pki-operator` (shares CPO image)
- **Cluster**: Management cluster (one per HCP namespace)
- **Responsibility**: Certificate rotation, break-glass CSR signing (customer and SRE paths), certificate revocation
- **Framework**: library-go `controllercmd` (not controller-runtime)
- **Entry point**: `control-plane-pki-operator/main.go`

### karpenter-operator
- **Path**: `karpenter-operator/`
- **Binary**: `karpenter-operator`
- **Cluster**: Dual — management cluster for HCP, guest cluster for Karpenter CRDs
- **Responsibility**: Manages Karpenter deployment, syncs EC2NodeClass, handles ignition configs for Karpenter-provisioned nodes, approves machine CSRs
- **Entry point**: `karpenter-operator/main.go`

## CLI Tools

### hypershift CLI
- **Path**: `main.go`, `cmd/`
- **Binary**: `hypershift`
- **Subcommands**: install, create (cluster, nodepool, bastion), destroy (cluster, nodepool, bastion), dump, fix, consolelogs, kubeconfig, version
- **Infrastructure management**: `cmd/infra/` — AWS, Azure, GCP, PowerVS IAM and networking setup

### product-cli (hcp)
- **Path**: `product-cli/`
- **Binary**: `hcp`
- **Purpose**: Production-facing CLI subset. Provides `create` and `destroy` commands for cluster lifecycle. Cross-platform builds (linux/darwin/windows, amd64/arm64/ppc64/s390x).

## Auxiliary Components

### Control Plane Sidecars

| Component | Path | Purpose |
|-----------|------|---------|
| availability-prober | `availability-prober/` | Probes KAS and other endpoints for readiness. Runs as init container or sidecar. Verifies CRD availability and RBAC readiness. |
| token-minter | `token-minter/` | Continuously mints and refreshes ServiceAccount tokens. Runs as sidecar, writes tokens to files with configurable audience and expiration. |
| konnectivity-socks5-proxy | `konnectivity-socks5-proxy/` | SOCKS5 proxy tunneling through Konnectivity server. Resolves hostnames via K8s Service ClusterIPs. |
| konnectivity-https-proxy | `konnectivity-https-proxy/` | HTTPS CONNECT proxy through Konnectivity. Supports upstream proxy chaining and TLS. |
| kubernetes-default-proxy | `kubernetes-default-proxy/` | TCP proxy using HTTP CONNECT for K8s API access through corporate proxies. |
| endpoint-resolver | `control-plane-operator/endpoint-resolver/` | Endpoint resolution for control plane services. |
| metrics-proxy | `control-plane-operator/metrics-proxy/` | Metrics proxying for control plane monitoring. |

### etcd Utilities

| Component | Path | Purpose |
|-----------|------|---------|
| etcd-backup | `etcd-backup/` | Takes etcd snapshots, uploads to S3 via AWS transfer manager. |
| etcd-defrag | `etcd-defrag/` | Controller-runtime based periodic etcd defragmentation. |
| etcd-recovery | `etcd-recovery/` | Disaster recovery: scales down etcd, restores from snapshot, scales back up. |
| etcd-upload | `etcd-upload/` | Uploads etcd snapshots to S3 or Azure Blob (supports MSI for ARO HCP). |

### Infrastructure Components

| Component | Path | Purpose |
|-----------|------|---------|
| ignition-server | `ignition-server/` | HTTP server serving Ignition configs for node bootstrapping. Sub-commands: `start`, `run-local-ignition-provider`. |
| kas-bootstrap | `kas-bootstrap/` | KAS init container: applies CRDs from cluster-config-operator, updates featureGate CR status. |
| dnsresolver | `dnsresolver/` | Init container that resolves DNS names with retries before main workloads start. |
| shared-ingress | `shared-ingress/` | HAProxy v3 container image for multi-tenant shared ingress routing. |
| sharedingress-config-generator | `sharedingress-config-generator/` | Watches HostedControlPlane resources, generates HAProxy config, reloads via Unix socket. |
| sync-fg-configmap | `sync-fg-configmap/` | Syncs feature gate ConfigMaps. |
| sync-global-pullsecret | `sync-global-pullsecret/` | Propagates global pull secrets to hosted clusters. |

### Hosted Cluster Config Operator (HCCO)
- **Path**: `control-plane-operator/hostedclusterconfigoperator/`
- **Runs in**: Guest cluster (not management cluster)
- **Purpose**: Reconciles guest-side OpenShift resources, manages CA bundles, global pull secret propagation, node draining, in-place upgrades, spot remediation, HCP status reporting
- **Controllers**: cmca, drainer, globalps, hcpstatus, inplaceupgrader, machine, node, nodecount, resources, spotremediation

## Support Library

### Resource Management
| Package | Path | Purpose |
|---------|------|---------|
| upsert | `support/upsert/` | Idempotent create-or-update with loop detection |
| config | `support/config/` | Workload configuration: resources, scheduling, security, probes, labels |
| labelenforcingclient | `support/labelenforcingclient/` | Client wrapper enforcing required labels |
| assets | `support/assets/` | Embedded YAML asset reader |

### v2 Component Framework
| Package | Path | Purpose |
|---------|------|---------|
| controlplane-component | `support/controlplane-component/` | Declarative reconciliation framework for CPO components. Builder API for Deployments/StatefulSets/Jobs/CronJobs. Dependency tracking, status conditions, sidecar injection. |

### Cloud Provider Utilities
| Package | Path | Purpose |
|---------|------|---------|
| awsapi | `support/awsapi/` | Delegating client interfaces for AWS SDK services |
| awsutil | `support/awsutil/` | AWS error handling, STS, platform detection |
| azureutil | `support/azureutil/` | Azure annotations, constants, validation |
| gcpapi | `support/gcpapi/` | GCS client interfaces |
| gcputil | `support/gcputil/` | GCP platform utilities |
| openstackutil | `support/openstackutil/` | OpenStack type conversions |

### Networking
| Package | Path | Purpose |
|---------|------|---------|
| netutil | `support/netutil/` | Service exposure strategies (Route, NodePort, LB), IP utilities |
| konnectivityproxy | `support/konnectivityproxy/` | Konnectivity proxy dialer with DNS resolution |
| proxy | `support/proxy/` | HTTP proxy configuration, no_proxy generation |

### Cluster and Release
| Package | Path | Purpose |
|---------|------|---------|
| releaseinfo | `support/releaseinfo/` | OCP release image provider with caching and Cincinnati resolution |
| capabilities | `support/capabilities/` | Management cluster capability detection (SCC, Routes, Proxy) |
| supportedversion | `support/supportedversion/` | OCP version support matrix and validation |
| globalconfig | `support/globalconfig/` | OpenShift global configuration reconcilers |

### Security
| Package | Path | Purpose |
|---------|------|---------|
| pki | `support/pki/` | KAS certificate generation and validation |
| certs | `support/certs/` | TLS certificate generation |
| oidc | `support/oidc/` | OIDC document and JWKS generation |

### Observability
| Package | Path | Purpose |
|---------|------|---------|
| conditions | `support/conditions/` | HostedCluster/NodePool condition helpers |
| events | `support/events/` | Event message formatting |
| metrics | `support/metrics/` | Metrics sets (SRE, Telemetry), platform monitoring |

## Contrib Directory

Community-contributed tools and utilities in `contrib/`:
- `contrib/ai/` — AI-assisted development
- `contrib/ci/` — CI helpers
- `contrib/konflux/` — Konflux integration
- `contrib/managed-azure/`, `contrib/self-managed-azure/` — Azure tooling
- `contrib/metrics/`, `contrib/repo_metrics/` — Metrics analysis
- `contrib/migration/` — Migration tools
- `contrib/oadp-recovery/` — OADP disaster recovery
- `contrib/oidc/` — OIDC utilities
- `contrib/reqserving-setup/` — Request-serving setup
- `contrib/snyk/` — Security scanning
