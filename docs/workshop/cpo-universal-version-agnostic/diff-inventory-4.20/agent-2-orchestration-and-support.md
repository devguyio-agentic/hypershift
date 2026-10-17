# Agent-2 — CPO Orchestration & Supporting-Library Diff Inventory (4.20 → main)

**Branches:** `upstream/release-4.20` (48fd5966) → `upstream/main` (220e0e73)
**Method:** two-dot `git diff upstream/release-4.20..upstream/main`, read-only, no compile.
**Scope:** `control-plane-operator/controllers/hostedcontrolplane/` (EXCL `v2/`), `support/controlplane-component/`, `support/config/`, `support/releaseinfo/`, `control-plane-pki-operator/`.
**Baseline compared against:** the 5.0→main run (`.../diff-inventory/agent-2-orchestration-and-support.md`).
**Headline:** at a ~5-minor floor the delta is *materially wider* than 5.0 on **every** axis — TLS config emission, framework gating, releaseinfo, and PKI all changed behaviorally (several of them were byte-neutral or absent at 5.0).

---

## (a) Summary table — region × category

| Region | C3 gating | C4 API | C6 protocol | C7 image/release | C8 infra/cloud | C9 PKI/cert | C11 config-emit |
|---|---|---|---|---|---|---|---|
| hostedcontrolplane/ (top-level, pki/, infra/, kas/, cloud/, kms) | — | ✅ (KMS direct-validate, deprecated-annotation cond, **ControlPlaneInitialized NEW field**) | ✅ (KMS fallback state machine; status-patch helper) | — | ✅ (KMS direct check, AWS endpoint-service delete order, LB health metrics) | ✅ (konnectivity signer 1→5 split; **KAS bootstrap-container signer NEW**; GCP WIF cert; etcd shard certs; CA aggregation variadic) | — |
| support/controlplane-component/ | ✅ (PDB predicate skips SingleReplica; **etcd dep-exclusion**; **ObservedGeneration**; **NativeSidecar gating**; **template-data rendering**) | ✅ (reads `hcp.Spec.Tolerations`; node-failure toleration injection; GCP restricted SCC) | — | ✅ (release-image resolution `util`→`imageregistry`; karpenter image key) | — | — | — |
| support/config/ | — | ✅ **(`MinTLSVersion`/`CipherSuites` signatures now return `error`)** | — | — | — | — | ✅ **NEW `CurvePreferences()`→curve 4588; NEW `TLSArgs()`, `GenericControllerConfig`, `ServingInfo`, `SupportedEtcdCipherSuites`, AESCBC objects** |
| support/releaseinfo/ | ✅ (**NEW Cincinnati version resolver**) | ✅ (stream-metadata type swap `CoreOS*`→`stream.Stream`; multi-stream `OSStreams`) | — | ✅ (registry-override on pullspec; canonical-image tracking; RHEL9/10 streams) | — | — | — |
| control-plane-pki-operator/ | — | — | ⚠️→✅ **(requeue protocol reworked: SyntheticRequeueError→AddAfter; revocation timeout gate; event-bug fixes)** | — | — | ⚠️ (import-path move + CSR fulfillment-event bug fix) | — |

Legend: ✅ substantive behavioral change · ⚠️ touched but near-neutral · — none found.

---

## (b) Detailed findings

