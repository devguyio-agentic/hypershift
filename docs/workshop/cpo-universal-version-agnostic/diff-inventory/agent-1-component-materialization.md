# Agent-1 — Control-Plane Component Materialization Diff Inventory
**Region:** `control-plane-operator/controllers/hostedcontrolplane/v2/` (v2 component adapters + embedded assets + testdata fixtures)
**Diff semantics:** `git diff upstream/release-5.0..upstream/main` (two-dot; how main's tip differs from 5.0's tip)
**main tip inspected:** `220e0e73a47ee3384e999d30a7dfe87e1e825ea3` (branch `upstream/main`; task referenced `253c3f5c…` but the fetched branch tip was used as directed)
**Mode:** read-only source/diff analysis, no compilation.

> Skew model: the CPO (built from main) renders manifests/flags, but operand **binaries** come from the guest 5.0 payload. Any arg/env/config the main CPO emits that a 5.0-era binary rejects is a skew hazard. Probes/resources/labels/securityContext are evaluated by the kubelet/management cluster and are therefore payload-agnostic (FINE) unless they point at an endpoint/port the binary doesn't serve.

---

## (a) Summary table — changed components × category

| Component | C1 flag/arg/env | C2 comp-set | C3 dep-graph | C5 shape | C10 gating | Net skew |
|---|---|---|---|---|---|---|
| machine_approver | ✅ `--tls-curve-preferences` | | | | | **BREAKS (proven)** |
| configoperator (HCCO) | ✅ `--controllers` += `user-ca-bundle` (IBM) | | | | | BREAKS? (UNCONFIRMED) |
| kube_scheduler | ✅ `--v`; removed static `-v=2` | | | ✅ new readinessProbe | | config `podGroupPostFilter` UNCONFIRMED |
| karpenter-operator | ✅ env rename + Azure env + TOKEN_MINTER_IMAGE | ✅ (predicate/credential changes) | ✅ MonitorOperandsRolloutStatus | ✅ volumes/creds | ✅ Azure gated on standalone flag | UNCONFIRMED |
| kas (kube-apiserver) | ✅ `--v` refactor | ✅ GCP WIF webhook sidecar | ✅ GCP kubeconfig manifest | ✅ startupProbe + probe retune, GCP vol/mounts | ✅ EnableForPlatform(GCP) | FINE (non-GCP); C2 GCP-only |
| cloud_credential_operator | ✅ `--tls-min-version`/`--tls-cipher-suites` | | | | ✅ gated ≥4.23 / ≥5.x | FINE (gated) |
| capi_manager | ✅ `--skip-crd-migration-phases` ×2 | | | | ✅ gated ≥4.20 | FINE (gated; 5.0>4.20) |
| ocm | ✅ `--v` (brand-new adaptDeployment) | | | | | FINE |
| oapi (openshift-apiserver) | ✅ `--v` | | | | | FINE |
| oauth | ✅ `--v` | | | | | FINE |
| oauth_apiserver | ✅ `--v` | | | | | FINE |
| kcm | ✅ `--v` | | | | | FINE |
| etcd | ✅ env `ETCD_METRICS=extensive`, `ETCD_LOG_LEVEL` | | | ✅ resources comment | | FINE/SILENT |
| aws-cloud-controller-manager | ✅ cloud-config `Region = …` | | | | | FINE? (UNCONFIRMED strictness) |
| ignition-server | | | | ✅ service label `app` | | FINE (cosmetic) |
| router | (import refactor only) | | | | | FINE |
| cno | (import refactor only) | | | | | FINE |
| cvo | (import refactor only) | | | | | FINE |
| controlplaneoperator | (import refactor only) | | | | | FINE |
| konnectivity_agent | (import refactor only) | | | | | FINE |
| olm/catalogs | (import refactor only) | | | | | FINE |
| autoscaler, karpenter | (test-only changes) | | | | | N/A |

**Changed component directories with material (non-test) deltas: 21.** Of these, **7 are pure import/package refactors** (`util`→`reconcilerpolicy` / `util`→`imageregistry` / `podspec.KubeconfigKey`→`config.KubeconfigKey` / `manifests`→`cpomanifests`) that materialize **no** operand difference. autoscaler & karpenter changed test files only.

---

## (b) C1 — OPERAND-FLAG / ARG / ENV findings (highest value)

