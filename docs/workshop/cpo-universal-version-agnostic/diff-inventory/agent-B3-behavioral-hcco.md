# Agent B3 — Behavioral (logic/state-machine) diff: HCCO guest-facing controllers

**Scope:** `control-plane-operator/hostedclusterconfigoperator/**/*.go`
**Diff:** `git diff upstream/release-5.0..upstream/main` (two-dot)
**main commit:** 253c3f5c790d08668d4aa365688e88a0ab950af8 (local `upstream/main` = 220e0e73a4…; `upstream/release-5.0` = b9d6ae1c63…)
**Method:** read Go control flow only; no compile. Resource *shape* drift is out of scope (covered elsewhere) — findings here are about LOGIC, state transitions, guest-API interactions, and error/requeue behavior.

**Skew classes:** SELF-CONTAINED | ASSUMES-NEWER-GUEST (new logic calls/expects a guest-API verb/resource/field/condition a 5.0 guest may lack — the key HCCO skew risk) | ASSUMES-NEWER-OPERAND (expects a newer CRD/operator version on the *management*/CAPI side) | COORDINATION-RISK (guest-side protocol that can stall mid-transition under skew).

---

## Executive summary

- **reencryption state machine: NOT changed** (the ReadOnlyDeploy→WritePromote→Migrating transitions, convergence detection, and StorageVersionMigration polling inside `reconcile()` are byte-identical). Only the *outer status-patch wrapper* changed (now uses `statuspatching.PatchStatus` + `SyncCondition`, and metric emission is unconditional). SELF-CONTAINED.
- **inplaceupgrade state machine: changed in two small but real ways** — (1) capacity is now clamped to the candidate count before slicing (crash-guard), and (2) node discovery now reads CAPI `Machine.Status.NodeRef` via the **v1beta2** API (`IsDefined()` instead of `!= nil`). The cordon/drain/MCD-pod/wait protocol is otherwise unchanged. The v1beta2 bump is an OPERAND-version assumption, not a guest assumption.
- **New controller `usercabundle`** split out of `resources`: same create/delete logic, but now (a) also keys off `Proxy.TrustedCA`, (b) sources from the CPO-managed `TrustedCABundleConfigMap` instead of `UserCAConfigMap`, and (c) runs on its own predicate/queue. Behavioral reconcile-ownership change.
- **New guest-API writes in `resources`:** a GCP workload-identity webhook stack (ClusterRole + ClusterRoleBinding + MutatingWebhookConfiguration) and a new **RBAC ValidatingAdmissionPolicy** (`kas.AdmissionPolicyNameRBAC`) guarding two guest ClusterRoleBindings. Both are new imperative guest writes.
- **Ingress passthrough port resolution rewritten** to support `HostNetwork` strategy and to read `IngressController.Status/Spec.EndpointPublishingStrategy` from the guest — new guest-state read + new branch.
- Large `resources.go` churn is overwhelmingly **mechanical refactor** (manifests moved to `pkg/manifests/hcco`, helpers moved to `support/reconcilerpolicy` / `support/imageregistry`, CRD/CV/CO/NS reconcilers extracted into a `configuration` package, RBAC list extracted into `rbac.Reconcile`). Call order preserved.
- **drainer, cmca, webhookvalidation, hcpstatus, node, globalps(logic), spotremediation(logic): no behavioral change** (0 logic-line diff or import-only).

---

## (a) Per-controller behavioral-logic changes

### reencryption — `controllers/reencryption/reencryption.go`
- **`Reconcile`** | Replaces manual `DeepEqual(hcp.Status, originalHCP.Status)` → `Status().Patch` with `statuspatching.PatchStatus(...)` that re-fetches the HCP and re-applies only `Status.SecretEncryption` + the `EtcdDataEncryptionUpToDate` condition (via `SyncCondition`). Reconcile now runs against a `workingCopy` so the re-fetch doesn't clobber in-memory mutations. | **SELF-CONTAINED** — operates on HCP in the *management* cluster (`cpClient`); the guest-side protocol is untouched.
- **`Reconcile`** | Migration-state gauge `recordMigrationState` is now emitted on **every** reconcile (survives pod restarts) rather than only when status changed; `recordMigrationDuration` still fires only on the transition into `Completed`. | **SELF-CONTAINED** — metrics only, no guest effect.
- **`encryptedResources`** | `util.HCPOAuthEnabled` → `reconcilerpolicy.HCPOAuthEnabled` (import move, identical logic). | **SELF-CONTAINED**.
- **Inner state machine (`reconcile`, ReadOnlyDeploy/WritePromote/Migrating, StorageVersionMigration polling): UNCHANGED.** No diff in those hunks.