### support/config — C11 SHARED-CONFIG-EMISSION + C4 API-SIGNATURE (far wider than 5.0)
- **C11 | `groups.go` (NEW) | `CurvePreferences(ctx, profile)` → curve 4588 (X25519MLKEM768).** Same new function as in the 5.0 run, resolving a profile's `Groups` via `libgocrypto.TLSGroupsToCurveIDs`. On **4.20 the testdata has ZERO `tls-curve-preferences` / `4588` / `X25519MLKEM768`**; on main there are 8 files with the flag, 7 with `4588`. **SKEW: BREAKS** — a 4.20 operand binary (older Go/component-base) rejects `--tls-curve-preferences=4588,...` at startup → crashloop. Identical mechanism to 5.0, but the 4.20 floor confirms the curve dimension was *entirely absent*, so the whole flag is net-new to any 4.20 operand.
- **C11/C4 | `cipher.go` | CHANGED at 4.20 (was UNCHANGED at 5.0).** `MinTLSVersion()` and `CipherSuites()` signatures changed from returning a bare value to **`(value, error)`** (nil-`Custom` guard). NEW helpers added: `SupportedEtcdCipherSuites(ctx, …)` (filters against etcd `tlsutil.GetCipherSuite`), `SetMinTLSVersionUsingAPIServer`, `SetCipherSuitesUsingAPIServer`. **SKEW: FINE for the operand** (these are management-side CPO-internal APIs; the *emitted* min-version/cipher strings are unchanged — `VersionTLS12`, same IANA suites), but the signature churn is a **COORDINATION-RISK** for any out-of-tree caller compiled against the 4.20 signature. The emitted cipher/min-TLS *values* did NOT change (same as 5.0).
- **C11 | `deployment.go` (NEW) | `TLSArgs(profile)` emits `--tls-min-version=…` and `--tls-cipher-suites=…`.** Centralizes operand-flag emission. **SKEW: FINE** (values unchanged) but it is the rendering path many operands now share.
- **C11 | `genericcontrollerconfig.go` (NEW) | `BuildGenericControllerConfigData` / `SetGenericControllerConfig` emit a `GenericControllerConfig` YAML with ServingInfo (bind addr + ciphers + min-TLS).** New shared config-emission surface. **SKEW: FINE** (values unchanged; cipher/min from same source).
- **C11 | `servinginfo.go` (NEW) | `ApplyServingInfoFromTLSProfile` sets `ServingInfo.MinTLSVersion`+`CipherSuites`.** **SKEW: FINE.**
- **C11 | `kms.go` | NEW `AESCBCEncryptedObjects()` (returns `["secrets"]`) alongside existing `KMSEncryptedObjects()`.** Encryption-config resource list emission. **SKEW: FINE** (additive; AESCBC still encrypts only secrets).
- **C11-adjacent | `constants.go` | added `Version420`, `KubeconfigKey`, `AWSCloudProviderName`, `EtcdClientPort`, `ReconcileLegacyEnvVar`, `CloudTokenMountPath`, Azure defaults, Karpenter role IDs/MIs.** Pure constants. **SKEW: FINE.**

### support/controlplane-component — framework gating / API (CHANGED, unlike 5.0)
- **C3 | `status.go::checkDependencies` | etcd components no longer get an implicit kube-apiserver dependency (`!isEtcdComponent(name)`).** New helper `isEtcdComponent` matches `etcd` and `etcd-*` shards. This *removes* a dependency gate for etcd/shards. **SKEW: FINE** — management-side rollout ordering; relaxes a self-referential gate. (Was not present at 5.0.)
- **C3 | `status.go::reconcileComponentStatus` | NEW `component.Status.ObservedGeneration = hcp.Generation` write.** New status field population. **SKEW: FINE→COORDINATION-RISK(low)** — if a 4.20 `ControlPlaneComponent` CRD lacks `Status.ObservedGeneration` the write is dropped (SILENT-NOOP). UNCONFIRMED whether 4.20 CRD has the field.
- **C3/C4 | `controlplane-component.go` + `token-minter-container.go` | NEW `NativeSidecarContainersEnabled` context flag gates whether token-minter/konnectivity are injected as native sidecars (K8s ≥1.29) vs init-containers.** Management-cluster-capability gating. **SKEW: FINE** — decided by the *management* cluster version, not the guest payload.
- **C3 | `builder.go` + `controlplane-component.go` | NEW `WithAssetDir` / `WithTemplateData` + `loadManifest`/`LoadManifestTemplated`.** Manifests can now be rendered as Go templates before decode; delete/update/status all route through `AssetDirName()`. **SKEW: FINE** — rendering mechanism on the management side; the *core version-gating* (see (d)) is unchanged.
- **C3 | `common.go::AdaptPodDisruptionBudget` | PDB now a predicate (skip `SingleReplica`, delete existing).** Same change the 5.0 run found. **SKEW: FINE.** Also NEW `IsStorageAndCSIManaged(platform)` helper (IBMCloud/PowerVS excluded).
- **C4 | `defaults.go::setControlPlaneIsolation` | reads `hcp.Spec.Tolerations`; injects short NoExecute node-failure tolerations (10s API-critical / 60s etcd) via taint-match semantics unless user already tolerates.** Same as 5.0. **SKEW: FINE.** Also NEW GCP `EnforceRestrictedSecurityContextToContainers`. **SKEW: FINE** (GCP-only).
- **C7 | `defaults.go` + `controlplane-component.go` | release-image/metadata resolution `util.*`→`imageregistry.*`; availability-prober `util.*`→`podspec.*`; delete helpers `util.*`→`k8sutil.*`.** Behavior-equivalent package relocation. **SKEW: FINE.**

