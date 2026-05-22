# Codebase Information

## Project Identity

- **Name**: HyperShift
- **Repository**: `github.com/openshift/hypershift`
- **Description**: Middleware for hosting OpenShift control planes at scale. Provides cost-effective and time-efficient cluster provisioning with portability across clouds and strong separation between management and workloads.
- **License**: Apache 2.0
- **Language**: Go 1.25.7
- **Module Path**: `github.com/openshift/hypershift` (root), `github.com/openshift/hypershift/api` (API module)

## Multi-Module Structure

The repository contains three Go modules:

| Module | Path | Purpose |
|--------|------|---------|
| Root | `go.mod` | Main operator, CLI, controllers |
| API | `api/go.mod` | CRD type definitions (standalone, vendored by external consumers) |
| Tools | `hack/tools/go.mod` | Build and lint tooling |

The root module consumes the API module via a `replace` directive. After modifying `api/` files, `make update` must be run to revendor.

## Key Technologies

| Category | Technology | Version |
|----------|-----------|---------|
| Language | Go | 1.25.7 |
| Kubernetes APIs | k8s.io/* | v0.35.1 |
| Controller Framework | controller-runtime | v0.19.7 (pinned via replace) |
| CLI | cobra | v1.10.2 |
| Testing | ginkgo/v2, gomega | v2.28.1, v1.39.1 |
| AWS SDK | aws-sdk-go-v2 | v1.41.5 |
| Azure SDK | azure-sdk-for-go | v1.21.1 (azcore) |
| IBM Cloud | power-go-client, vpc-go-sdk | v1.11.0, v0.68.0 |
| Karpenter | sigs.k8s.io/karpenter | v1.9.0 (OpenShift fork) |
| etcd | go.etcd.io/etcd | v3.6.11 |
| OpenShift | openshift/api, library-go | v0.0.0-2026* |

## Binaries Produced

| Binary | Source | Purpose |
|--------|--------|---------|
| `hypershift` | `main.go` | User-facing CLI |
| `hcp` (product-cli) | `product-cli/main.go` | Production CLI subset |
| `hypershift-operator` | `hypershift-operator/main.go` | Management cluster operator |
| `control-plane-operator` | `control-plane-operator/main.go` | Per-cluster control plane operator (multi-binary image) |
| `control-plane-pki-operator` | `control-plane-pki-operator/main.go` | PKI and certificate management |
| `karpenter-operator` | `karpenter-operator/main.go` | Karpenter auto-scaling integration |

## Platform Support

| Platform | Status | Key SDKs |
|----------|--------|----------|
| AWS | Primary | aws-sdk-go-v2, karpenter-provider-aws |
| Azure | Primary | azure-sdk-for-go |
| IBM Cloud (PowerVS) | Supported | power-go-client, vpc-go-sdk |
| KubeVirt | Supported | kubevirt.io/api |
| OpenStack | Feature-gated | openstack-resource-controller |
| GCP | Feature-gated | google.golang.org/api |
| Agent | Supported | cluster-api-provider-agent |

## Container Images

| Image | Build File | Contents |
|-------|-----------|----------|
| hypershift-operator | `Dockerfile` / `Containerfile.operator` | hypershift, hcp, hypershift-operator, karpenter-operator |
| control-plane | `Dockerfile.control-plane` / `Containerfile.control-plane` | control-plane-operator, control-plane-pki-operator |
| cli | `Containerfile.cli` | Cross-platform hcp CLI tarballs |
| shared-ingress | `shared-ingress/Containerfile` | HAProxy v3 shared ingress router |

## Dependency Management

- **Vendoring**: `go mod vendor` enforced (`GOFLAGS=-mod=vendor`)
- **Workspace**: `hack/workspace/go.work` for local multi-module development
- **Renovate**: Configured but currently disabled; full Go updates on main, patch-only on release branches
- **Security**: Snyk (excludes vendor/), gitleaks (secret scanning)
