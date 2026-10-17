# Agent B1 — Behavioral (control-flow / algorithm / protocol) diff: CPO imperative core + v2 framework

**Scope:** `control-plane-operator/controllers/hostedcontrolplane/*.go`, `.../{pki,cloud,infra}/` (NOT v2/), and `support/controlplane-component/` (v2 framework).
**Method:** two-dot Go-logic diff `git diff upstream/release-5.0..upstream/main -- <path>`. zz_fixture YAML deliberately NOT used as a finding source.
**Branch tips used:** `upstream/release-5.0` = `b9d6ae1c`, `upstream/main` = `220e0e73`.
**NOTE / discrepancy:** task context cited main commit `253c3f5c…`; the actual `upstream/main` tip in this clone is `220e0e73`. Findings are against `220e0e73`. If the parent needs exactly `253c3f5c`, re-run; the deltas below are unlikely to differ materially but this is **UNCONFIRMED** against that SHA.

---

## (a) What the Reconcile loop does differently on main vs 5.0 (chronological)

Top-level `HostedControlPlaneReconciler.Reconcile` + `reconcileDeletion` ordering:

1. **Deletion path (`reconcileDeletion`)** — signature changed (drops the pre-captured `originalHostedControlPlane`; now uses concurrency-safe `statuspatching.*`). **NEW teardown step inserted** *after* the default-SG cleanup and *before* finalizer removal: for AWS platform it calls `deleteAWSEndpointServices` and, if not done, **returns `RequeueAfter: 10s`** — i.e. HCP deletion now BLOCKS on VPC-endpoint/AWSEndpointService cleanup (bounded to 10 min). This step did not exist in 5.0.
2. **Normal path** — **NEW step added**: `r.reconcileDeprecatedConfigurationStatus(hostedControlPlane)` is called right before `reconcileDegradedStatus`. Pure message-only condition (`HostedClusterConfigurationDeprecated`); does not gate rollouts.
3. Just before the final status patch / pause check: **NEW write** `hostedControlPlane.Status.Initialization.ControlPlaneInitialized = ptr.To(true)` (CAPI 1.11 v1beta2 contract). New status sub-resource field the controller now always sets.
4. Helper namespace churn only (no ordering change): `util.HCPOAuthEnabled` → `reconcilerpolicy.HCPOAuthEnabled`, `util.HCPControlPlaneReleaseImage` → `imageregistry.HCPControlPlaneReleaseImage`, `manifests.*` → `cpomanifests.*`, `util.Is/GenerateReconciliation*` → `reconcilerpolicy.*`. Behavior-preserving refactors.
5. Availability derivation (`reconcileAvailabilityAndReadyStatus`): **changed guard** — component-availability is now recomputed when `!alreadyAvailable || healthCheckErr != nil` (5.0 only recomputed when `!alreadyAvailable`).
6. `update()`: the `ValidReleaseInfo` status write no longer re-GETs the HCP first; it now runs inside a `statuspatching.PatchStatus` closure (retry-on-conflict, re-fetch each attempt).

**Verdict: the Reconcile loop ORDERING DID change** — two new steps added in the normal path (deprecated-config status; controlPlaneInitialized write) and one new blocking/requeue step added in the deletion path (AWSEndpointService cleanup).

---

## (b) Full behavioral findings (file:func | what changed | CLASS | why)

### Reconcile core — `hostedcontrolplane_controller.go`