### hostedcontrolplane (top-level controller + subpackages)
- **C4 | `hostedcontrolplane_controller.go::Reconcile` | NEW `Status.Initialization.ControlPlaneInitialized = ptr.To(true)`.** **CONFIRMED NEW: the `ControlPlaneInitialized` field does not exist in the 4.20 API** (`api/hypershift/v1beta1/hosted_controlplane.go` — present only on main). **SKEW: SILENT-NOOP → COORDINATION-RISK** — against a 4.20 CRD the write is silently dropped; any consumer gating on "control plane initialized" never sees it. (At 5.0 this was UNCONFIRMED; **at 4.20 it is CONFIRMED a schema gap**.)
- **C8/C6 | `validateAWSKMSConfig` + NEW `validateKMSKeyDirect` | KMS validation fallback state machine** (direct `Encrypt` probe via CPO AWS session; error classification into `ValidAWSKMSConfig`). Same as 5.0. **SKEW: FINE→COORDINATION-RISK(low)** — altered condition wording, management-side only.
- **C8 | NEW `deleteAWSEndpointServices` + reordered `reconcileDeletion`** gated on `ValidAWSIdentityProvider`. **SKEW: FINE** (deletion orchestration).
- **C8 | `reconcileDefaultSecurityGroup` | writes `hcp.Status.Platform.AWS.DefaultWorkerSecurityGroupID`.** **CONFIRMED: at 4.20 the field already lives at `Status.Platform.AWS.*` (api/.../aws.go:1003).** So — unlike the 5.0 run, where this looked like a fresh migration — **the SG status-shape migration predates 4.20; NO skew at the 4.20 floor.** **SKEW: FINE.** *(This is a DELTA: a 5.0 hazard that disappears at 4.20.)*
- **C8 | `kas/metrics.go` (NEW) + health-check histograms.** Observability. **SKEW: FINE.**
- **C4 | NEW `reconcileDeprecatedConfigurationStatus`** (warning condition for deprecated verbosity annotation). **SKEW: FINE.**
- **C6 | status-condition writes migrated `meta.SetStatusCondition`→`statuspatching.PatchStatusCondition`** across deletion/cloud/CVO paths. **SKEW: FINE** (persistence mechanism).
- **C9 | `pki/konnectivity.go` + `kas_pki_setup.go` + controller | FIVE dedicated konnectivity signer CAs** (server-serving 8090, cluster-serving 8091, server-auth, client-auth) replace the single `konnectivity-signer`; `pki/ca.go::ReconcileKonnectivityConfigMap` now variadic `signers...`. Same split as 5.0. **SKEW: COORDINATION-RISK** — trust-topology expansion; a 4.20 konnectivity agent/server pinned to one signer could mismatch (bundle aggregation mitigates). Consumers are mostly in v2/ (another agent).
- **C9 | NEW `pki/ca.go::ReconcileKASBootstrapContainerSigner` + `kas_pki_setup.go` wiring + client cert.** **CONFIRMED NEW at 4.20** (0 refs at 4.20, 3 on main). A new signer is appended to `totalClientCABundle` and a `system:kas-bootstrap-container` client cert issued. **SKEW: COORDINATION-RISK(low)** — additive to the client CA bundle; a 4.20 KAS consuming the aggregated bundle still trusts it, but any pinned bundle snapshot would miss it. New guest-less trust path.
- **C9 | `pki/etcd.go` | NEW `ReconcileEtcdShardServerSecret`/`ReconcileEtcdShardPeerSecret` (per-shard DNS SANs) + `127.0.0.1`/`::1` added to the discovery peer cert.** Etcd sharding PKI. **SKEW: FINE** (additive; management-side etcd topology).
- **C9 | `pki/gcp_workload_identity_federation_webhook.go` (NEW) | 127.0.0.1 serving cert for GCP WIF webhook.** GCP-only additive. **SKEW: FINE.**
- **Refactor churn (neutral) | `util.*`→`reconcilerpolicy.*`/`k8sutil.*`/`podspec.*`; `manifests.*`→`pkg/manifests/cpo.*`.** Package relocation. **SKEW: FINE.**