### inplaceupgrader — `controllers/inplaceupgrader/inplaceupgrader.go`, `setup.go`
- **`getAvailableCandidates`** | Adds `if capacity > len(candidateNodes) { capacity = len(candidateNodes) }` before `candidateNodes[:capacity]`. Prevents a slice out-of-range panic when requested upgrade capacity exceeds available candidate nodes. Behavior: previously could panic; now upgrades at most the available count. | **SELF-CONTAINED** — pure guard on guest-node list it already read.
- **`getNodesForMachineSet`** | `machine.Status.NodeRef != nil` → `machine.Status.NodeRef.IsDefined()`, driven by CAPI import bump `cluster-api/api/core/v1beta1` → `v1beta2`. Semantics equivalent (nil/empty vs defined) but reads the Machine via the v1beta2 schema. | **ASSUMES-NEWER-OPERAND** — expects the CAPI v1beta2 Machine CRD/conversion on the *management* cluster; NodeRef itself is read on the guest but the type contract is CAPI's, not the guest API's.

### machine / node / spotremediation — `.go` + `setup.go`
- CAPI import bump `v1beta1`→`v1beta2` only; **no logic change** (machine.go also swaps manifests import to `pkg/manifests/hcco`). | **ASSUMES-NEWER-OPERAND** (CAPI v1beta2 on mgmt side).

### nodecount — `controllers/nodecount/controller.go`
- `util.IsReconciliationPaused` → `reconcilerpolicy.IsReconciliationPaused` (import move only). No logic change. | **SELF-CONTAINED**.

### globalps — `controllers/globalps/globalps.go`
- Manifest constructors re-homed `controllers/resources/manifests` → `pkg/manifests/hcco` (`GlobalPullSecret`, `OriginalPullSecret`, `GlobalPullSecretDaemonSet`, `AdditionalPullSecret`). Merge/distribute/delete control flow identical. | **SELF-CONTAINED** (object identity unchanged — risk is manifest-shape, out of scope).

### usercabundle — `controllers/usercabundle/usercabundle.go`, `setup.go` (NEW controller, extracted from resources)
- **`reconcileUserCertCABundle`** | Logic moved out of `resources.reconcileNetworkingAndSecrets` into a standalone controller with its own reconcile loop, pause/releaseImage gating, and a dedicated `trustBundleChangedPredicate`. | behavioral ownership change.
- **Gating condition widened** | 5.0 wrote/deleted the guest `user-ca-bundle` ConfigMap based on `hcp.Spec.AdditionalTrustBundle != nil` **only**. main writes it when `AdditionalTrustBundle != nil` **OR** `proxyTrustedCAName(hcp) != ""` (i.e. `Spec.Configuration.Proxy.TrustedCA.Name`). The delete branch only fires when *both* are absent. | **SELF-CONTAINED** — still a plain ConfigMap CRUD on the guest `openshift-config` ns; broadens when the CM exists but needs no new guest API.
- **Source ConfigMap changed** | main reads `cpomanifests.TrustedCABundleConfigMap(hcp.Namespace)` (the CPO-aggregated trust bundle) instead of 5.0's `cpomanifests.UserCAConfigMap`. | **SELF-CONTAINED** — read is on the management/CP namespace; depends on CPO producing the aggregated CM (same-version CPO, so fine).
- **Watch exclusion** | `resources.Setup` now wraps every `ConfigMap` watch with `notUserCABundlePredicate()` so the resources controller no longer reacts to `user-ca-bundle` (ownership handed to the new controller). | **SELF-CONTAINED**.