1. `reconcileDeletion` + **NEW** `deleteAWSEndpointServices` | On AWS deletion, lists `AWSEndpointServiceList` in the HCP ns, deletes each, and **requeues every 10s until the CPO finalizer `hypershift.openshift.io/control-plane-operator-finalizer` is gone or a 10-min per-object timeout elapses**; skips only when `ValidAWSIdentityProvider==False`. New k8s LIST/DELETE + new blocking wait in teardown. | **COORDINATION-RISK** | HCP deletion now stalls on another controller (awsprivatelink) removing a finalizer; if that controller is absent/old/stuck the teardown hangs up to 10 min before falling back.
2. **NEW** `reconcileDeprecatedConfigurationStatus` | Adds `HostedClusterConfigurationDeprecated` condition (True when `hypershift.openshift.io/kube-apiserver-verbosity-level` annotation present). | **SELF-CONTAINED** | Reads HCP annotations only; message-only, no rollout/verbosity effect.
3. `Reconcile` | **NEW** always sets `Status.Initialization.ControlPlaneInitialized=true`. | **SELF-CONTAINED** (w.r.t. guest) | Field consumed by management-cluster CAPI v1beta2 contract, not by the 5.0 guest/operands; only risk is an *old management-side* CAPI, out of this task's skew axis.
4. `reconcileAvailabilityAndReadyStatus` | Component availability is now ALSO recomputed whenever `healthCheckErr != nil` (not just when not-yet-available). | **SELF-CONTAINED** | Pure internal recompute; changes how the Available/message is derived but calls the same component checks.
5. `reconcileAvailabilityStatus` | In the `healthCheckErr != nil` branch, now appends `componentsNotAvailableMsg` to the message. | **SELF-CONTAINED** | Message composition only.
6. `validateAWSKMSConfig` | **Reordered + new guard + new fallback.** (a) Now checks `KMS.AWS.Region == ""` FIRST and sets `ValidAWSKMSConfig=False / InvalidConfigurationReason`; (b) guest-token acquisition moved behind an injectable `r.getGuestToken`; (c) on token failure it no longer just reports Unknown — it calls **NEW** `validateKMSKeyDirect`; (d) KMS client now built with explicit `o.Region = kmsRegion`; (e) probe plaintext `"text"`→`"test"`. | **COORDINATION-RISK** | A 5.0-created HCP whose spec left `KMS.AWS.Region` empty (previously tolerated) is now flipped to `ValidAWSKMSConfig=False`, changing the condition a 5.0 guest relied on without any manifest change.
7. **NEW** `validateKMSKeyDirect` | When the guest API is unavailable, issues a direct `kms.Encrypt` with the CPO's OWN AWS creds and maps AWS error codes (NotFound/Disabled/… → False; AccessDenied → Unknown) to `ValidAWSKMSConfig`. | **SELF-CONTAINED** (new imperative AWS call) | New cloud API call on CPO credentials; independent of guest/operand version, but it is a new side-effecting AWS request the 5.0 CPO never made.
8. **NEW** `defaultGetGuestToken` | Extracts the guest-SA-token minting into an injectable seam (calls `GetGuestClusterClient` + `CreateTokenForServiceAccount` on `KASContainerKMSProviderServiceAccount`). | **ASSUMES-NEWER-GUEST** (no new verb) | Same TokenRequest call as 5.0 — behavior preserved; listed because the KMS validation path now branches on its failure rather than bailing.
9. `reconcileKonnectivityCerts` | **Protocol change.** 5.0 used ONE `konnectivity-signer` for all konnectivity certs. Main keeps the legacy signer but ADDS 4 dedicated signers (server-serving/cluster-serving/server-auth/client-auth) and **re-signs** server, cluster, client, and agent certs each from its own dedicated signer; the CA bundle now aggregates legacy + all 4. | **COORDINATION-RISK** | Multi-step cert migration: konnectivity-server (control plane) and konnectivity-agent (guest data plane, from the 5.0 payload) must each trust the new aggregate CA during rotation; mismatched rotation ordering could transiently break the konnectivity tunnel, though the legacy signer is retained to soften this.
10. `reconcileGCPPlatformCerts` | **NEW** reconciles `GCPWorkloadIdentityFederationWebhookServingCert` (127.0.0.1 client+server auth). | **SELF-CONTAINED** | New self-signed cert for a GCP webhook; additive.
11. `healthCheckKASEndpoint` / `healthCheckKASLoadBalancers` | Now takes `*kas.KASHealthMetrics` and emits request-duration + available gauge on every probe. | **SELF-CONTAINED** | Observability only; probe semantics unchanged.
12. `update` / `reconcileDeletion` / `reconcileDefaultSecurityGroup` / `removeCloudResources` / `reconcileValidIDPConfigurationCondition` / `removeHCPIngressFromRoutes` | All migrated from capture-then-`Status().Patch(MergeFromWithOptimisticLock)` to `statuspatching.PatchStatus[Condition]` closures that **re-fetch and recompute inside a retry loop**; several add explicit "don't clobber a concurrent True/ingress write" re-checks (CloudResourcesDestroyed, route ingress). | **SELF-CONTAINED** | Concurrency/conflict-handling improvement; converges to same conditions, but retry/requeue behavior on conflict changed.
13. `reconcileDefaultSecurityGroup` & `reconcileValidIDPConfigurationCondition` | **NEW staleness guard**: capture `evaluatedGeneration := hcp.Generation`; inside the patch closure, if `hcp.Generation` changed, **return an error to force requeue** instead of applying a stale result. | **SELF-CONTAINED** | New requeue/error behavior on generation races; internal.