### support/releaseinfo — C7 / C4 / C3 (wider than 5.0)
- **C3/C7 | `cincinnati_version_resolver.go` (NEW) | `CincinnatiVersionResolver.Resolve(ctx, version, channel)` queries the Cincinnati graph API (api.openshift.com) to resolve a version string → release pullspec, with a 10-min TTL cache.** Brand-new version-resolution path. **SKEW: FINE** (management-side resolution; additive), but it is a **new external dependency** (network to api.openshift.com) and a new release-selection surface — **COORDINATION-RISK(low)** in air-gapped/disconnected setups.
- **C4/C7 | `releaseinfo.go` | stream-metadata type swapped `CoreOSStreamMetadata` (in-tree) → `github.com/coreos/stream-metadata-go/stream.Stream`; added multi-stream `OSStreams map[string]*stream.Stream` + RHEL9/RHEL10 stream constants + `canonicalComponentImages`.** Payload metadata model change (multi-stream, OCP ≥5.0). **SKEW: FINE** on the operand flag axis; **COORDINATION-RISK(low)** for anything deserializing the old `CoreOS*` shape from a cached ConfigMap.
- **C7 | `registry_mirror_provider.go::Lookup` | applies `registryoverride.Replace` to the release pullspec *before* delegating + preserves `OSStreams`/canonical images.** Same intent as 5.0. **SKEW: FINE.**

### control-plane-pki-operator — C6 PROTOCOL CHANGED (unlike 5.0, which was byte-neutral)
- **C6 | `certificaterevocationcontroller.go` | the requeue protocol was reworked.** On 4.20 the revocation state machine returned `factory.SyntheticRequeueError` to requeue; on main it returns **`nil` + `syncContext.Queue().AddAfter(key, 10s)`** to bypass the `DefaultControllerRateLimiter` (which caused 11+ min stalls), plus a NEW **`revocationVerificationTimeout` (10 min) gate** in step 6 that surfaces a real `PreviousCertificatesRevoked=False` / `RevocationVerificationTimeout` condition. Several step-3/step-6 branches changed from "no-requeue" to "requeue" (bug fixes: missing signer/trust-bundle now requeue instead of silently completing). A unit-test `clock` hook was added. **SKEW: COORDINATION-RISK** — this is a genuine behavioral change to the break-glass cert-rotation/revocation *timing protocol*. It is self-contained within the PKI operator (does not require guest cooperation), but a mixed-version PKI-operator/CPO deployment could observe different requeue/timeout semantics. *(DELTA: at 5.0 this controller was byte-for-byte behavior-neutral; at 4.20 it is not.)*
- **C9/C6 | `certificatesigningcontroller.go` | bug fix: the "fulfilled" event fired on `err != nil` (wrong) → now fires on `err == nil`; also fixed a format-arg mismatch.** `certificatesigningrequestapprovalcontroller.go`: "`%q in is approved`" → "`%q is approved`". **SKEW: FINE** (event/message correctness only; signing/approval logic unchanged).
- **C6 | `main.go` | sets `KUBE_FEATURE_WatchListClient=false` unless already set.** Pins streaming-list feature off. **SKEW: FINE** (management-side client behavior).
- **C9 | `certificates/validation.go`, `targetconfigcontroller.go`, `topology/detector.go` | import-path move `hypershift-operator/controllers/manifests`→`pkg/manifests`; `%v`→`%w` error wrapping.** **SKEW: FINE** (neutral).

---

## (c) FOCUS — full set of TLS values support/config emits that differ 4.20 → main

