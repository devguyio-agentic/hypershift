# Agent B2 (4.20 floor) — Behavioral diff inventory: component adapt logic + maintenance subprocesses + PKI protocol

**Scope:** `control-plane-operator/controllers/hostedcontrolplane/v2/**` (adapt/predicate/gating logic), the maintenance/helper subprocess algorithms under `control-plane-operator/` + shared `support/`/`pkg/etcdcli/` libs, and `control-plane-pki-operator/**`.

**Diff basis:** `git diff upstream/release-4.20..upstream/main` (two-dot).
- release-4.20 tip = `48fd596611`
- main tip = `220e0e73a4`

**Method:** read `.go` decision logic only. Ignored embedded YAML assets, `zz_fixture`/testdata, and `*_test.go`. Logic read, not compiled.

**Skew classes:** SELF-CONTAINED / ASSUMES-NEWER-GUEST / ASSUMES-NEWER-OPERAND / COORDINATION-RISK — **against a 4.20 FLOOR** (operand/guest ≈ 5 minors behind main=5.1).

**Comparison baseline:** the 5.0-run findings in `../diff-inventory/agent-B2-behavioral-components-and-protocols.md`.

---

## 0. Headline conclusions — the DELTA vs 5.0

| Area | 5.0 → main verdict (baseline) | **4.20 → main verdict (this run)** |
|---|---|---|
| **PKI operator protocol** | **UNCHANGED** (import-refactor only) | **CHANGED — real state-machine/protocol change** |
| **etcd defrag** | UNCHANGED | **core algorithm UNCHANGED** (only a new configurable leader-election-id) |
| **etcd backup** | UNCHANGED | **CHANGED — AWS SDK v1→v2 rewrite + cert-path + tag-encoding + new fetchcerts.go** |
| **etcd restore** | UNCHANGED | UNCHANGED |
| **pkg/etcdcli health/helpers** | n/a | **CHANGED — removed `IsQuorumFaultTolerantErr`, deleted `helpers.go`, etcd import path** |
| **token-minter container injection** | UNCHANGED (test-only) | **CHANGED — AWS→AWS+Azure+GCP + native-sidecar init-container pattern** |
| **availability-prober / kas-bootstrap / sync-global-pullsecret** | UNCHANGED | **UNCHANGED** (no non-test subprocess-dir diffs) |
| **konnectivity proxy dialer/resolver** | CHANGED (multi-IP fallback + 90s budget) | **SAME CHANGE present** (multi-IP fallback + `overallDialBudget`=90s + `maxFallbacks`) |
| **Per-component LogLevel `--v`** | new (gated field) | **SAME** (per-component `OperatorConfiguration.*.LogLevel`, FG=`HCPUserFacingOperatorLogs`) |
| **Component adapt breadth** | moderate | **much wider**: KAS config PSa default flip, structured-auth parity, KMS/secret-encryption convergence, GCP webhook, 5 new v2 components, etcd sharding |

**Bottom line:** At the 4.20 floor, **two things that were safe no-ops at 5.0 are now live behavioral movers: the PKI revocation protocol and the etcd backup algorithm.** Both apply **ungated** (not version-guarded), so a manifest diff misses them entirely and they execute against a 4.20 guest unconditionally.

---

## (a) Per-component behavioral-logic changes (.go logic only)