### 1. machine_approver — `--tls-curve-preferences`  ⚠️ **BREAKS (CONFIRMED)**
- File: `machine_approver/deployment.go`
- Added: `tlsArgs = append(tlsArgs, fmt.Sprintf("--tls-curve-preferences=%s", strings.Join(curvePreferences, ",")))` via new `config.CurvePreferences(...)`.
- Exact flag emitted: `--tls-curve-preferences=<comma-separated curveIDs>` (e.g. `--tls-curve-preferences=23,24,25`).
- **SKEW: BREAKS** — the 5.0 cluster-machine-approver binary has no such flag → `unknown flag: --tls-curve-preferences` → crash-loop. This is the already-proven live example.

### 2. configoperator / hosted-cluster-config-operator — `--controllers` += `user-ca-bundle` (IBMCloud only)  ⚠️ **BREAKS? UNCONFIRMED**
- File: `configoperator/deployment.go`
- Change: `--controllers=controller-manager-ca,resources,inplaceupgrader,drainer,hcpstatus` → `…,resources,user-ca-bundle,inplaceupgrader,…`.
- **SKEW: BREAKS-potential (UNCONFIRMED)** — HCCO image ships from the release payload; if the 5.0 HCCO has no `user-ca-bundle` controller registered, an explicit `--controllers` entry naming it typically errors ("unknown controller"). IBMCloud-only path. Could not confirm the 5.0 HCCO controller registry in-region → UNCONFIRMED.

### 3. kube_scheduler — `--v` (and removal of static `-v=2`)  ✅ FINE
- Files: `kube_scheduler/deployment.go` (adds `--v=<n>` from `OperatorConfiguration.KubeScheduler.LogLevel`), `assets/kube-scheduler/deployment.yaml` (removed hard-coded `- -v=2`).
- **SKEW: FINE** — `--v` is a universal klog flag present in every kube-scheduler build.

### 4. kas (kube-apiserver) — `--v` resolution refactor  ✅ FINE
- File: `kas/deployment.go` — `resolveKASVerbosity()` now prefers `OperatorConfiguration.KubeAPIServer.LogLevel`, falls back to the deprecated `KubeAPIServerVerbosityLevelAnnotation`. Still emits only `--v=%d`.
- **SKEW: FINE** — flag name unchanged; universal klog flag.

### 5–8. `--v` added to oapi / oauth / oauth_apiserver / kcm  ✅ FINE
- Files: `oapi/deployment.go`, `oauth/deployment.go`, `oauth_apiserver/deployment.go`, `kcm/deployment.go`.
- Each appends `--v=<n>` sourced from a new `hcp.Spec.OperatorConfiguration.<X>.LogLevel` field via `util.LogLevelToKlogVerbosity`.
- **SKEW: FINE** — `--v` understood by every openshift-apiserver / oauth-server / oauth-apiserver / kube-controller-manager build.

### 9. ocm (openshift-controller-manager) — brand-new `--v`  ✅ FINE
- Files: `ocm/deployment.go` (new file), `ocm/component.go` (adds `.WithAdaptFunction(adaptDeployment)`).
- Appends `--v=<n>` from `OperatorConfiguration.OpenShiftControllerManager.LogLevel`.
- **SKEW: FINE** — klog flag; new C2 wiring (adapter function) but emits only `--v`.

### 10. capi_manager — `--skip-crd-migration-phases` ×2 (version-gated)  ✅ FINE (gated)
- File: `capi_manager/deployment.go`
- Added (only when release `version.GE(config.Version420)` = 4.20.0):
  - `--skip-crd-migration-phases=StorageVersionMigration`
  - `--skip-crd-migration-phases=CleanupManagedFields`
- **SKEW: FINE** — gate protects *older* (<4.20) binaries; a 5.0 guest (5.0 > 4.20) ships a capi binary that supports the flag, and the capi image rides the same payload. C10 gate = built-in skew mitigation.

### 11. cloud_credential_operator — `--tls-min-version` / `--tls-cipher-suites` (version-gated)  ✅ FINE (gated)
- File: `cloud_credential_operator/deployment.go`
- Added only when `version.Major >= 5 || (Major==4 && Minor>=23)`: `config.TLSArgs(...)` → `--tls-min-version=<v>`, `--tls-cipher-suites=<list>`.
- **SKEW: FINE** — explicit C10 guard ("Only … for OCP 4.23+ or 5.x+ where they are actually used"); for a 5.0 guest the gate passes and the 5.0 CCO binary supports these flags.