| Dimension | 4.20 floor | main | Delta | Skew |
|---|---|---|---|---|
| **Curves** (`--tls-curve-preferences`) | **ABSENT** (0 testdata files; `groups.go` does not exist) | **`4588,29,23,24`** via NEW `CurvePreferences()`; 8 files, 7 with `4588`, 1 with `X25519MLKEM768` | **NET-NEW flag + NET-NEW value 4588** | **BREAKS** — 4.20 operand rejects unknown curve 4588 → crashloop |
| **Cipher suites** (`--tls-cipher-suites`) | present (15 files); e.g. `TLS_ECDHE_*_GCM_*`, `CHACHA20_POLY1305`, TLS1.3 suites | **same IANA suite strings**; now emitted via NEW `TLSArgs()`/`GenericControllerConfig`; 100 files (counts up only because testdata expanded) | **values UNCHANGED**; emission *path* centralized; API signature now returns `error` | **FINE** (value); COORDINATION-RISK (signature) |
| **MinTLSVersion** (`--tls-min-version`) | present (16 files); `VersionTLS12` | **same** `VersionTLS12`/`VersionTLS13`; 125 files | **values UNCHANGED**; signature now returns `error` | **FINE** (value); COORDINATION-RISK (signature) |
| **Etcd cipher filter** | none | NEW `SupportedEtcdCipherSuites()` filters input against etcd's `tlsutil.GetCipherSuite` | NET-NEW filter | **FINE** (narrows, never widens) |

**Answer:** **The curve axis is the only new *emitted operand value* (4588) — identical to the 5.0 finding — but at the 4.20 floor it is an even cleaner break because 4.20 had NO curve flag at all.** Beyond the value, 4.20→main also adds a much larger *emission-surface* delta than 5.0: `TLSArgs()`, `GenericControllerConfig`, `ServingInfo`, `SupportedEtcdCipherSuites`, and an error-returning signature change on `MinTLSVersion`/`CipherSuites` (which were byte-for-byte unchanged at 5.0). **So: YES — support/config emits MORE new TLS machinery at 4.20 than at 5.0, though the single new operand-flag *value* that breaks is still just curve 4588.**

---

## (d) FOCUS — did the component-framework version/dependency gating change 4.20 → main? **YES — more than at 5.0.**

- **Core version/rollout gate is still the same algorithm:** `checkDependencies` deems a dependency satisfied iff `Available=True AND RolloutComplete=True AND Status.Version == ReleaseImageProvider.Version()` (`status.go`). This semver-equality gate is unchanged 4.20→main. There is **no new payload-version guard** that would stop a skewed operand from being rolled — so, like 5.0, the framework still offers **no safety net** against the curve-4588 flag reaching a 4.20 operand. This *amplifies* the C11 BREAKS hazard.
- **But the dependency/gating *logic around* that core DID change at 4.20 (unlike 5.0):**
  1. **etcd dependency exclusion** — etcd and `etcd-*` shard components no longer get an implicit kube-apiserver dependency (`!isEtcdComponent`). Relaxes a gate.
  2. **`Status.ObservedGeneration`** now written per component (new field population; SILENT-NOOP if 4.20 CRD lacks it).
  3. **`NativeSidecarContainersEnabled`** — new management-cluster-capability gate deciding sidecar vs init-container injection.
  4. **Template-data rendering** (`WithTemplateData`/`WithAssetDir`) — new asset-loading mode threaded through update/delete/status.
- **Conclusion:** the framework's *skew-tolerance posture for payload version* is unchanged (same equality gate, still no version floor check), but the surrounding dependency/capability/status-gating logic moved materially — a clear DELTA from the 5.0 run where the framework gating was reported essentially untouched.

---

## (e) DELTA vs the 5.0 findings

