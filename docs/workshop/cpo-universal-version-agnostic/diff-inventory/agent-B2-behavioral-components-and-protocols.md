# Agent B2 — Behavioral diff inventory: component adapt logic + maintenance subprocesses + PKI protocol

**Scope:** `control-plane-operator/controllers/hostedcontrolplane/v2/**` (adapt/predicate/gating logic), the maintenance/helper subprocess algorithms under `control-plane-operator/` (and shared `support/` libs they use), and `control-plane-pki-operator/**`.

**Diff basis:** `git diff upstream/release-5.0..upstream/main` (two-dot).
- release-5.0 = `b9d6ae1c63453c012e889525eb924b3a07bac26b`
- main       = `220e0e73a47ee3384e999d30a7dfe87e1e825ea3` (task-stated commit `253c3f5c…` is an ancestor of this main HEAD; confirmed present in history).

**Method:** read `.go` decision logic only. Ignored embedded YAML assets, `zz_fixture`/testdata, and `*_test.go`. Logic read, not compiled.

**Skew classes:** SELF-CONTAINED (behavior depends only on CPO + HCP spec it already controls) / ASSUMES-NEWER-GUEST / ASSUMES-NEWER-OPERAND (the adapted operand binary must support the new flag/env/behavior) / COORDINATION-RISK (relies on ordering/propagation across components or management-cluster state).

---

## 0. Headline conclusions

- **PKI operator protocol: NOT changed.** The only `.go` edits in `control-plane-pki-operator/` are import-path refactors (`hcpmanifests`→`cpomanifests`, `hypershift-operator/.../manifests`→`pkg/manifests`) with identical constant values. The per-pod `verifyCertificateAgainstAllKASPods` live-verification, replica-count guard, SelfSubjectReview, CSR approval, and revocation state machine already exist in 5.0 and are byte-for-byte behaviorally unchanged.
- **etcd maintenance algorithm (defrag/backup/restore): NOT changed.** `etcd-defrag/`, `etcd-backup/`, `availability-prober/`, `token-minter/`, `kas-bootstrap/`, `sync-global-pullsecret/` have **test-only** diffs — zero change to the defrag member-selection / leader-last / learner-exclusion / fragmentation logic, backup/restore, token renewal thresholds, or prober checks.
- **The one real maintenance-subprocess algorithm change is in the konnectivity proxy dialer/resolver** (`support/konnectivityproxy/`): multi-IP resolution + fallback dial loop + a new overall dial budget. This is the single highest-impact behavioral change a manifest diff cannot see.
- **The dominant component-logic change is a new log-verbosity computation** pulled from a brand-new HCP API field `hcp.Spec.OperatorConfiguration.<Component>.LogLevel` (absent in 5.0), applied across KAS/KCM/scheduler/OAPI/OAuth/OAuth-apiserver/OCM/etcd. Plus new GCP workload-identity webhook in KAS, Azure standalone-karpenter, and a version-gated TLS/CRD-migration branch in CCO and CAPI.

---

## (a) Per-component behavioral-logic changes (.go logic only)

### KAS (kube-apiserver)
- **`kas/deployment.go:resolveKASVerbosity` (new)** | KAS `--v` is now computed from `hcp.Spec.OperatorConfiguration.KubeAPIServer.LogLevel` (new API field) with the old `KubeAPIServerVerbosityLevelAnnotation` as *fallback*; previous code only read the annotation and defaulted to 2. | **SELF-CONTAINED** — reads HCP spec the CPO owns; maps to a klog `--v` int the operand already understands.
- **`kas/deployment.go:applyGCPWorkloadIdentityFederationWebhookContainer` (new) + `applyPlatformSpecificIdentityWebhookContainers` (new)** | Adds a `GCPPlatform` case that injects a `/usr/bin/gcp-workload-identity-federation-webhook` sidecar (annotation-prefix, gcp-default-region from `Spec.Platform.GCP.Region`, token-audience=openshift) alongside the existing AWS/Azure cases. | **ASSUMES-NEWER-OPERAND** — requires the release payload to ship the `gcp-workload-identity-federation-webhook` binary and a GCP-capable HCP; a 5.0 guest/payload without it will crashloop the init/sidecar.
- **`kas/component.go:NewComponent`** | Registers a new `gcp-workload-identity-federation-webhook-kubeconfig.yaml` manifest adapter gated `EnableForPlatform(GCPPlatform)`. | **ASSUMES-NEWER-OPERAND** — only matters on GCP; wires the kubeconfig the new sidecar consumes.
- **`kas/kubeconfig.go:adaptGCPWorkloadIdentityFederationWebhookKubeconfigSecret` (new)** | Builds a localhost-URL SA kubeconfig (`openshift-authentication`/`gcp-workload-identity-federation-webhook`) from CSRSigner+RootCA, skipped when `SkipCertificateSigning`. | **SELF-CONTAINED** (CPO-owned certs), but only exercised under the GCP path above.
- **`kas/config.go` + `kas/kubeconfig.go`** | `util.ConfigOAuthEnabled`→`reconcilerpolicy.ConfigOAuthEnabled`; `podspec.KubeconfigKey`→`config.KubeconfigKey`; capi `v1beta1`→`v1beta2` import. Verified same function bodies/values. | **SELF-CONTAINED / no behavioral change** (pure refactor).