### resources (reconcile loop) — `controllers/resources/resources.go`
- **Step extraction, order preserved** | `reconcileCRDs`, `reconcileClusterVersion`, `reconcileClusterOperators`, `reconcileNamespaces` now delegate to `configuration.Reconcile(ctx, client, createOrUpdate, params, <Stage>)`. Same call sequence, same guest objects, data passed via `configuration.ReconcileParams`. | **SELF-CONTAINED** (pure refactor; verified `configuration/*.go` bodies equal the old inline bodies).
- **`reconcileRBAC`** | Entire inline manifest/capability-map loop replaced by `rbac.Reconcile(ctx, client, createOrUpdate, rbac.ReconcileParams{IngressEnabled, IsAROHCP})`. Order and capability-gating (`IngressCapability`) preserved; `RbacCapabilityMap` moved into the `rbac` package. | **SELF-CONTAINED**.
- **`reconcilePlatformSpecificResources`** | **NEW** `case hyperv1.GCPPlatform:` → `reconcileGCPIdentityWebhook(ctx)`. | see (c).
- **`reconcileGCPIdentityWebhook` (NEW)** | Writes to the **guest**: ClusterRole + ClusterRoleBinding (`gcp-workload-identity-federation-webhook` SA in `openshift-authentication`) and a `MutatingWebhookConfiguration` (`pod-identity-webhook.gcp.mutate.io`, FailurePolicy=Ignore). | see (c).
- **`reconcileIngressController`** | Now computes `ingress.EffectiveEndpointPublishingStrategy(ingressController, hcp)` and only fetches the guest `IngressDefaultIngressNodePortService` when strategy==`NodePortService`; for `HostNetwork` it skips the NodePort lookup. On NodePort-service-not-found it now **returns early** (aggregated error) instead of continuing. | new guest-state read; see (c).
- **`reconcileNetworkingAndSecrets`** | Removed the inline `reconcileUserCertCABundle` call (moved to the new controller). Remaining steps unchanged. | **SELF-CONTAINED**.
- **`destroyCloudResources`** | Status-condition write switched to `statuspatching.PatchStatusCondition` (re-fetch + retry) from manual `DeepEqual` + optimistic-lock `Status().Patch`. The guest cloud-resource check that derives the condition is unchanged. | **SELF-CONTAINED** (HCP status on mgmt cluster).
- **`reconcileKubeletConfig` / `mutateKubeletConfig` / `deleteImmutableConfigMapIfNeeded`** | Label constants re-homed `nodepool.KubeletConfigConfigMapLabel`/`NTOMirroredConfigLabel` → `hyperv1.*`. **Same string values** → same guest selectors/behavior. | **SELF-CONTAINED**.
- **`reconcileAzureCloudNodeManager`** | Adds a toleration for `karpenter.sh/unregistered:NoExecute` on the azure-cloud-node-manager DaemonSet. | guest DaemonSet-shape (out of scope) — behaviorally lets the pod schedule onto unregistered Karpenter nodes.
- **`cmd.go`/`operator/config.go`** | Registers `usercabundle.Setup` in `controllerFuncs`; drops the unused `KubeClient()`/`Fatal()` helpers and the `kubeClient` field; `util.*` image helpers → `support/imageregistry`. | **SELF-CONTAINED** (wiring).

### kas admission policies — `controllers/resources/kas/admissionpolicies.go`
- **`ReconcileKASValidatingAdmissionPolicies`** | **NEW** step `reconcileRBACValidatingAdmissionPolicy`. | see (c).
- **`reconcileRBACValidatingAdmissionPolicy` (NEW)** | Creates a guest `ValidatingAdmissionPolicy` named `kas.AdmissionPolicyNameRBAC` on `clusterrolebindings` (UPDATE/DELETE) scoped to resourceNames `hcco-cluster-admin` and `kas-bootstrap-container-cluster-admin`, with a CEL whitelist widened to include `system:admin`. Policy-name constants (`config/mirror/icsp/infra/ntomirroredconfigmaps`) moved to `pkg/kas`. | see (c).

### olm — `controllers/resources/olm/catalogs.go`, `params.go`
- **`reconcileCatalogSource`** | Guest CatalogSource registry-poll interval **10m → 240m** (`RawInterval`/`Interval`). Behavioral change to how often the guest polls catalog registries. | **SELF-CONTAINED** (field value on a guest resource; no new API surface).
- `params.go` image-helper import move only. | **SELF-CONTAINED**.

### Unchanged (verified 0 logic diff)
`drainer/drainer.go`, `drainer/setup.go`, `cmca/configmap_observer.go`, `cmca/setup.go`, `webhookvalidation/webhookvalidation.go`, `webhookvalidation/setup.go`, `hcpstatus/hcpstatus.go`. **webhookvalidation match/scope logic did NOT change.** `node/node.go` = capiv1 import bump only.

---

## (b) High-stakes state machines, diffed step-by-step

### reencryption state machine — **UNCHANGED (inner), wrapper-only change (outer)**
Inner `reconcile()` (the actual protocol) has **no diff**. For the record, the steps that remain identical:
1. Determine encrypted resources (`encryptedResources`) — gated by `HCPOAuthEnabled` (import moved only).
2. ReadOnlyDeploy → WritePromote → Migrating transition logic — unchanged.
3. Convergence detection across KAS pods — unchanged.
4. StorageVersionMigration creation/polling — unchanged.