### Infra — `infra/infra.go`
14. `ReconcileInfrastructure*` / `reconcileAPIServerServiceStatus` | `sharedingress.KasRouteHostname/ExternalDNSLBPort` → `netutil.KASRouteHostname/ExternalDNSLBPort`; `manifests.*` → `cpomanifests.*`; `util.HCPOAuthEnabled` → `reconcilerpolicy.*`. | **SELF-CONTAINED** | Package-move refactor; no logic change confirmed in diff.

### PKI
15. `pki/ca.go:ReconcileKonnectivityConfigMap` | Signature `(…, konnectivityCA)` → `(…, signers ...*Secret)` aggregating N signers. | **SELF-CONTAINED** | Enables finding #9's aggregate bundle.
16. `pki/konnectivity.go` | 4 new `Reconcile*SignerSecret` CA constructors. | **SELF-CONTAINED** | Additive CA material.
17. `pki/gcp_workload_identity_federation_webhook.go` (NEW file) | New serving-cert reconciler for the GCP WIF webhook. | **SELF-CONTAINED** | Additive.
18. `pki/kas.go`, `cloud/aws/providerconfig.go` | `podspec.KubeconfigKey`/`podspec.AWSCloudProviderName`/`manifests.KubeAPIServerService` → `config.*`/`cpomanifests.*`. | **SELF-CONTAINED** | Constant/package relocation.

### v2 framework — `support/controlplane-component/`
19. `common.go:AdaptPodDisruptionBudget` | **Logic rewrite.** 5.0 always wrote a PDB (SingleReplica→minAvailable=1, HA→maxUnavailable=1). Main **gates the PDB behind a predicate `ControllerAvailabilityPolicy != SingleReplica`** — SingleReplica now gets NO PDB and any existing one is **deleted via the predicate-false path**; HA clears shipped minAvailable and sets maxUnavailable=1. | **SELF-CONTAINED** | Changes which objects the framework creates/deletes based on HCP spec; independent of guest/operand version.
20. `defaults.go:setControlPlaneIsolation` (+ `shortNodeFailureToleration`, `tolerationsTolerateTaint`) | **NEW algorithm.** For api-critical and etcd components, injects short NoExecute tolerations on `not-ready`/`unreachable` taints (10s normal, 60s etcd) unless a user toleration already tolerates the taint (via real `ToleratesTaint` semantics). | **SELF-CONTAINED** | Pod-template defaulting on the management side; affects HA eviction timing, not guest/operand behavior.
21. `defaults.go:replaceContainersImageFromPayload` + `setAnnotations` | `util.HCPControlPlaneReleaseImage`→`imageregistry.*`; karpenter image key literal `"aws-karpenter-provider-aws"`→`karpenterutil.KarpenterProviderAWSImageName`. | **SELF-CONTAINED** | Refactor; same fallback logic.
22. `controlplane-component.go`, `token-minter-container.go` | `util.ImageMetadataProvider`→`imageregistry.ImageMetadataProvider`; `podspec.KubeconfigKey`→`config.KubeconfigKey`. | **SELF-CONTAINED** | Type/constant relocation.

---