### CCO (cloud-credential-operator)
- **`cloud_credential_operator/deployment.go:adaptDeployment`** | Now parses `ReleaseImageProvider.Version()` (semver) and **only** appends TLS args when `major>=5 || (major==4 && minor>=23)`; errors if version unparseable. 5.0 unconditionally passed TLS args. | **ASSUMES-NEWER-OPERAND** — the version gate exists precisely because older CCO binaries don't accept the TLS flags; feeding a mismatched operand/payload changes whether TLS args appear.

### machine-approver
- **`machine_approver/deployment.go:adaptDeployment`** | New branch: computes `config.CurvePreferences(...)` (a function that does **not exist in 5.0**) from the TLS security profile and appends `--tls-curve-preferences=<ids>` when non-empty. | **ASSUMES-NEWER-OPERAND** — the machine-approver binary must accept `--tls-curve-preferences`; an older operand rejects the flag.

### Karpenter operator
- **`karpenteroperator/component.go:predicate`** | Signature now takes `*KarpenterOperatorOptions`; adds a gate that **disables** the component on `AzurePlatform` unless `StandaloneKarpenterOperatorEnabled`. New SA name/namespace sourced from `karpenterutil` constants; adds `MonitorOperandsRolloutStatus()`. | **COORDINATION-RISK** — component now runs/doesn't-run based on an operator env-var feature flag plus platform; behavior diverges from 5.0's unconditional enable-when-AutoNode.
- **`karpenteroperator/component.go:karpenterCredentialsSecretEnabled` (new predicate)** | The `karpenter-credentials` secret is now gated to AWS only (`...Karpenter.Platform == AWSPlatform`); Azure uses workload-identity env + token-minter instead. | **SELF-CONTAINED.**
- **`karpenteroperator/secret.go:adaptCredentialsSecret`** | Now switches on `Karpenter.Platform`; AWS unchanged, **any non-AWS platform returns an error** instead of silently writing AWS creds. | **SELF-CONTAINED** (fail-fast on unsupported platform).
- **`karpenteroperator/deployment.go:adaptStandaloneDeployment`** | Major expansion: full Azure branch (requires `AutoNode…Azure.ClientID`, subnetID, resourceGroup; injects `AZURE_CLIENT_ID`/`AZURE_FEDERATED_TOKEN_FILE`/`AZURE_SUBSCRIPTION_ID`/`AZURE_TENANT_ID`/`AZURE_NODE_RESOURCE_GROUP`/`VNET_SUBNET_ID`), new `TOKEN_MINTER_IMAGE` env, image override via `hyperkarpenterv1.KarpenterOperatorImage` annotation, provider-creds volume now AWS-only. | **ASSUMES-NEWER-OPERAND** — standalone karpenter-operator + azure karpenter provider images must exist in the payload and honor these env vars.

### CAPI manager
- **`capi_manager/deployment.go:adaptDeployment`** | New branch: when `version.GE(config.Version420)` (`Version420` constant is **new in main**, absent in 5.0) appends `--skip-crd-migration-phases=StorageVersionMigration` and `=CleanupManagedFields`. | **ASSUMES-NEWER-OPERAND** — the CAPI manager binary must support `--skip-crd-migration-phases`; only applied at ≥4.20.