### KAS — `kas/config.go:adaptKubeAPIServerConfig` / `generateConfig`
- **PodSecurity admission default flipped** `Enforce: "privileged"` → `Enforce: "restricted"` in the static `PodSecurityConfiguration`. (Still overridden back to `privileged` when the `OpenShiftPodSecurityAdmission=true` FG is absent, so net effect only changes when that gate is off.) | **COORDINATION-RISK** — the compiled-in default now assumes restricted; divergence only manifests on clusters without the OSPSa gate.
- **TLS serving config path changed** — removed inline `CipherSuites`/`MinTLSVersion` and now calls new `hcpconfig.ApplyServingInfoFromTLSProfile(&config.ServingInfo.ServingInfo, p.TLSSecurityProfile)` (`ApplyServingInfoFromTLSProfile` is **new in main, absent @4.20**). | **SELF-CONTAINED** (CPO computes serving info; emitted into KAS config the operand parses).
- **New `kubernetesVersionForFeatureGates()` read** — when `DRADeviceTaintRules=true` FG present, reads `ReleaseImageProvider.ComponentVersions()["kubernetes"]`; if kube `1.36` adds `runtime-config=resource.k8s.io/v1beta2=true` (replaces the old `MutatingAdmissionPolicy→v1alpha1` branch). | **ASSUMES-NEWER-OPERAND** — only emits v1beta2 runtime-config at k8s 1.36 (self-tracks operand version); a 4.20 guest (older k8s) does not hit the branch.
- **New `etcd-servers-overrides` arg** when `p.EtcdServersOverrides` non-empty (feeds the etcd-sharding work). | **SELF-CONTAINED / COORDINATION-RISK** w.r.t. sharding.
- `util.ConfigOAuthEnabled`→`reconcilerpolicy.ConfigOAuthEnabled` refactor (value-identical).

### KAS — `kas/auth.go` (structured external-OIDC authentication config)
- **New feature-gated protocol branch** `generateUsernameClaimMapping` now dispatches on `featuregates.Gate().Enabled(featuregates.ExternalOIDCWithUpstreamParity)` → `…WithParity` vs `…Legacy`. Parity path adds strict validation (claim XOR expression, prefix-policy rules) and emits CEL-expression mappings. | **ASSUMES-NEWER-OPERAND** — `ExternalOIDCWithUpstreamParity` is **enabled in the Default featureset @main** and produces an `AuthenticationConfiguration` (expression mappings + `UserValidationRules`) that an older KAS structured-auth parser may reject. **This FG does not exist @4.20.**
- **New `generateUserValidationRules` / `generateGroupsClaimMapping`** — emits `out.UserValidationRules` (new field) under the same gate. | **ASSUMES-NEWER-OPERAND.**
- **New exported `GenerateAuthConfig(...)`** wrapper. | SELF-CONTAINED.

### KAS — `kas/secretencryption.go` + `kas/kms.go` + `kas/kms/azure.go`
- **New convergence-detection algorithm** (`isKASConverged`, `readCurrentEncryptionConfig`, `deriveAESCBCEncryptionConfig`, `getKMSAPIVersion`): the adapt now **reads the live KAS Deployment and the live encryption-config secret** and only rotates/derives keys when the running KAS has converged on the expected config hash (`secretencryption.KASDeploymentConvergedWithEncryptionConfig`). Previously the config was rendered statelessly. | **COORDINATION-RISK** — new imperative reads of management-cluster state gate key derivation; mis-sequencing during encryption-config rollout differs from 4.20.
- **Azure KMS token-minter integration** (`buildKASContainerAzureKMSTokenMinter`, `kasVolumeAzureKMSCloudToken`, `AzureKMSProviderName(key, keyVaultType)` now returns error + takes vault type). | **ASSUMES-NEWER-OPERAND** (Azure KMS path).

### KAS — `kas/deployment.go`
- **GCP workload-identity-federation webhook sidecar** (`applyGCPWorkloadIdentityFederationWebhookContainer`, `applyPlatformSpecificIdentityWebhookContainers`) — injects `/usr/bin/gcp-workload-identity-federation-webhook` on `GCPPlatform`. **Same change as the 5.0 run.** | **ASSUMES-NEWER-OPERAND** (GCP payload must ship the binary).
- **Verbosity** — old `updateMainContainer` annotation-based `--v` replaced by `resolveKASVerbosity` reading `hcp.Spec.OperatorConfiguration.KubeAPIServer.LogLevel`. **Same as 5.0.** | **SELF-CONTAINED** (default preserves `--v=2`; note panic-on-unknown mapper).