| 5.0 finding | 4.20 status | Delta |
|---|---|---|
| `CurvePreferences`/4588 new; **only** new TLS value; cipher/MinTLS unchanged | **Same break, wider surface.** Still only new *value* is 4588, but 4.20 also gains `TLSArgs`, `GenericControllerConfig`, `ServingInfo`, etcd cipher filter, and error-returning `MinTLSVersion`/`CipherSuites` (these were *unchanged* at 5.0). | **WIDER** |
| `cipher.go` UNCHANGED | **CHANGED** at 4.20 (signatures → `error`, new helpers) | **NEW delta** |
| Framework version/rollout gating UNCHANGED | **Core gate unchanged**, but etcd-dep exclusion + ObservedGeneration + NativeSidecar + template-data all NEW | **WIDER** |
| PKI protocol byte-neutral | **CHANGED** — revocation requeue protocol reworked (SyntheticRequeueError→AddAfter), 10-min revocation timeout gate, step-3/6 requeue bug fixes, CSR event bug fixes | **NEW delta (PKI now behavioral)** |
| Konnectivity 1→5 signer split (COORDINATION-RISK) | **Same split** + NEW **KAS bootstrap-container signer** + etcd shard certs | **WIDER** |
| SG status moved to `Status.Platform.AWS.*` (COORDINATION-RISK, UNCONFIRMED) | **At 4.20 the field is ALREADY at `.AWS.*`** — migration predates 4.20 | **HAZARD REMOVED at 4.20 floor** |
| `ControlPlaneInitialized` write (COORDINATION-RISK, UNCONFIRMED 5.0 schema) | **CONFIRMED: field absent in 4.20 API** → write silently dropped | **CONFIRMED SILENT-NOOP** |
| releaseinfo registry-override pullspec | Same + NEW **Cincinnati resolver**, stream-metadata type swap, multi-stream `OSStreams`, RHEL9/10 | **WIDER** |

---

## (f) Ranked top skew / coordination hazards (against a 4.20 floor)

1. **[BREAKS] `support/config.CurvePreferences` emits curve 4588 (X25519MLKEM768)** → rendered as `--tls-curve-preferences=4588,...` into operands via NEW `TLSArgs()`. At 4.20 the flag is entirely absent; a 4.20 operand binary rejects the unknown curve → crashloop. **No framework version-gate stops it.** *Root cause in my region.*
2. **[SILENT-NOOP/COORDINATION-RISK] `Status.Initialization.ControlPlaneInitialized` write — field CONFIRMED absent in 4.20 API.** A main CPO against a 4.20 CRD has the write dropped; any gate keyed on "initialized" never fires. (Upgraded from UNCONFIRMED at 5.0 to CONFIRMED at 4.20.)
3. **[COORDINATION-RISK] PKI revocation protocol rework** (`certificaterevocationcontroller.go`: SyntheticRequeueError→`AddAfter`, new 10-min revocation-timeout condition, step-3/6 requeue semantics). Behaviorally different break-glass cert-rotation timing; a mixed-version PKI-operator/CPO pairing observes different requeue/timeout behavior. (NEW vs 5.0, where PKI was neutral.)
4. **[COORDINATION-RISK] Konnectivity trust split (1→5 signers) + NEW KAS bootstrap-container signer.** Trust-topology expansion against a 4.20 konnectivity agent/server / pinned CA bundle. Mitigated by CA aggregation; main consumers in v2/.
5. **[COORDINATION-RISK(low)] support/config API-signature + emission churn** — `MinTLSVersion`/`CipherSuites` now return `error`; new `GenericControllerConfig`/`ServingInfo`/`TLSArgs` emission paths. Emitted *values* unchanged (cipher/min-TLS identical), so no operand break, but out-of-tree callers compiled against 4.20 signatures won't link. Plus releaseinfo's NEW Cincinnati resolver adds an external api.openshift.com dependency (air-gap risk).

**Framework gating note (re: #1):** the core version/rollout equality gate is unchanged — the framework still renders the curve-4588 flag into a skewed component without any version floor check. No safety net at this layer (same conclusion as 5.0).

---

### Confidence / UNCONFIRMED markers
- **CONFIRMED by diff/grep:** `groups.go` new; `tls-curve-preferences`/`4588` absent on 4.20 (0 files) vs present on main (8/7); `cipher.go` signature change; framework etcd-dep/ObservedGeneration/NativeSidecar/template-data; PKI requeue protocol rework + revocation timeout; konnectivity 5-signer split; KAS bootstrap-container signer new (0→3 refs); `ControlPlaneInitialized` field absent in 4.20 API; SG field already at `.AWS.*` in 4.20 API; Cincinnati resolver new; stream-metadata type swap.
- **UNCONFIRMED:** whether the 4.20 `ControlPlaneComponent` CRD has `Status.ObservedGeneration`; real-world konnectivity trust behavior under skew (consumers in v2/, another agent); whether any 4.20 guest-side component actually consumes `ControlPlaneInitialized` (schema gap confirmed, consumer impact not traced).