### AWS cloud-controller-manager
- **`cloud_controller_manager/aws/config.go:adaptConfig`** | The config template `Sprintf` now interpolates an **additional `Region`** argument (`Spec.Platform.AWS.Region`) between zone and vpc. | **ASSUMES-NEWER-OPERAND (template-coupling)** — the paired config template must contain the extra `%s`; mismatched template/operand skew mis-renders the CCM config. (Template lives in the asset, but the *code* changed the positional contract.)

### Log-verbosity additions (new `--v` computed from `Spec.OperatorConfiguration.<comp>.LogLevel`)
All new `resolve<Comp>Verbosity` helpers map the new HCP `LogLevel` enum via `util.LogLevelToKlogVerbosity` (Normal/""→2, Debug→4, Trace→6, TraceAll→8; panics on unknown):
- **`kcm/deployment.go:resolveKCMVerbosity` (new)** → appends `--v` to kube-controller-manager.
- **`kube_scheduler/deployment.go:resolveSchedulerVerbosity` (new)** → appends `--v`.
- **`oapi/deployment.go:resolveOAPIVerbosity` (new)** → appends `--v`; also `util.HCPOAuthEnabled`→`reconcilerpolicy.HCPOAuthEnabled`.
- **`oauth/deployment.go:resolveOAuthVerbosity` (new)** → appends `--v`; `KubeadminSecretHashAnnotation` moved to `pkgoauth`.
- **`oauth_apiserver/deployment.go:resolveOAuthAPIServerVerbosity` (new)** → appends `--v`.
- **`ocm/deployment.go:adaptDeployment` + `resolveOCMVerbosity` (new file/func)** | OCM gains an adapt function (previously had none) solely to append `--v`; `ocm/component.go` wires `WithAdaptFunction(adaptDeployment)`. | 
- Class for all of the above: **SELF-CONTAINED** — reads a new CPO-owned HCP field, emits a klog `--v` int every one of these operands already accepts; default (unset field) preserves `--v=2`. Low skew risk, but note the **panic-on-unknown-LogLevel** in the shared mapper is a new hard-fail path if an unexpected enum value ever reaches it.

### etcd
- **`etcd/statefulset.go:adaptStatefulSet`** | New: sets `ETCD_LOG_LEVEL` env from `Spec.OperatorConfiguration.Etcd.LogLevel` via `util.LogLevelToEtcdLevel` (Normal/""→"info", Debug→"debug"). | **SELF-CONTAINED** — env var honored by etcd; unset→"info" preserves prior behavior. (This is etcd *logging config*, not a maintenance-algorithm change.)

### Router
- **`router/config.go:generateRouterConfig`** + **`router/component.go:aroExpectedHCPRouterRouteNames`** | KAS/OAuth route-name constants switched to `cpomanifests.*`; `supportutil.HCPOAuthEnabled`→`reconcilerpolicy.HCPOAuthEnabled`. Values verified identical. | **SELF-CONTAINED / no behavioral change** (refactor).

### hosted-cluster-config-operator (configoperator)
- **`configoperator/deployment.go:adaptDeployment`** | For `IBMCloudPlatform`, the HCCO `--controllers` list now **adds `user-ca-bundle`** (was `controller-manager-ca,resources,inplaceupgrader,drainer,hcpstatus`). Also `util.Convert…`→`imageregistry.Convert…` refactors. | **COORDINATION-RISK (IBM only)** — enables an additional in-cluster controller loop on the guest; benign elsewhere.

### Behavior-neutral refactors (confirmed no logic change — flag so a reviewer doesn't re-chase them)
- `cno/deployment.go`, `controlplaneoperator/deployment.go`, `cvo/deployment.go`, `konnectivity_agent/deployment.go`, `olm/catalogs/deployment.go`, `ignitionserver/deployment.go`: `support/util`→`support/imageregistry` and/or `support/util`→`support/reconcilerpolicy` moves; identical function bodies/values.
- `kcm/kubeconfig.go`, `kube_scheduler/kubeconfig.go`, `kas/kms/aws.go`, `kas/kms/azure.go`, `oauth/config.go`, `oauth/component.go`, `oauth_apiserver/component.go`: `podspec.KubeconfigKey`→`config.KubeconfigKey` / `manifests`→`cpomanifests` / `util`→`reconcilerpolicy`. Pure package relocation.