### Verbosity siblings (same as 5.0 run)
`kcm`, `kube_scheduler`, `oapi`, `oauth`, `oauth_apiserver`, `ocm` all gain `resolve<Comp>Verbosity` reading the per-component `OperatorConfiguration.*.LogLevel`; `etcd/statefulset.go` sets `ETCD_LOG_LEVEL`. The per-component `LogLevel` fields are **new @main (FG `HCPUserFacingOperatorLogs`), absent @4.20**. | **SELF-CONTAINED** — CPO-owned HCP field, default→`--v=2`/`info`; new read-of-HCP-state a manifest diff can't attribute. Shared mapper `panic`s on unknown enum (new hard-fail surface).

### CCO — `cloud_credential_operator/deployment.go:adaptDeployment`
- Parses `ReleaseImageProvider.Version()`; appends `config.TLSArgs(...)` **only when `major>=5 || (major==4 && minor>=23)`**; errors if unparseable. | **ASSUMES-NEWER-OPERAND**, but the gate **reads the operand's own version** → **self-tracking**. **At a 4.20 guest the gate does NOT fire (4.20 < 4.23) → no TLS args → inert/safe.** (At 5.0 it fired.)

### CAPI manager — `capi_manager/deployment.go:adaptDeployment`
- `version.GE(config.Version419)` → `--feature-gates=MachineSetPreflightChecks=false` (fires @4.20).
- `major>=5 || minor>=23` → TLS args (does **not** fire @4.20).
- **`version.GE(config.Version420)` → `--skip-crd-migration-phases=StorageVersionMigration` and `=CleanupManagedFields`.** `config.Version420` is **new @main, absent @4.20**. | **ASSUMES-NEWER-OPERAND.** **This gate sits exactly at the 4.20 floor and FIRES for a 4.20 guest** (4.20 ≥ 4.20) → main's CPO emits both skip-crd-migration flags to the 4.20 CAPI manager. Because the gate reads the operand's own version, the 4.20 capi-provider binary is expected to support the flag (introduced at 4.20); **UNCONFIRMED** that the exact 4.20 binary accepts both phase values.

### machine-approver — `machine_approver/deployment.go:adaptDeployment`
- New: computes `config.CurvePreferences(cpContext, profile)` (**function new @main, absent @4.20**) and appends `--tls-curve-preferences=<ids>` plus `config.TLSArgs(...)`. **Ungated** (no version guard). | **ASSUMES-NEWER-OPERAND** — the machine-approver binary must accept `--tls-curve-preferences`/`--tls-*`; applied unconditionally regardless of guest version, so a 4.20 operand that doesn't recognize the flag would reject it. Higher risk than CCO/CAPI because there is **no version gate**.

### New v2-migrated / new components (predicate gating a manifest diff sees but logic it doesn't)
- **`endpoint_resolver/component.go:Predicate`** (new) — deploys only when `!DisableMonitoringServices && Spec.Monitoring.MetricsForwarding.Mode == Forward`. `metrics_proxy` reuses this predicate. | **SELF-CONTAINED** (reads HCP spec); **COORDINATION-RISK** if 4.20 HCP lacks the `Monitoring.MetricsForwarding` field shape.
- **`cloud_controller_manager/gcp/{component,config}.go`** (new) — GCP CCM gated `Platform.Type == GCPPlatform`. | ASSUMES-NEWER-OPERAND (GCP).
- **`awsnodeterminationhandler/*`** (new) — gated AWS + `AWS.RolesRef.NodePoolManagementARN != "" ` and reads `AWS.TerminationHandlerQueueURL`. | SELF-CONTAINED (AWS).
- **`kube_storage_version_migrator/component.go`** (new) — gated off for `IBMCloud`/`PowerVS`. | SELF-CONTAINED.
- **`etcd/shard.go:NewShardComponent`** (new) — per-shard StatefulSet component; `needsManagementKASAccess = shard.Replicas>=3`; skips PDB for single-replica shards; predicate `isManagedETCD`. Reads new `ManagedEtcdShardSpec` (**new API, absent @4.20**). **TODO in source: shard PVCs are NOT backed up.** | **COORDINATION-RISK / ASSUMES-NEWER-GUEST-API** — only active under TechPreview EtcdSharding; a manifest diff sees the StatefulSet but not the un-backed-up data hazard.

