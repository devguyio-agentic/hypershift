# Dependencies

## Core Framework Dependencies

| Dependency | Version | Purpose |
|-----------|---------|---------|
| `k8s.io/api` | v0.35.1 | Kubernetes core API types |
| `k8s.io/apimachinery` | v0.35.1 | API machinery (runtime, scheme, serialization) |
| `k8s.io/apiextensions-apiserver` | v0.35.1 | CRD support |
| `k8s.io/apiserver` | v0.35.1 | API server libraries |
| `k8s.io/client-go` | v0.35.1 | Kubernetes client |
| `k8s.io/cli-runtime` | v0.35.1 | CLI runtime support |
| `k8s.io/component-base` | v0.35.1 | Component base utilities |
| `k8s.io/kubectl` | v0.35.1 | kubectl libraries |
| `k8s.io/utils` | v0.0.0-20260108 | Kubernetes utilities |
| `sigs.k8s.io/controller-runtime` | **v0.19.7** | Controller framework (pinned down from v0.22.4 via replace due to webhook.Validator breakage) |
| `sigs.k8s.io/yaml` | v1.6.0 | YAML serialization |
| `sigs.k8s.io/structured-merge-diff/v6` | v6.3.1 | Server-side apply support |

## Cluster API Dependencies

| Dependency | Version | Notes |
|-----------|---------|-------|
| `sigs.k8s.io/cluster-api` | v1.10.4 | Replaced with `csrwng/cluster-api` fork for K8s v0.34+ fuzzer compatibility |
| `sigs.k8s.io/cluster-api-provider-aws/v2` | v2.8.2 | AWS CAPI provider |
| `sigs.k8s.io/cluster-api-provider-azure` | v1.21.0 | Azure CAPI provider |
| `sigs.k8s.io/cluster-api-provider-gcp` | v1.10.0 | GCP CAPI provider |
| `sigs.k8s.io/cluster-api-provider-ibmcloud` | v0.11.0 | IBM Cloud CAPI provider |
| `sigs.k8s.io/cluster-api-provider-kubevirt` | v0.1.9 | KubeVirt CAPI provider |
| `sigs.k8s.io/cluster-api-provider-openstack` | v0.12.1 | OpenStack CAPI provider |

## Cloud Provider SDKs

### AWS
| Dependency | Version |
|-----------|---------|
| `aws/aws-sdk-go-v2` (core) | v1.41.5 |
| `aws/aws-sdk-go-v2/service/ec2` | v1.279.2 |
| `aws/aws-sdk-go-v2/service/iam` | v1.53.2 |
| `aws/aws-sdk-go-v2/service/route53` | v1.62.1 |
| `aws/aws-sdk-go-v2/service/s3` | v1.97.3 |
| `aws/aws-sdk-go-v2/service/sts` | v1.41.10 |
| `aws/aws-sdk-go-v2/service/kms` | v1.50.0 |
| `aws/aws-sdk-go-v2/service/elb*` | v1.29.6 / v1.54.7 |
| `aws/aws-sdk-go-v2/service/sqs` | v1.42.21 |
| `aws/karpenter-provider-aws` | v1.8.6 (replaced with OpenShift fork) |

### Azure
| Dependency | Version |
|-----------|---------|
| `Azure/azure-sdk-for-go/sdk/azcore` | v1.21.1 |
| `Azure/azure-sdk-for-go/sdk/azidentity` | v1.13.1 |
| `Azure/azure-sdk-for-go/sdk/resourcemanager/*` | Various (auth v2.2.0, dns v1.2.0, network v5.2.0, etc.) |
| `Azure/azure-sdk-for-go/sdk/storage/azblob` | v1.7.0 |
| `Azure/msi-dataplane` | v0.4.3 |

### IBM Cloud
| Dependency | Version |
|-----------|---------|
| `IBM-Cloud/power-go-client` | v1.11.0 |
| `IBM/go-sdk-core/v5` | v5.19.1 |
| `IBM/vpc-go-sdk` | v0.68.0 |
| `IBM/networking-go-sdk` | v0.51.4 |
| `IBM/ibm-cos-sdk-go` | v1.12.2 |

### GCP
| Dependency | Version |
|-----------|---------|
| `google.golang.org/api` | v0.279.0 |

## OpenShift Dependencies