---

## (b) Maintenance / helper subprocess algorithm changes

| Subprocess | Verdict | Evidence |
|---|---|---|
| **etcd defrag** (`etcd-defrag/`) | **UNCHANGED** | No non-test `.go` diff. Member selection / leader-last / learner-exclusion / fragmentation thresholds untouched. |
| **etcd backup/restore** (`etcd-backup/`) | **UNCHANGED** | Only `etcdbackup_test.go`, `fetchcerts_test.go` changed. No restore-revision/compaction logic change. |
| **availability-prober** (`availability-prober/`) | **UNCHANGED** | No non-test diff. Probe checks/wait conditions identical. |
| **token-minter** (`token-minter/`) | **UNCHANGED** | No non-test diff. Renewal thresholds identical. |
| **kas-bootstrap** (`kas-bootstrap/`) | **UNCHANGED** | Only `kas_boostrap_test.go` changed. |
| **sync-global-pullsecret** (`sync-global-pullsecret/`) | **UNCHANGED** | Only `*_test.go` changed. |
| **konnectivity-https-proxy / socks5-proxy** (cmd) | **UNCHANGED** | Only `cmd_test.go`. |
| **konnectivity proxy dialer** (`support/konnectivityproxy/dialer.go`, `resolver.go`) | **CHANGED — real algorithm change** | See below. |

### konnectivity proxy dialer/resolver — the one real subprocess algorithm change
This library is executed inside the HCP konnectivity proxy sidecars (socks5 + https). Behavior now:

- **`support/konnectivityproxy/resolver.go:guestClusterResolver.resolve`** | Return type changed `net.IP` → `[]net.IP`; now parses and returns **all** resolved A/AAAA records, not just `addresses[0]`. | **SELF-CONTAINED.**
- **`resolver.go:proxyResolver.Resolve`** | On both the management-cluster fallback path and guest-cluster path, now collects all IPs via new `resolveAllIPs(ctx, name, preferIPv4)`, returns the primary IP and stashes the remainder in context via new `contextWithFallbackIPs(ctx, ips[1:])`. The konnectivity-health markFailure/markSuccess logic is preserved. | **SELF-CONTAINED** (management + guest DNS resolution), but see coordination note.
- **`dialer.go:DialContext`** | Rewritten: resolve-before-dial moved up; builds an address list (`buildAddressList`, primary + up to `maxFallbacks=3` fallback IPs from context); wraps the whole thing in a new **`overallDialBudget = 90s`** context; **loops** over addresses calling the new `dialThroughKonnectivity`, returning on first success, breaking when the budget expires, returning `lastErr` otherwise. | **SELF-CONTAINED** behaviorally, but **COORDINATION-RISK** w.r.t. latency SLOs: previously a single-address attempt with a 60s CONNECT deadline; now up to 4 attempts under a 90s overall cap — failure/timeout characteristics toward the guest API and tunneled services change materially. A manifest diff shows nothing.
- **`dialer.go:dialThroughKonnectivity` (extracted)** | Per-attempt CONNECT deadline now the *earlier* of 60s or the parent `overallDialBudget` deadline, so no single handshake outlives the budget. | **SELF-CONTAINED.**

---

## (c) PKI operator protocol — step-by-step

**Protocol verdict: UNCHANGED between 5.0 and main.** The certificate-revocation / trust-replacement / CSR-approval / live-verification state machine is identical. The two changed files contain only import relocations:

1. `certificaterevocationcontroller/certificaterevocationcontroller.go`
   - `kasAppLabelSelector` now builds from `cpomanifests.KubeAPIServerServiceName` instead of `hcpmanifests.KubeAPIServerServiceName` (same string constant).
   - `verifyCertificateAgainstAllKASPods` still: (i) GETs the KAS **Deployment** to read the authoritative replica count (guard against informer-cache lag / partial pod lists), (ii) lists KAS pods by the app selector, (iii) connects to **each pod's PodIP** over HTTPS with `TLSClientConfig.ServerName = <KAS service name>` (SNI + hostname validation, CA verification on), (iv) runs `verifyFunc` per pod under `perPodVerifyTimeout = 10s`. The only edit is the service-name constant source. **No change to order, conditions, or the live SelfSubjectReview-style verification.**