### 12. etcd — `ETCD_METRICS=extensive` + `ETCD_LOG_LEVEL`  ✅ FINE / SILENT
- Files: `assets/etcd/statefulset.yaml` (adds env `ETCD_METRICS: extensive`), `etcd/statefulset.go` (adds env `ETCD_LOG_LEVEL` from `OperatorConfiguration.Etcd.LogLevel`).
- **SKEW: FINE** — both are long-standing etcd env vars understood by 5.0-era etcd.

### 13. karpenter-operator — env/arg rename + Azure env + TOKEN_MINTER_IMAGE  ⚠️ UNCONFIRMED
- Files: `assets/karpenter-operator/deployment.yaml`, `karpenteroperator/deployment.go`.
- Asset: arg `--namespace=$(MY_NAMESPACE)` → `--namespace=$(NAMESPACE)`; env `MY_NAMESPACE`→`NAMESPACE`, `MY_NAME`→`POD_NAME`.
- deployment.go (standalone/Azure): adds `AZURE_CLIENT_ID`, `AZURE_FEDERATED_TOKEN_FILE`, `AZURE_SUBSCRIPTION_ID`, `AZURE_TENANT_ID`, `AZURE_NODE_RESOURCE_GROUP`, `VNET_SUBNET_ID`, and `TOKEN_MINTER_IMAGE` env.
- **SKEW: UNCONFIRMED** — the `--namespace` flag name is unchanged (the `$()` expansion is k8s-side), so that part is safe; but if the 5.0 karpenter-operator binary reads pod identity from env vars named `MY_NAME`/`MY_NAMESPACE`, the rename would feed it empty/renamed vars. karpenter-operator is a management-side operator whose image rides the payload. Could not confirm the 5.0 binary's env contract in-region.

### 14. aws-cloud-controller-manager — cloud-config `Region = <region>`  ✅ FINE? (UNCONFIRMED strictness)
- Files: `assets/aws-cloud-controller-manager/config.yaml` (adds `Region = %s` to `[Global]`), `cloud_controller_manager/aws/config.go` (passes `hcp.Spec.Platform.AWS.Region` into the template). Confirmed in fixture `zz_fixture_TestConfig.yaml` (`Region = my-region`).
- **SKEW: FINE (likely)** — `Region` is a long-standing AWS cloud-provider INI key; a 5.0 aws-cloud-controller-manager should accept it. Flagged UNCONFIRMED only because INI strict-parse behavior wasn't verified against the 5.0 binary.

### 15. kas GCP workload-identity-federation webhook — new container command flags (GCP only)
- File: `kas/deployment.go` `applyGCPWorkloadIdentityFederationWebhookContainer`.
- New container `gcp-workload-identity-federation-webhook` command: `--annotation-prefix=cloud.google.com`, `--gcp-default-region=<region>`, `--health-probe-bind-address=:8081`, `--kubeconfig=…`, `--metrics-bind-address=127.0.0.1:8080`, `--token-audience=openshift`, plus TLS args (`--cipher-suites=` rewritten to `--tls-cipher-suites=`).
- **SKEW: N/A for 5.0 non-GCP; C2 GCP-only** — GCP platform support is new; a 5.0 payload has no GCP path, so this never materializes against a 5.0 operand on existing platforms. Primarily a C2/C10 addition.

**Total C1 (flag/arg/env) findings: 15.**

---

## (c) C2 / C3 / C5 / C10 findings

### C2 — Component-set / sidecars / init-containers
- **kas:** new sidecar container `gcp-workload-identity-federation-webhook` (GCP platform). `kas/deployment.go`, `kas/component.go`. Also new embedded asset `assets/kube-apiserver/gcp-workload-identity-federation-webhook-kubeconfig.yaml`. → GCP-only; FINE elsewhere.
- **ocm:** new `adaptDeployment` wired via `WithAdaptFunction` (previously no deployment adapter). → FINE.
- **karpenter-operator:** `karpenter-credentials.yaml` manifest now predicated (`karpenterCredentialsSecretEnabled`, AWS-only); adapter container constant `karpenter-adapter` introduced. → management-side; FINE.

### C3 — Dependency-graph / ordering / predicates
- **karpenteroperator/component.go:** adds `.MonitorOperandsRolloutStatus()`; `WithPredicate` now also disables Karpenter on Azure unless `StandaloneKarpenterOperatorEnabled`; credentials secret gated to AWS. → CPO-side reconcile gating, does not reach operand binary → FINE.
- **kas/component.go:** new `WithManifestAdapter("gcp-workload-identity-federation-webhook-kubeconfig.yaml", EnableForPlatform(GCP), ReconcileExisting())`. → C3/C10, GCP-only.