Outer `Reconcile()` changes (do **not** touch the guest):
- 5.0: `originalHCP := hcp.DeepCopy()` → run `reconcile(hcp)` → if `Status` differs, `cpClient.Status().Patch(MergeFrom(originalHCP))` → then record metrics.
- main: snapshot `previousEncryption` → run `reconcile(workingCopy)` → `statuspatching.PatchStatus` re-fetches HCP and re-applies `Status.SecretEncryption` + syncs only the `EtcdDataEncryptionUpToDate` condition → **always** `recordMigrationState` → `recordMigrationDuration` only on transition into `Completed`.
- **Why it matters:** the retry-safe re-apply assumes this reconciler is the *sole* writer of `Status.SecretEncryption` and that condition (documented in-code). No guest coupling. **SELF-CONTAINED.** No skew hazard for a 5.0 guest.

### inplaceupgrade state machine — **changed (2 points), protocol otherwise intact**
Protocol steps (all preserved): compute target config → select candidate nodes by config/capacity → for each: ensure MCD pod exists on the guest → wait for MCD completion → cordon/annotate → proceed. Diffs:
1. **Capacity clamp** (`getAvailableCandidates`): `candidateNodes[:capacity]` is now preceded by clamping `capacity` down to `len(candidateNodes)`. 5.0 would panic (slice OOR) if `capacity > len(candidateNodes)`; main caps the batch. Pure robustness, no new guest interaction. **SELF-CONTAINED.**
2. **NodeRef read** (`getNodesForMachineSet`): `machine.Status.NodeRef != nil` → `.IsDefined()` under CAPI **v1beta2**. Logic equivalent; depends on the v1beta2 Machine CRD being served (management/CAPI side). **ASSUMES-NEWER-OPERAND.** It still then `Get`s the referenced Node on the guest (unchanged verb).

No change to MCD pod creation, the wait loop, cordon/drain annotations, or the per-node upgrade ack.

---

## (c) NEW guest-API interactions main-HCCO performs that 5.0-HCCO did not

> "Guest-API interaction" = an imperative read/write/watch against the **guest** cluster that 5.0 did not do.

| # | Where | New guest interaction | Verb(s)/Resource | Skew vs 5.0 guest API |
|---|-------|-----------------------|------------------|-----------------------|
| 1 | `resources.go:reconcileGCPIdentityWebhook` → (GCP platform only) | Create/Update guest **ClusterRole** `gcp-workload-identity-federation-webhook` | create/update `rbac.authorization.k8s.io/v1 ClusterRole` | **SELF-CONTAINED** — RBAC v1 GA since forever; 5.0 guest supports it. Only reachable on `GCPPlatform`. |
| 2 | same | Create/Update guest **ClusterRoleBinding** for that SA in `openshift-authentication` | create/update `ClusterRoleBinding` | **SELF-CONTAINED** — RBAC v1 GA; requires the `openshift-authentication` ns to exist (it does on any OCP guest). |
| 3 | same | Create/Update guest **MutatingWebhookConfiguration** `pod-identity-webhook.gcp.mutate.io` (FailurePolicy=Ignore) | create/update `admissionregistration.k8s.io/v1 MutatingWebhookConfiguration` | **SELF-CONTAINED** — admissionregistration/v1 GA; FailurePolicy=Ignore means even a mis-wired webhook won't block guest pod creation. GCP-only. |
| 4 | `kas/admissionpolicies.go:reconcileRBACValidatingAdmissionPolicy` | Create/Update guest **ValidatingAdmissionPolicy** + **…Binding** on `clusterrolebindings` (names `hcco-cluster-admin`, `kas-bootstrap-container-cluster-admin`) | create/update `admissionregistration.k8s.io/v1 ValidatingAdmissionPolicy` + `ValidatingAdmissionPolicyBinding` | **ASSUMES-NEWER-GUEST (likely)** — `ValidatingAdmissionPolicy` is only **GA in k8s 1.30 (OCP 4.17)**. A genuinely older 5.0-era guest payload (≤4.16 / k8s ≤1.29) would serve it only as `v1beta1` or not at all; the `v1` create would fail and this reconcile step would error every loop. *The existing config/mirror/infra VAPs already shipped in 5.0, so a 5.0 guest almost certainly already serves VAP v1 — mark as UNCONFIRMED but low risk; the NEW part is only the extra RBAC policy object, not a new API version.* |
| 5 | `resources.go:reconcileIngressController` + `ingress/reconcile.go:EffectiveEndpointPublishingStrategy` | Reads guest `IngressController.Status.EndpointPublishingStrategy` / `.Spec…` to decide passthrough target port; conditionally **skips** the guest NodePort-Service read when strategy=HostNetwork | read `operator.openshift.io/v1 IngressController` (status+spec fields already present) | **SELF-CONTAINED** — fields (`EndpointPublishingStrategy`, `HostNetwork.HTTPSPort`) exist in 5.0's operator API; KubeVirt-only path. Reads an *existing* field it didn't read before. |