### Karpenter / other (carried from 5.0, confirmed present @4.20→main)
Karpenter Azure-standalone gating + credentials-secret AWS-only split, AWS CCM positional `Region` arg, HCCO `user-ca-bundle` on IBM — all present; same classes as the 5.0 run.

---

## (b) Maintenance / helper subprocess algorithm changes

| Subprocess | 4.20→main verdict | Evidence |
|---|---|---|
| **etcd defrag** (`etcd-defrag/`) | **core UNCHANGED** | Only new `--leader-election-id` flag (default unchanged) to allow per-shard defrag leases + a ctx-param lint. Member-selection / leader-last / learner-exclusion / fragmentation logic untouched. |
| **etcd backup** (`etcd-backup/etcdbackup.go` + new `fetchcerts.go`) | **CHANGED** | (1) **AWS SDK v1→v2**: `session.NewSession`/`s3manager.Uploader` → `config.LoadDefaultConfig` + `transfermanager.New(s3.NewFromConfig)`. (2) **Cert path default** `/etc/etcd/tls/{client,etcd-ca}/…` → `/etc/etcd-certs/…` (`DefaultCertsDir`). (3) **Tag encoding** ad-hoc `k=v&` → `url.Values.Encode()` (URL-encoded + alphabetically sorted → deterministic). (4) Flags now default to the struct values instead of empty. (5) New `fetchcerts.go` (135 lines). |
| **pkg/etcdcli** (`health.go`, `etcdcli.go`, `helpers.go`) | **CHANGED** | Removed `IsQuorumFaultTolerantErr` (−17, dead quorum-check variant). `helpers.go` **deleted** (−256; fake client moved out of prod pkg). etcd import `etcdserver`→`etcdserver/errors` (`ErrLearnerNotReady`). `%v`→`%w` wrapping. Health/quorum decision logic (`IsQuorumFaultTolerant`, `IsClusterHealthy`) **unchanged**. |
| **token-minter container** (`support/controlplane-component/token-minter-container.go`) | **CHANGED — injection algorithm** | Cloud-token minting extended **AWS → AWS + self-managed Azure + GCP** (`azureutil.IsSelfManagedAzure`, `GCPPlatform`). New `injectContainer()`: OneShot→init container; else if `NativeSidecarContainersEnabled` (k8s≥1.29)→init container w/ `RestartPolicy=Always` + StartupProbe (`test -f <CloudTokenMountPath>/token`, period 1s, threshold 30); else fallback sidecar. Mount path const `/var/run/secrets/openshift/serviceaccount` → `config.CloudTokenMountPath`. | **ASSUMES-NEWER-GUEST** (native sidecars need k8s≥1.29 on the management cluster) + **ASSUMES-NEWER-OPERAND** (Azure/GCP token-minter paths). |
| **availability-prober** | **UNCHANGED** | No non-test diff. |
| **kas-bootstrap** | **UNCHANGED** | No non-test diff. |
| **sync-global-pullsecret** (`globalps/`) | **UNCHANGED** | No non-test subprocess-logic diff. |
| **konnectivity proxy dialer/resolver** (`support/konnectivityproxy/`) | **CHANGED — same as 5.0** | `dialer.go` has `overallDialBudget` (90s), `buildAddressList`, `maxFallbacks` loop; `resolver.go` has `resolveAllIPs` + `contextWithFallbackIPs`. Identical to the 5.0-run finding → multi-IP fallback + 90s budget present at the 4.20 floor too. | **COORDINATION-RISK** (retry count 1→≤4, timeout envelope 60s→90s). |

