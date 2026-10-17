# Agent-2 — CPO Orchestration & Supporting-Library Diff Inventory
**Branches:** `upstream/release-5.0` (b9d6ae1) → `upstream/main` (220e0e7, task-named 253c3f5 lineage)
**Method:** two-dot `git diff upstream/release-5.0..upstream/main`, read-only, no compile.
**Scope:** `control-plane-operator/controllers/hostedcontrolplane/` (EXCL `v2/`), `support/controlplane-component/`, `support/config/`, `support/releaseinfo/`, `control-plane-pki-operator/`.

---

## (a) Summary table — region × category

| Region | C3 gating | C4 API | C6 protocol | C7 image/release | C8 infra/cloud | C9 PKI/cert | C11 config-emit |
|---|---|---|---|---|---|---|---|
| hostedcontrolplane/ (top-level, pki/, infra/, kas/, cloud/, kms) | — | ✅ (KMS direct-validate, deprecated-annotation cond, ControlPlaneInitialized) | ✅ (KMS validation fallback state machine; status-patch helper) | — | ✅ (KMS direct check, SG status shape, AWS endpoint-service delete order, LB health metrics) | ✅ (new konnectivity signer split; GCP WIF webhook cert; konnectivity CA aggregation) | — |
| support/controlplane-component/ | ✅ (PDB predicate now skips SingleReplica) | ✅ (reads `hcp.Spec.Tolerations`; node-failure toleration injection) | — | ✅ (release-image resolution moved `util`→`imageregistry`; karpenter image key) | — | — | — |
| support/config/ | — | — | — | — | — | — | ✅ **NEW `CurvePreferences()` emits post-quantum curve 4588** |
| support/releaseinfo/ | — | — | — | ✅ (registry-override applied to release pullspec before lookup) | — | — | — |
| control-plane-pki-operator/ | — | — | ⚠️ no protocol change (import-path refactor + test renames only) | — | — | ⚠️ no cert-logic change (KAS selector source moved to `pkg/manifests/cpo`) | — |

Legend: ✅ substantive behavioral change · ⚠️ touched but behavior-neutral · — none found.

---

## (b) Detailed findings

### support/config — **C11 SHARED-CONFIG-EMISSION (the headline hazard)**
- **C11 | `support/config/groups.go` (NEW FILE) + `constants.go` | `CurvePreferences(ctx, profile)` is brand-new on main; absent on 5.0.** It resolves a `TLSSecurityProfile`'s `Groups` to Go `tls.CurveID`s via `libgocrypto.TLSGroupsToCurveIDs`. The vendored library-go maps `TLSGroupX25519MLKEM768 → tls.X25519MLKEM768` (**curve ID 4588**, post-quantum). **SKEW: BREAKS** — the emitted curve set now includes `4588,29,23,24`; a 5.0-era operand binary built against an older Go/k8s that doesn't know curve 4588 rejects `--tls-curve-preferences=4588,...` at startup. This is the upstream origin of the machine-approver class of bug (full trace in section (c)).
- **C11-adjacent | `constants.go` | added `Version420`, `KubeconfigKey`, `AWSCloudProviderName`, Karpenter Azure role IDs/MIs.** Pure constant additions consumed by newer code paths. **SKEW: FINE** (no operand flag emission).
- `cipher.go` (MinTLSVersion / CipherSuites / etcd cipher filter) is **UNCHANGED** between branches — the cipher-suite and min-TLS-version emission did **not** change; only the curve/groups dimension is new.