### C5 — Manifest shape (volumes / probes / resources / securityContext / labels)
- **kube-apiserver (`assets/kube-apiserver/deployment.yaml`):** NEW `startupProbe` (`readyz` on port `client`, failureThreshold 36 × 10s); livenessProbe retuned (failureThreshold 6→3, period 180→60, timeout 160→30, dropped initialDelay 300); readinessProbe failureThreshold 18→6. → kubelet-evaluated, endpoints (`livez`, `readyz`) exist in 5.0 KAS → **FINE**.
- **kube-scheduler (`assets/kube-scheduler/deployment.yaml`):** NEW `readinessProbe` `/readyz` on port `10259` (HTTPS). → 5.0 scheduler serves `/readyz` on the secure port → **FINE** (verify-worthy but standard).
- **openshift-oauth-apiserver (`assets/openshift-oauth-apiserver/deployment.yaml`):** `wait-for-etcd` init container rewritten (bash + curl TLS health probe against `etcd-client:2379/health`), added `resources`, `securityContext` (runAsNonRoot, runAsGroup 0, readOnlyRootFilesystem, drop ALL caps), new volumeMounts for etcd certs; also removed static `--v=2` from main container. → runs in the `cli` image via shell script; no operand-binary flag contract → **FINE**.
- **etcd:** resources comment only (no value change). → FINE.
- **ignition-server (`assets/ignition-server/service.yaml`):** added label `app: ignition-server`. → cosmetic → FINE.
- **kas/oapi/oauth_apiserver:** GCP/volume plumbing additions (GCP-gated). → FINE elsewhere.

### C10 — Feature-gate / capability / version gating
- **cloud_credential_operator:** TLS args gated `≥4.23 || ≥5.x` (skew mitigation).
- **capi_manager:** `--skip-crd-migration-phases` gated `≥4.20` (skew mitigation).
- **karpenteroperator:** Azure path gated on `StandaloneKarpenterOperatorEnabled`.
- **kas:** GCP WIF webhook container + kubeconfig gated `EnableForPlatform(GCP)`.
- **kube_scheduler config `podGroupPostFilter`:** new scheduler-config extension point appears unconditionally (from a vendored `k8s.io/kube-scheduler/config/v1` types bump) — see Top Hazards.

---

## (d) TOP skew hazards (ranked)

1. **machine_approver `--tls-curve-preferences=<ids>`** — `machine_approver/deployment.go`. **BREAKS (CONFIRMED)**: 5.0 machine-approver binary rejects the flag → crash-loop. The reference exploit.
2. **configoperator/HCCO `--controllers=…,user-ca-bundle,…`** (IBMCloud) — `configoperator/deployment.go`. **BREAKS-potential (UNCONFIRMED)**: a 5.0 HCCO without the `user-ca-bundle` controller registered will likely reject an explicit `--controllers` entry naming it.
3. **kube_scheduler config extension point `podGroupPostFilter`** — `kube_scheduler/testdata/zz_fixture_*` (source: vendored `k8s.io/kube-scheduler/config/v1`). **UNCONFIRMED**: main renders a `KubeSchedulerConfiguration` profile containing `"podGroupPostFilter": {}`; a 5.0 kube-scheduler may reject/validate this unknown extension point on config decode. Lean SILENT-NOOP (empty, no enabled plugins) but a real unknown-field candidate.
4. **karpenter-operator env rename `MY_NAMESPACE`→`NAMESPACE`, `MY_NAME`→`POD_NAME`** — `assets/karpenter-operator/deployment.yaml`. **UNCONFIRMED**: if the 5.0 karpenter-operator binary reads identity/name from the old env names, the rename breaks it (the `--namespace` flag itself is unchanged, so only the env contract is at risk).
5. **aws-cloud-controller-manager cloud-config `Region = <region>`** — `assets/aws-cloud-controller-manager/config.yaml` / `cloud_controller_manager/aws/config.go`. **UNCONFIRMED (likely FINE)**: new `[Global] Region` INI key; safe if 5.0 AWS CCM parses leniently (it normally does), a BREAK only under strict unknown-key rejection.

> Lower-severity but noted as explicit **skew mitigations already present** in main: CCO TLS args (C10 ≥4.23/5.x) and capi_manager `--skip-crd-migration-phases` (C10 ≥4.20) — both version-gate themselves out for older binaries, so they are FINE for a 5.0 guest.

> All ubiquitous `--v=<n>` klog verbosity additions (kcm, oapi, oauth, oauth_apiserver, ocm, kube_scheduler, kas) are **FINE** — `--v` is understood by every build and never a skew hazard.