2. `certificates/validation.go`
   - Import `hypershift-operator/controllers/manifests` → `pkg/manifests`. No logic change; CSR validation conditions identical.
3. `certificaterevocationcontroller_test.go`, `certificatesigningcontroller_test.go` — test-only.

So: trust-propagation order, new-signer generation, prune, per-pod live verify, CSR approval conditions, and rotation/revocation triggers are **behaviorally identical**; a reviewer can treat PKI as a no-op for version-skew purposes (the moved constants resolve to the same values).

---

## (d) Ranked top behavioral hazards a manifest-diff would MISS

1. **konnectivity proxy multi-address fallback + 90s dial budget** — `support/konnectivityproxy/dialer.go:DialContext` / `dialThroughKonnectivity` | **COORDINATION-RISK.** Changes retry count (1→up to 4) and overall timeout envelope for every tunneled connection from HCP components to the guest; alters failure latency and health-marking cadence. Pure Go logic — invisible in rendered YAML.
2. **CCO TLS-args version gate** — `cloud_credential_operator/deployment.go:adaptDeployment` | **ASSUMES-NEWER-OPERAND.** The `>=5 || >=4.23` branch decides whether TLS flags are passed; running main's CPO against a 5.0/older CCO operand flips flag presence based on payload version, not manifest.
3. **CAPI `--skip-crd-migration-phases` at ≥4.20** — `capi_manager/deployment.go:adaptDeployment` | **ASSUMES-NEWER-OPERAND.** Silently adds two CLI flags the CAPI manager binary must support; a mismatched operand rejects them. Gated on a constant that doesn't exist in 5.0.
4. **KAS GCP workload-identity-federation webhook sidecar** — `kas/deployment.go:applyGCPWorkloadIdentityFederationWebhookContainer` (+ `applyPlatformSpecificIdentityWebhookContainers`, `kas/component.go`, `kas/kubeconfig.go`) | **ASSUMES-NEWER-OPERAND.** New GCP code path injects a binary/kubeconfig the 5.0 payload may not ship; conditional on `GCPPlatform`.
5. **Verbosity now driven by new `Spec.OperatorConfiguration.*.LogLevel` field + panic-on-unknown mapper** — `kas/deployment.go:resolveKASVerbosity` (and the KCM/scheduler/OAPI/OAuth/OAuth-apiserver/OCM/etcd siblings via `util.LogLevelToKlogVerbosity`/`LogLevelToEtcdLevel`) | **SELF-CONTAINED but version-skew-sensitive.** The field is absent in the 5.0 API; the shared mapper `panic`s on any unhandled enum value. Default path preserves `--v=2`, but this is new read-of-HCP-state logic a manifest diff can't attribute, and the panic is a new hard-fail surface.

Honorable mentions (behavioral, lower severity): Karpenter Azure-standalone gating (`karpenteroperator/component.go:predicate`, COORDINATION-RISK via feature-flag), machine-approver `--tls-curve-preferences` (ASSUMES-NEWER-OPERAND), AWS CCM config template positional `Region` arg (template-coupling), HCCO `user-ca-bundle` controller on IBM Cloud (COORDINATION-RISK).

---

## Confidence / UNCONFIRMED notes
- PKI "unchanged" is **high confidence** — exhaustive file list shows only the two import-refactor files + tests; constants verified same values.
- etcd/token-minter/prober/bootstrap/pullsecret "unchanged" is **high confidence** — `git diff --name-only` shows test-only files.
- `reconcilerpolicy.*` == old `util.*` verified for `HCPOAuthEnabled`/`ConfigOAuthEnabled` (identical bodies). Other moved helpers (`imageregistry.*`, `config.KubeconfigKey`, `cpomanifests.*`) assumed value-identical relocations based on diff context; **UNCONFIRMED** that every moved constant/function is byte-identical, though the diffs strongly indicate mechanical moves.
- The AWS CCM `Region` positional-arg change: the code contract changed; the paired template change lives in the asset (out of scope here). Flagged as template-coupling rather than verified end-to-end.