### support/controlplane-component — framework gating / API
- **C3 | `common.go::AdaptPodDisruptionBudget` | PDB now gated by a predicate (`ControllerAvailabilityPolicy != SingleReplica`).** On 5.0 it always created a PDB (SingleReplica→minAvailable=1). On main SingleReplica PDBs are skipped and existing ones deleted via the predicate-false path. **SKEW: FINE** — management-side orchestration of PDBs; does not depend on guest payload version. (It is a genuine rollout/predicate-gating change, hence C3.)
- **C4 | `defaults.go::setControlPlaneIsolation` | reads `hcp.Spec.Tolerations` and injects short NoExecute node-failure tolerations (10s API-critical / 60s etcd) unless the user already tolerates the taint.** New consumption of the Tolerations field with taint-match semantics. **SKEW: FINE** — pod-spec tuning on the management cluster; no guest-version coupling.
- **C7 | `defaults.go` + `controlplane-component.go` | release-image resolution moved `util.HCPControlPlaneReleaseImage`/`util.ImageMetadataProvider` → `imageregistry.*`; karpenter fallback keyed on `karpenterutil.KarpenterProviderAWSImageName`.** Behavior-equivalent refactor of where the payload image is resolved. **SKEW: FINE** (same resolution semantics, new package home).

### hostedcontrolplane (top-level controller + subpackages)
- **C8/C6 | `hostedcontrolplane_controller.go::validateAWSKMSConfig` + new `validateKMSKeyDirect`/`defaultGetGuestToken`/`newKMSClient` | KMS validation gained a fallback state machine.** When the guest API is unavailable it now falls back to a direct KMS `Encrypt` probe using the CPO's own AWS session, classifying errors (`NotFoundException`, `DisabledException`, `KeyUnavailableException`, etc.) into the `ValidAWSKMSConfig` condition. **SKEW: FINE→COORDINATION-RISK(low)** — this is management-side cloud validation; it changes *which* condition/message the HCP surfaces but does not require guest cooperation. Harmless under skew, though a 5.0 consumer of the condition message sees new wording.
- **C8 | `reconcileDefaultSecurityGroup` | status shape changed: `DefaultWorkerSecurityGroupID` moved from `hcp.Status.Platform.DefaultWorkerSecurityGroupID` to `hcp.Status.Platform.AWS.DefaultWorkerSecurityGroupID` (nil-guarded).** **SKEW: COORDINATION-RISK** — if any older reader expects the field at the old location it reads empty; this is an HCP status-schema migration (C4-flavored). UNCONFIRMED whether 5.0 guest-side components read this field.
- **C8 | new `deleteAWSEndpointServices` + AWS cleanup ordering in `reconcileDeletion` | private-link/endpoint-service teardown reordered and gated on `ValidAWSIdentityProvider` condition.** **SKEW: FINE** — deletion-path orchestration on management side.
- **C8 | `kas/metrics.go` (NEW) + `healthCheckKASLoadBalancers`/`healthCheckKASEndpoint` | added Prometheus `hypershift_kube_apiserver_available` + request-duration histogram for KAS health.** **SKEW: FINE** — observability only.
- **C4 | `reconcileDeprecatedConfigurationStatus` (NEW) | surfaces a warning condition when the deprecated `hypershift.openshift.io/kube-apiserver-verbosity-level` annotation is present.** Message-only; does not change verbosity. **SKEW: FINE.**
- **C4 | `Reconcile` | now sets `hcp.Status.Initialization.ControlPlaneInitialized = true`.** New status field write. **SKEW: COORDINATION-RISK(low)** — if a 5.0 CRD lacks `Status.Initialization.ControlPlaneInitialized` the write is dropped by the API server (SILENT-NOOP); UNCONFIRMED whether 5.0 API schema has the field.
- **C6 (infra) | status-condition writes migrated from `meta.SetStatusCondition` to `statuspatching.PatchStatusCondition` (ctx, client, obj).** Changes *how* conditions are persisted (server-side patch vs in-memory set) across deletion, cloud-resource removal, CVO scaledown paths. **SKEW: FINE** — management-side persistence mechanism.
- **Refactor churn (behavior-neutral) | `util.*`→`reconcilerpolicy.*` (HCPOAuthEnabled, IsReconciliationPaused, GenerateReconciliationActiveCondition) and `manifests.*`→`pkg/manifests/cpo.*` (KubeAPIServerService / ServiceName / AzureLB) across `infra/infra.go`, `kas/service.go`, `kas/healthcheck.go`, `kas/kubeconfig.go`, `pki/kas.go`, `cloud/aws/providerconfig.go`.** Package relocation; identical values. **SKEW: FINE.** Note: `ReconcileKonnectivityServerLocalService` was removed from `kas/service.go` (moved under v2 ownership).
- **C9 | `pki/konnectivity.go` | FIVE new dedicated konnectivity signer CAs** (server-serving 8090, cluster-serving 8091, server-auth, client-auth) splitting the previously single konnectivity-signer. `pki/ca.go::ReconcileKonnectivityConfigMap` now aggregates variadic `signers...`. **SKEW: COORDINATION-RISK** — this is a trust-topology expansion: a main CPO issuing multiple konnectivity signers against a 5.0 konnectivity-agent/server that expects one signer bundle could mismatch trust. Mitigated by the CA bundle aggregating all signers, but a skewed agent pinned to the old single signer name is a stall risk. (Most consumers live in v2/ — another agent's region — so cross-check there.)
- **C9 | `pki/gcp_workload_identity_federation_webhook.go` (NEW) | new 127.0.0.1 serving cert for the GCP WIF webhook.** GCP-only, additive. **SKEW: FINE** (new platform feature, no 5.0 counterpart to break).

### support/releaseinfo — C7
- **C7 | `registry_mirror_provider.go::Lookup` | applies `registryoverride.Replace(image, RegistryOverrides)` to the release-image pullspec *before* delegating the lookup.** On 5.0 the raw image was looked up directly; overrides were applied later/elsewhere. Lets non-OpenShift management clusters (no IDMS/ICSP) redirect the initial pull. **SKEW: FINE** — changes image *source resolution* on the management side; does not alter which component versions land in the payload.

### control-plane-pki-operator — C6/C9 (NO protocol change)
- **⚠️ C9/C6 | `certificaterevocationcontroller.go`, `certificates/validation.go` | KAS service/deployment name + manifests source moved from `hcpmanifests`/`hypershift-operator/controllers/manifests` to `pkg/manifests/cpo` and `pkg/manifests`.** The CSR signing, cert-rotation, revocation state machine, per-pod verification, and replica-count guard logic are **unchanged**; only the import source of the KAS name constant moved. Test-case *descriptions* were reworded ("csr missing" → "When CSR is missing, it should return no error") with no assertion changes. **SKEW: FINE** — the break-glass PKI / CSR / trust-replacement protocol is byte-for-byte equivalent in behavior.

---

## (c) SPECIAL FOCUS — does support/config emit a NEW TLS curve/cipher/version value? **YES (curve).**

**Trace (upstream source of the machine-approver bug):**
1. **5.0 baseline:** `support/config/groups.go` **does not exist**; `grep tls-curve-preferences` over 5.0 testdata → **0 matches**. The machine-approver (and any other operand) never received a `--tls-curve-preferences` flag on 5.0.
2. **main:** `support/config/groups.go::CurvePreferences(ctx, profile)` is new. For a nil/default profile it uses `libgocrypto.DefaultTLSProfileType`, pulls `configv1.TLSProfiles[type].Groups`, and calls `libgocrypto.TLSGroupsToCurveIDs(groups)`.
3. The vendored `library-go/pkg/crypto` (bumped on main) maps `configv1.TLSGroupX25519MLKEM768 → tls.X25519MLKEM768` = **curve ID 4588** (post-quantum hybrid), alongside 29 (X25519), 23 (P-256), 24 (P-384).
4. The sole in-tree consumer is `control-plane-operator/.../v2/machine_approver/deployment.go` (another agent's region), which stringifies the IDs into `--tls-curve-preferences=4588,29,23,24`. Confirmed in every `testdata/machine-approver/**/..._deployment.yaml` fixture on main.
5. **Skew impact:** the *value* (4588) is emitted by **my-region code** (`support/config`), then rendered into an operand flag by the v2 layer. A 5.0-vintage operand binary (older Go/k8s component-base) does not recognize curve 4588 and **rejects the flag → crashloop**. Cipher suites and MinTLSVersion emission are **unchanged** (`cipher.go` identical), so the curve/groups axis is the *only* new TLS value.

**Answer: YES — support/config now emits curve ID 4588 (X25519MLKEM768) via the new `CurvePreferences()`; this value did not exist on 5.0 and is the upstream source of the `--tls-curve-preferences` skew break.**

---

## (d) SPECIAL FOCUS — did the component-framework's version/dependency gating change in a way that affects skew tolerance?

**Largely NO for version/desired-version gating; one predicate change + new field consumption.**
- No changes found to a *desired-version* / *rollout-complete* / *payload-version* gate that would decide whether a version-skewed component is rolled. The `support/controlplane-component` diff contains **no new semver/version comparison** governing component rollout.
- The one genuine gating change is **`AdaptPodDisruptionBudget` gaining a predicate** (skip PDB for `SingleReplica`). This governs *whether a PDB object is created/deleted*, not whether a skewed component rolls — **no skew-tolerance impact** (management-side, availability-policy driven).
- New **toleration injection** (`setControlPlaneIsolation`) and the **image-resolution package move** are additive/neutral.
- **Conclusion:** the framework's skew-gating posture is essentially unchanged on main. It does **not** add a guard that would stop an old-version operand from being rolled — i.e. the framework will happily render the new curve flag into a skewed component without a version check. This *amplifies* the C11 hazard: there is no safety net at the framework layer.

---

## (e) Ranked top skew / coordination hazards

1. **[BREAKS] `support/config.CurvePreferences` emits curve 4588 (X25519MLKEM768)** → rendered as `--tls-curve-preferences=4588,...` into operands (machine-approver confirmed; any component calling it). A 5.0 operand binary rejects the unknown curve and crashloops. *Root cause lives in my region.*
2. **[COORDINATION-RISK] Konnectivity PKI trust split (`pki/konnectivity.go`, `pki/ca.go`)** — five dedicated signers replace one; a 5.0 konnectivity agent/server pinned to the single old signer can mismatch trust and stall the tunnel (bundle aggregation mitigates but does not guarantee).
3. **[COORDINATION-RISK] SG status schema migration** — `DefaultWorkerSecurityGroupID` moved to `Status.Platform.AWS.*`; older readers may see an empty field. (UNCONFIRMED which 5.0 consumers read it.)
4. **[COORDINATION-RISK(low)/SILENT-NOOP] `Status.Initialization.ControlPlaneInitialized` write** — dropped if the 5.0 CRD lacks the field; downstream gates keyed on it would never see "initialized." (UNCONFIRMED 5.0 schema.)
5. **[FINE→low-risk] KMS direct-validation fallback (`validateKMSKeyDirect`)** — changes `ValidAWSKMSConfig` condition messages/behavior; management-side only, no guest coupling, but altered condition semantics could confuse a 5.0 consumer of that condition.

**Framework gating note (re: #1):** no version/dependency gate was added that would prevent the skew-breaking curve flag from reaching an old operand — the framework offers no safety net here.

---

### Confidence / UNCONFIRMED markers
- CONFIRMED by diff: `groups.go` new; `tls-curve-preferences` absent on 5.0; library-go 4588 mapping; PKI protocol byte-neutral; PDB predicate; KMS fallback; konnectivity signer split.
- UNCONFIRMED (needs cross-region / CRD-schema check): whether 5.0 guest/CRD actually has `Status.Platform.AWS.DefaultWorkerSecurityGroupID` and `Status.Initialization.ControlPlaneInitialized`; the real-world konnectivity trust behavior under skew (consumers are in v2/, another agent's region).