**Count of new guest-API interactions: 5** (3 GCP-webhook writes [GCP-only], 1 new VAP object write, 1 new guest IngressController status/spec read). The one true skew-sensitive class is **#4 (ValidatingAdmissionPolicy)** if the guest predates VAP v1; all others are either SELF-CONTAINED or platform-gated.

---

## (d) Ranked top behavioral hazards a manifest-diff would MISS

1. **`kas/admissionpolicies.go:reconcileRBACValidatingAdmissionPolicy` — ASSUMES-NEWER-GUEST (UNCONFIRMED severity).** A brand-new guest-enforced VAP that *denies UPDATE/DELETE of two cluster-admin ClusterRoleBindings* to everyone outside a CEL whitelist (now incl. `system:admin`). If the guest doesn't serve VAP `v1`, this step errors every reconcile; if it does, it silently hardens guest RBAC in a way no manifest-shape diff reveals as *enforcement behavior*. Coordination-sensitive because the whitelist must match whatever identity the KAS bootstrap container / HCCO actually use.
2. **`usercabundle.go:reconcileUserCertCABundle` — SELF-CONTAINED but behavior-widening.** Gating changed from `AdditionalTrustBundle != nil` to `… OR Proxy.TrustedCA.Name != ""`, and the source CM changed (`UserCAConfigMap`→`TrustedCABundleConfigMap`). A guest `user-ca-bundle` that 5.0 would have *deleted* may now be *kept/written* (or vice-versa) purely from proxy config — a trust-bundle lifecycle change invisible to resource-shape diffing.
3. **`ingress/reconcile.go:ReconcileDefaultIngressPassthroughService` / `EffectiveEndpointPublishingStrategy` — SELF-CONTAINED (KubeVirt).** New HostNetwork branch changes which guest value becomes the passthrough target port (443/HostNetwork.HTTPSPort vs NodePort). Under an unexpected strategy it now hard-errors (`unsupported … strategy`) and returns early, where 5.0 only failed if the NodePort lookup failed — a new failure/requeue mode for KubeVirt ingress.
4. **`resources.go:reconcileGCPIdentityWebhook` — SELF-CONTAINED (GCP-only).** Entirely new guest webhook+RBAC stack reconciled on GCP HostedClusters; a MutatingWebhook with FailurePolicy=Ignore is a new admission actor in the guest. No 5.0 analogue.
5. **`inplaceupgrader.go:getAvailableCandidates` capacity clamp + `getNodesForMachineSet` v1beta2 NodeRef — SELF-CONTAINED / ASSUMES-NEWER-OPERAND.** The clamp changes batch sizing on large upgrades (silently caps instead of panicking); the CAPI v1beta2 read assumes the newer Machine CRD on the management side. A manifest diff sees neither (both are in-memory control flow).

Honorable mention (SELF-CONTAINED, easy to miss): **`olm/catalogs.go` registry-poll 10m→240m** changes guest catalog freshness cadence by 24×; and **reencryption/destroyCloudResources** now use retry-safe `statuspatching` (benign, but changes conflict/requeue behavior on HCP status writes).

---

## Notes / confidence
- reencryption inner protocol unchanged — **high confidence** (empty diff in those hunks).
- VAP v1 availability on a 5.0 guest — **UNCONFIRMED**; flagged because VAP GA is k8s 1.30, but 5.0 already ships other VAPs so the API version is probably present. The *new object* (RBAC policy) is the real delta, not a new API version.
- CAPI v1beta2 is a *management-plane* operand assumption, not guest skew; classified ASSUMES-NEWER-OPERAND accordingly.
- Everything under `configuration/`, `rbac/` package-level `Reconcile` is a verified functional-equivalent extraction (bodies match the deleted inline code); not counted as behavioral change.