---

## (c) PKI operator protocol — step-by-step DELTA (the big flip vs 5.0)

**Protocol verdict: CHANGED between 4.20 and main.** (At 5.0 it was import-refactor only; the requeue/timeout rework below is **not** in the 5.0→main diff — confirmed `requeueInterval`/`revocationVerificationTimeout` absent from the 5.0..main diff.)

### `certificaterevocationcontroller/certificaterevocationcontroller.go` (+83/−35) — revocation state machine
1. **Requeue mechanism replaced.** Old: `requeue==true` → return `factory.SyntheticRequeueError`. New: return `nil` and `syncContext.Queue().AddAfter(key, requeueInterval)` with **`requeueInterval = 10s` fixed**. Rationale in-code: `SyntheticRequeueError` feeds `DefaultControllerRateLimiter` (5ms→1000s exponential), causing **11+ minute stalls** while polling for KAS trust-bundle reloads (~1–2 min); returning `nil` also triggers `Forget(key)` resetting the failure counter. | **COORDINATION-RISK** — materially changes revocation convergence latency and requeue cadence; invisible to a manifest diff.
2. **New terminal timeout in step 6** (`ensureOldSignerCertificateRevoked`): if the `LeafCertificatesRegenerated=True` condition is older than **`revocationVerificationTimeout = 10m`**, it writes `PreviousCertificatesRevoked=False` with reason `RevocationVerificationTimeout`, emits `CertificateRevocationStalled`, and **returns a real error** instead of polling forever. | **new imperative action / COORDINATION-RISK** — a 4.20-based environment where KAS reload is slow now surfaces a hard failure condition that did not exist.
3. **State-machine branches flipped `requeue=false → requeue=true`** in both `ensureNewSignerCertificatePropagated` (step 3) and `ensureOldSignerCertificateRevoked` (step 6): when the signer secret / `total-client-ca` trust bundle / signer-class secret is **not yet present**, the controller now **requeues** (with new V(2) log lines) instead of silently returning "done". Also the `secretForSignerClass` miss now returns `requeue=true`. | **COORDINATION-RISK** — the revocation now correctly waits for propagation instead of prematurely completing; different end-state timing vs 4.20.
4. **Error handling hardened** — apply errors for CRR/Secret/ConfigMap are now wrapped and returned (previously early-returned raw err before applying other actions); error returned *after* applying the action.
5. Import refactor `hcpmanifests.KubeAPIServerServiceName` → `cpomanifests.KubeAPIServerServiceName`, `supportutil.IsPodReady/ContainerPort` → `podspec.*` (value-identical; same per-pod `verifyCertificateAgainstAllKASPods` live-verify, replica-count guard, SNI override, `perPodVerifyTimeout=10s`).

### `certificatesigningcontroller/certificatesigningcontroller.go` — CSR signing
- **Event-emission bug fix with behavioral effect:** `if err != nil && validationErr == nil` → **`if err == nil && validationErr == nil`** for the `CertificateSigningRequestFulfilled` event. @4.20 the "fulfilled" event fired only on apply **error** (wrong); now fires on success. | **SELF-CONTAINED** (observability/eventing correctness change).
- `%v`→`%w` error wrapping in `ParseCSR`/`sign`.

### Other PKI files
- `certificatesigningrequestapprovalcontroller.go` — event message typo fix (`"%q in is approved"`→`"%q is approved"`); approval decision logic unchanged.
- `targetconfigcontroller.go`, `topology/detector.go`, `certificates/validation.go` — `%v`→`%w` / import `hypershift-operator/.../manifests`→`pkg/manifests` (value-identical). CSR validation conditions unchanged.