| Dependency | Version | Purpose |
|-----------|---------|---------|
| `openshift/api` | v0.0.0-20260416 | OpenShift API types |
| `openshift/client-go` | v0.0.0-20260416 | OpenShift client |
| `openshift/library-go` | v0.0.0-20251204 | Shared library (PKI operator uses this controller framework) |
| `openshift/cloud-credential-operator` | v0.0.0-20250225 | Cloud credential management |
| `openshift/cluster-api-provider-agent/api` | v0.0.0-20250624 | Agent platform CAPI types |
| `openshift/cluster-autoscaler-operator` | v0.0.1-0.20241204 | Autoscaler types |
| `openshift/cluster-node-tuning-operator` | v0.0.0-20250225 | Node tuning types |
| `openshift/multi-operator-manager` | v0.0.0-20260112 | Multi-operator management |

## Infrastructure Dependencies

| Dependency | Version | Purpose |
|-----------|---------|---------|
| `go.etcd.io/etcd/*` | v3.6.11 | etcd server/client |
| `sigs.k8s.io/karpenter` | v1.9.0 | Node auto-scaling (OpenShift fork) |
| `kubevirt.io/api` | v1.8.2 | KubeVirt virtualization |
| `k-orc/openstack-resource-controller` | v1.0.0 | OpenStack resources (replaced, lives in CAPO) |
| `coreos/ignition/v2` | v2.25.1 | Node bootstrapping configs |

## Observability Dependencies

| Dependency | Version | Purpose |
|-----------|---------|---------|
| `prometheus/client_golang` | v1.23.2 | Prometheus metrics |
| `prometheus/client_model` | v0.6.2 | Prometheus data model |
| `prometheus-operator/prometheus-operator` | v0.88.0 | Monitoring CRDs |
| `go-logr/logr` | v1.4.3 | Structured logging |
| `go-logr/zapr` | v1.3.0 | Zap logging backend |

## Testing Dependencies

| Dependency | Version | Purpose |
|-----------|---------|---------|
| `onsi/ginkgo/v2` | v2.28.1 | BDD test framework |
| `onsi/gomega` | v1.39.1 | Assertion library |
| `stretchr/testify` | v1.11.1 | Test assertions |
| `go.uber.org/mock` | v0.6.0 | Mock generation |
| `google/go-cmp` | v0.7.0 | Value comparison |
| `google/gofuzz` | v1.2.0 | Fuzz testing |

## CLI and Utilities

| Dependency | Version | Purpose |
|-----------|---------|---------|
| `spf13/cobra` | v1.10.2 | CLI framework |
| `blang/semver` | v3.5.1 | Semantic versioning |
| `google/cel-go` | v0.26.1 | CEL expression evaluation |
| `google/uuid` | v1.6.0 | UUID generation |
| `clarketm/json` | v1.17.1 | JSON utilities |
| `evanphx/json-patch/v5` | v5.9.11 | JSON Patch operations |
| `distribution/reference` | v0.6.0 | Container image references |

## Notable Replace Directives

| Module | Replaced With | Reason |
|--------|--------------|--------|
| `sigs.k8s.io/controller-runtime` | v0.19.7 | webhook.Validator deprecation breakage in v0.20 |
| `sigs.k8s.io/cluster-api` | `csrwng/cluster-api` fork | K8s API v0.34+ fuzzer compatibility |
| `sigs.k8s.io/karpenter` | `openshift/karpenter` fork | OpenShift-specific patches |
| `aws/karpenter-provider-aws` | `openshift/aws-karpenter-provider-aws` fork | OpenShift-specific patches |
| `github.com/golang-jwt/jwt/v4` | v4.5.2 | CVE-2025-30204 fix |
| `github.com/openshift/hypershift/api` | `./api` | Local multi-module development |

## Build Tooling (hack/tools/ module)

| Tool | Source | Purpose |
|------|--------|---------|
| controller-gen | `sigs.k8s.io/controller-tools` (OpenShift fork) | CRD and deepcopy generation |
| codegen | `openshift/api/tools/codegen/cmd` | OpenShift API code generation |
| golangci-lint | `golangci/golangci-lint/v2` (v2.11.4) | Linting |
| kube-api-linter | `sigs.k8s.io/kube-api-linter/pkg/plugin` | API convention enforcement (Go plugin) |
| staticcheck | `honnef.co/go/tools/cmd/staticcheck` | Static analysis |
| mockgen | `go.uber.org/mock/mockgen` | Mock generation |
| yq | `mikefarah/yq/v4` | YAML processing |
| setup-envtest | `sigs.k8s.io/controller-runtime/tools/setup-envtest` | envtest binary management |
| gotestsum | `gotest.tools/gotestsum` (v1.13.0) | Test output formatting |