## (c) Framework-gating behavior changes & skew-tolerance implications

**The v2 framework's rollout/availability engine is UNCHANGED between 5.0 and main.** Confirmed byte-identical (no diff) across the files that actually implement gating:
- `status.go` (availability / rollout-complete / at-desired-version derivation) — **NO CHANGE**
- `deployment.go`, `statefulset.go` (readiness/rollout evaluation) — **NO CHANGE**
- `builder.go`, `generic-adapter.go` (predicate evaluation, dependency gating, reconcile ordering) — **NO CHANGE**

**Verdict: framework version/rollout/availability gating did NOT change.** The only framework-level behavioral deltas are:
- PDB creation is now predicate-gated (SingleReplica omitted/deleted) — affects node-drain budgets, not component rollout gating.
- Short node-failure tolerations default-injected for api-critical/etcd — affects management-node-failure eviction speed, not skew gating.

**Implication for skew tolerance:** because dependency-availability, rollout-complete, and desired-version logic are untouched, a main CPO will gate and roll 5.0-payload components with exactly the same convergence rules as a 5.0 CPO. The framework itself introduces NO new "assumes-newer" gating. The skew risk is concentrated in the imperative CPO core (section b), not the framework.

---

## (d) Ranked top behavioral hazards (could BREAK/STALL a 5.0 guest under a main CPO; invisible to a manifest diff)

1. **AWSEndpointService teardown wait — `hostedcontrolplane_controller.go:deleteAWSEndpointServices` (COORDINATION-RISK).** New 10s-requeue / 10-min-timeout loop that blocks HCP deletion on another controller removing a finalizer. If the awsprivatelink controller in a 5.0-era environment doesn't honor/remove `control-plane-operator-finalizer`, deletion stalls up to 10 min per object — a pure control-flow stall no YAML diff reveals.
2. **AWS KMS region guard — `validateAWSKMSConfig` (COORDINATION-RISK).** Now flips `ValidAWSKMSConfig=False` when `spec…KMS.AWS.Region` is empty. A 5.0 HostedControlPlane that legitimately omitted Region now gets an invalid-config condition it never had, potentially degrading/blocking downstream consumers of that condition.
3. **Konnectivity dedicated-signer cert migration — `reconcileKonnectivityCerts` + `pki/ca.go` (COORDINATION-RISK).** Re-signs server/cluster/client/agent konnectivity certs from 4 new per-purpose signers with an aggregate CA. The konnectivity-agent runs on the 5.0 guest data plane; mis-sequenced rotation could transiently drop the konnectivity tunnel (guest↔control-plane). Legacy signer retention mitigates but does not eliminate the window. Manifest diff shows the new secrets but not the rotation-ordering hazard.
4. **New direct-KMS probe fallback — `validateKMSKeyDirect` (SELF-CONTAINED, new imperative AWS call).** When the 5.0 guest API is unreachable, the CPO now fires a `kms.Encrypt` with its own creds that it never issued before — new AWS-side side effect / IAM surface + new condition semantics (Unknown vs False) a 5.0 guest's tooling may not expect.
5. **Availability recompute on health-check error — `reconcileAvailabilityAndReadyStatus` (SELF-CONTAINED).** `Available` is now re-derived from full component availability whenever the KAS LB health probe errors, even for an already-Available HCP. Against a 5.0 guest whose KAS LB health flaps, this can flip `HostedControlPlaneAvailable` and rewrite its message in ways the prior code suppressed — a behavioral status change with no manifest footprint.

---

## Confidence / UNCONFIRMED notes
- Findings derived from Go diff logic; no compilation performed (per instructions).
- Whether `ValidAWSKMSConfig=False` (#2) hard-blocks KAS vs. merely annotates is **UNCONFIRMED** from this diff alone (depends on degraded/validity aggregation elsewhere).
- Konnectivity agent runs on the guest; exact rotation ordering risk (#3) is reasoned, not proven to break — **UNCONFIRMED** severity.
- Main SHA mismatch (`220e0e73` actual vs `253c3f5c` cited) — **UNCONFIRMED** equivalence.