**Net:** trust-propagation **order and verify logic are preserved**, but the **requeue algorithm, the new 10-minute revocation-verification timeout terminal state, and several wait-vs-complete branch flips are genuine protocol/behavioral changes** that execute against a 4.20 guest unconditionally (no version gate).

---

## (d) DELTA vs 5.0 — explicit

- **PKI: UNCHANGED@5.0 → CHANGED@4.20.** The revocation-controller requeue overhaul + `RevocationVerificationTimeout` + step-3/step-6 branch flips landed between 4.20 and 5.0, so they are invisible in the 5.0→main diff but **live in the 4.20→main diff**. (Answer to KEY QUESTION 1: **yes, PKI protocol changed 4.20→main.**)
- **etcd maintenance: UNCHANGED@5.0 → PARTIALLY CHANGED@4.20.** Defrag core still unchanged, but **etcd-backup** (AWS SDK v1→v2, cert path, tag encoding, fetchcerts.go) and **pkg/etcdcli** (removed quorum helper, deleted helpers.go) changed between 4.20 and 5.0. (Answer to KEY QUESTION 2: **etcd *backup* changed 4.20→main; defrag/restore did not.**)
- **token-minter: UNCHANGED@5.0 → CHANGED@4.20** (AWS→AWS+Azure+GCP + native-sidecar init-container algorithm).
- **konnectivity dialer / GCP webhook / per-component LogLevel: SAME as 5.0** (present at both floors).
- **Component breadth: WIDER@4.20** — KAS PSa-default flip, structured-auth parity FG, KMS/secret-encryption convergence reads, 5 new v2 components, etcd sharding — all additional movement vs the 5.0 run.

### Gates that flip FINE@5.0 → BREAKS@4.20
All version gates found read **`ReleaseImageProvider.Version()` / `ComponentVersions()`** — i.e. the **operand's own release version** — so they **self-track the operand** rather than diverging from it:

| Gate (file:func) | Threshold | Fires @4.20 guest? | 5.0 vs 4.20 outcome |
|---|---|---|---|
| CCO TLS args (`cloud_credential_operator/deployment.go`) | `>=4.23 \|\| >=5` | **NO** (4.20<4.23) | Fired@5.0 (ASSUMES-NEWER-OPERAND); **INERT@4.20** → *safer*, not broken |
| CAPI TLS args (`capi_manager/deployment.go`) | `>=4.23 \|\| >=5` | **NO** | Fired@5.0; **INERT@4.20** |
| CAPI `--skip-crd-migration-phases` (`capi_manager/deployment.go`) | `>=4.20` | **YES** (at the floor) | Fires@5.0 and @4.20; emits to the 4.20 capi-provider — operand assumed to support it (gate==operand version). **UNCONFIRMED** binary acceptance |
| CAPI `MachineSetPreflightChecks=false` (`capi_manager/deployment.go`) | `>=4.19` | **YES** | Fires at both |
| KAS DRADeviceTaintRules runtime-config (`kas/config.go`) | k8s `==1.36` | **NO** (4.20 k8s older) | Fires only at k8s 1.36 operand |

**Count of version-gated flags that independently flip FINE@5.0 → BREAKS@4.20: 0 (confirmed).** Because these gates read the operand's own version, they are **self-consistent**; the TLS gates actually go **inert** at 4.20 (floor 4.23 > 4.20). The genuine 4.20 hazards are the **UNGATED** new behaviors that run regardless of guest version — PKI revocation requeue/timeout, etcd backup rewrite, token-minter injection, machine-approver `--tls-curve-preferences` (no version guard → **the one flag-level item that can break a 4.20 operand unconditionally**), KAS PSa-default flip, KAS structured-auth parity (FG default-enabled @main, FG absent @4.20).

---

## (e) Ranked top behavioral hazards a manifest-diff MISSES (4.20 floor)

1. **PKI revocation protocol rework** — `certificaterevocationcontroller.go:syncCertificateRevocationRequest` / `ensureOldSignerCertificateRevoked` | **COORDINATION-RISK.** SyntheticRequeueError→fixed-10s `AddAfter`, new 10-min `RevocationVerificationTimeout` terminal failure, step-3/step-6 wait-vs-complete flips. Ungated; executes against any 4.20 guest. **New vs the 5.0 run.** Pure Go — invisible in YAML.
2. **etcd backup algorithm rewrite** — `etcd-backup/etcdbackup.go:uploadToS3` / `mapToTags` (+ new `fetchcerts.go`) | **COORDINATION-RISK / ASSUMES-NEWER-OPERAND.** AWS SDK v1→v2, cert-dir path change (`/etc/etcd-certs/`), URL-encoded sorted S3 tags. If the 4.20 etcd pod still mounts certs at the old path or IAM/tag expectations differ, backups silently change behavior. **New vs the 5.0 run.**
3. **machine-approver `--tls-curve-preferences` (UNGATED)** — `machine_approver/deployment.go:adaptDeployment` | **ASSUMES-NEWER-OPERAND.** `config.CurvePreferences` is new @main; the flag is appended with **no version guard**, so a 4.20 machine-approver binary that doesn't recognize it is fed an unknown flag.
4. **token-minter injection algorithm** — `support/controlplane-component/token-minter-container.go:injectContainer` | **ASSUMES-NEWER-GUEST (+OPERAND).** Native-sidecar init-container + StartupProbe path assumes k8s≥1.29 on the mgmt cluster; Azure/GCP cloud-token paths assume newer operands. **New vs the 5.0 run.**
5. **KAS structured-auth upstream-parity branch** — `kas/auth.go:generateUsernameClaimMapping` / `generateUserValidationRules` | **ASSUMES-NEWER-OPERAND.** `ExternalOIDCWithUpstreamParity` is **default-enabled @main** and emits CEL-expression username mappings + `UserValidationRules` an older (4.20) KAS structured-auth parser may reject. FG absent @4.20.

**Honorable mentions:** KAS PSa default `privileged`→`restricted` (COORDINATION-RISK, `kas/config.go`); KAS KMS/secret-encryption convergence reads of live KAS Deployment (`kas/secretencryption.go:isKASConverged`, COORDINATION-RISK); etcd sharding with un-backed-up PVCs (`etcd/shard.go`, COORDINATION-RISK, TechPreview); CAPI `--skip-crd-migration-phases` firing exactly at the 4.20 floor (ASSUMES-NEWER-OPERAND, UNCONFIRMED); konnectivity 90s multi-IP dial budget (COORDINATION-RISK, same as 5.0).

---

## Confidence / UNCONFIRMED notes
- PKI "**changed**" is **high confidence** — the `requeueInterval`/`revocationVerificationTimeout`/branch-flip hunks are in the 4.20..main diff and absent from the 5.0..main diff (verified programmatically).
- etcd backup "changed" and defrag "unchanged" are **high confidence** (non-test diffs read directly).
- Version constants: `Version420`/`CurvePreferences`/`ApplyServingInfoFromTLSProfile`/`ExternalOIDCWithUpstreamParity` confirmed **absent @4.20, present @main**; `Version419` present at both.
- Whether the exact **4.20 capi-provider** binary accepts `--skip-crd-migration-phases=StorageVersionMigration`/`=CleanupManagedFields` is **UNCONFIRMED** (gate floor == operand version suggests intended support).
- Refactor-only moves (`util`→`reconcilerpolicy`/`podspec`/`k8sutil`/`imageregistry`, `manifests`→`cpomanifests`/`pkg/manifests`) assumed value-identical based on diff context; **UNCONFIRMED** byte-identity of every moved constant, though diffs indicate mechanical relocations.
- "count of flipping gates = 0" is scoped to **version-gated flags**; ungated behavioral changes (hazards #1–#5) are the real 4.20 risk and are not gate-flips.
