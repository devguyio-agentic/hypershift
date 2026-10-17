# Agent B3 — Behavioral (logic/state-machine) diff: HCCO guest-facing controllers — **4.20 → main**, as a DELTA vs the 5.0 baseline

**Scope:** `control-plane-operator/hostedclusterconfigoperator/**/*.go`
**Diff:** `git diff upstream/release-4.20..upstream/main` (two-dot)
**main tip:** `220e0e73a4…` (5.1-era) · **upstream/release-4.20** tip `48fd596611…`
**5.0 baseline read first:** `/home/sandbox-test/workspace/cpo-from-main/diff-inventory/agent-B3-behavioral-hcco.md`
**Method:** read Go control flow only; no compile. Resource *shape* drift is out of scope (covered by the shape agent). Findings are LOGIC / state transitions / guest-API interaction patterns / error-requeue.

**Skew classes:** SELF-CONTAINED | **ASSUMES-NEWER-GUEST** (new logic calls/expects a guest-API verb/resource/field/condition/apiVersion a ~k8s-1.31 4.20 guest may lack — the key class) | ASSUMES-NEWER-OPERAND (expects newer CRD/operator on the management/CAPI side) | COORDINATION-RISK (guest-side protocol that can stall mid-transition under skew).

> **UNCONFIRMED — guest k8s minor.** Task states the 4.20 guest runs *~k8s 1.31*; I take that as given but flag it UNCONFIRMED. If the 4.20 guest is actually newer (OCP 4.20 nominally tracks a later k8s), the version-skew risks below shrink further. All GA-availability calls (VAP GA @1.30, admissionregistration/v1, pods/log, RBAC v1) assume ≥1.31.

---

## Executive summary — the 4.20→main delta is MUCH larger than 5.0→main

The 5.0→main HCCO diff was dominated by *mechanical refactor* with a handful of new guest writes. The **4.20→main diff is structural**: four controllers that are *brand-new files in main do not exist at all in 4.20's HCCO*, and the resources controller grew several new guest-facing subsystems.

- **reencryption: CHANGED — and it is an ENTIRELY NEW HCCO controller 4.20→main.** (Answering KEY Q1: 5.0→main it was UNCHANGED; 4.20→main it is *net-new*.) The `controllers/reencryption/` package **does not exist in 4.20**. The full `ReadOnlyDeploy → WritePromote → Migrating → Completed` state machine, convergence detection, and StorageVersionMigration create/poll are new. In 4.20 only the *static* `EncryptionConfiguration` generation lived in the HCP controller (`hostedcontrolplane_controller.go` / `v2/kas/secretencryption.go`); there was **no HCCO-driven rotation state machine and no SVM polling at all**. The state machine is also *observable-state-derived* (it reads the EncryptionConfiguration Secret + KAS Deployment hash on the **CP cluster** rather than trusting `history[0].state`).
- **inplaceupgrade: CHANGED, and more than it changed 5.0→main.** (KEY Q3.) 4.20→main adds: (1) the capacity clamp (same as 5.0), (2) CAPI v1beta2 `NodeRef.IsDefined()` (same as 5.0), **plus new in 4.20→main**: a 30s periodic requeue while upgrading, a new `release-version` token-secret key *required* (hard error if missing), per-Machine `NodePoolReleaseVersionAnnotation` writes on the management/CAPI side, an extra MCD-pod gating condition (`MachineConfigDaemonStateDone`), and terminal-pod (Succeeded/Failed) delete-for-retry logic.
- **spotremediation: NEW controller** (absent in 4.20). Watches guest Nodes for `aws-node-termination-handler/*` taints → annotates & deletes the backing CAPI Machine (AWS only).
- **webhookvalidation: NEW controller** (absent in 4.20 as a package) — but the *logic* existed inline in 4.20 `resources.go` (`ensureGuestAdmissionWebhooksAreValid`). Extracted + rewired to also re-evaluate on CP-Service events. Match/scope semantics preserved.
- **usercabundle: NEW controller** (absent in 4.20) — logic extracted from 4.20 `resources.reconcileUserCertCABundle`; gating widened to include `Proxy.TrustedCA.Name`, source CM switched to `TrustedCABundleConfigMap` (same delta the 5.0 agent found, but here it is a *controller-topology* change too).
- **resources.go grew new guest subsystems absent in 4.20:** KAS-connection-checker (deploys guest pods in kube-system that self-PATCH a ConfigMap), DataPlaneConnectionAvailable (guest Pod list + **pods/log** read), metrics-forwarder (guest PodMonitor + Deployment), GCP identity webhook stack, GCP operand-credential secrets, RBAC ValidatingAdmissionPolicy, ingress HostNetwork strategy resolution.
- **globalps: CHANGED** — new guest ServiceAccount, node-affinity rewrite to also target Karpenter nodes, a new CP-side `combined-pull-secret` sync, and defensive pull-secret parsing. (5.0→main was import-only.)
- **hcpstatus: CHANGED** — HCP status write switched from `Status().Update` to a hand-built RFC-6902 JSON Patch with optimistic-lock test op. (5.0→main was unchanged.)
- **konnectivity agent, network, oauth, olm: CHANGED** — HostNetwork/health-host logic, IPv6 OVN config, oauth redirect URI via `net.JoinHostPort`, catalog poll 10m→240m + RedHatMarketplace catalog removed.

---

## (a) Per-controller behavioral-logic changes

### reencryption — `controllers/reencryption/{reencryption,setup,metrics}.go` — **NET-NEW controller**
- **`Reconcile` / `reconcile` (NEW)** | Full rotation lifecycle driven off **observable state**: reads `kas-secret-encryption-config` Secret + KAS Deployment (on **CP cluster**, `cpClient`) to derive `TargetKeyAbsent/ReadOnly/Write` role and KAS convergence, then advances `ReadOnlyDeploy→WritePromote→Migrating→Completed`. `history[0].state` is written for observability only, never read back for phase decisions. | **SELF-CONTAINED** for the CP-side derivation; see Migrating phase for the one guest coupling.
- **`handleMigratingPhase` (NEW)** | Uses library-go `migrators.NewKubeStorageVersionMigrator` to **create and poll `StorageVersionMigration` CRs on the GUEST** (`migration.k8s.io/v1alpha1`, built from `opts.TargetConfig` in `setup.go`), plus guest **discovery** to resolve preferred versions and skip resources "not found in discovery". Requeues 10s (progress) / 60s (failure) / 30s (waiting-for-KAS). | **ASSUMES-NEWER-GUEST (UNCONFIRMED, low)** + **COORDINATION-RISK** — guest must serve `migration.k8s.io/v1alpha1` *and* run a kube-storage-version-migrator that actually processes the SVMs. OpenShift has shipped this migrator/API for many releases so a 1.31 guest almost certainly serves it, but it is an *alpha* group and a genuinely minimal/older guest could 404 the create, stalling Migrating indefinitely. The requeue loop never hard-fails on a missing API group except via `EnsureMigration` error.
- **`Reconcile` status write (NEW)** | `statuspatching.PatchStatus` re-fetches HCP and re-applies only `Status.SecretEncryption` + the `EtcdDataEncryptionUpToDate` condition; metric `recordMigrationState` emitted every reconcile. | **SELF-CONTAINED** (HCP on mgmt cluster; sole-writer assumption documented in-code).

### inplaceupgrader — `controllers/inplaceupgrader/inplaceupgrader.go`, `setup.go`
- **`Reconcile`** | Now returns `RequeueAfter: 30s` unconditionally while an upgrade is in progress — closes the "force-deleted MCD pod → missed deletion event → replacement never created" gap. New steady requeue cadence. | **SELF-CONTAINED** (new requeue behavior).
- **`Reconcile`** | **Now REQUIRES** `tokenSecret.Data["release-version"]`; hard-errors if missing. 4.20 never read this key. | **COORDINATION-RISK** — the token secret is produced by a same-version CPO/ignition path, so fine in-version, but any skew where the token lacks `release-version` makes the controller error every loop.
- **`reconcileInPlaceUpgrade`** | On a node that has *completed* upgrade, writes `hyperv1.NodePoolReleaseVersionAnnotation` onto the backing **CAPI Machine** (mgmt/CAPI side) via `getNodesForMachineSet`'s new `nodeToMachine` map. New management-side write. | **ASSUMES-NEWER-OPERAND** (CAPI v1beta2 Machine) + SELF-CONTAINED annotation.
- **`reconcileUpgradePods`** | Idle-pod delete gate adds a third condition: `MachineConfigDaemonStateAnnotationKey == Done` (not just config/drainer match) before deleting the MCD pod; uses `k8sutil.DeleteIfNeeded`. **New branch:** a pod in `Succeeded/Failed` phase on a node that *still needs* upgrade is deleted to force a retry. | **SELF-CONTAINED** — changes guest MCD-pod lifecycle timing; all on guest Pods it already managed.
- **`getAvailableCandidates`** | Capacity clamp `if capacity > len(candidateNodes)` (same crash-guard as 5.0). | **SELF-CONTAINED.**
- **`getNodesForMachineSet`** | `NodeRef != nil` → `NodeRef.IsDefined()` under CAPI **v1beta2** (same as 5.0); now also returns a `nodeToMachine` map. | **ASSUMES-NEWER-OPERAND.**

### spotremediation — `controllers/spotremediation/{spotremediation,setup}.go` — **NET-NEW controller (AWS only)**
- **`Reconcile` (NEW)** | Watches guest **Nodes**; if a node carries an `aws-node-termination-handler/*` taint, looks up the backing **CAPI Machine** (mgmt side) via `MachineAnnotation`/`ClusterNamespaceAnnotation`, and if it is labeled `interruptible-instance` and not already deleting, annotates it with the interruption signal and **deletes the Machine**. | **SELF-CONTAINED** guest read (Node core/v1) + mgmt-side Machine delete. **ASSUMES-NEWER-OPERAND** (CAPI v1beta2). New guest Node watch is a new interaction but uses GA core/v1.

### webhookvalidation — `controllers/webhookvalidation/{webhookvalidation,setup}.go` — **NET-NEW package, logic pre-existing**
- **`Reconcile`/`reconcileWebhook`/`buildDisallowedURLs`** | Extracted from 4.20 `resources.ensureGuestAdmissionWebhooksAreValid`. Lists/gets guest `Validating`+`MutatingWebhookConfiguration`, deletes any whose `ClientConfig.URL` targets a CP Service lacking the `AllowGuestWebhooksServiceLabel`. **Behavioral additions:** (1) now also triggered by **CP-Service events** (sentinel `*` request → list-all), so relabeling a Service re-evaluates webhooks immediately; (2) type dispatch via `req.Namespace` carrying "validating"/"mutating". Match/scope/disallowed-URL construction is byte-equivalent to 4.20. | **SELF-CONTAINED** (admissionregistration/v1 GA; same enforcement as 4.20, just event-driven).

### usercabundle — `controllers/usercabundle/{usercabundle,setup}.go` — **NET-NEW controller**
- **`reconcileUserCertCABundle`** | Extracted from 4.20 `resources.reconcileUserCertCABundle`. Gating widened: writes guest `user-ca-bundle` CM when `AdditionalTrustBundle != nil` **OR** `Proxy.TrustedCA.Name != ""` (4.20 keyed on `AdditionalTrustBundle` only); deletes only when **both** absent. Source CM is CPO-aggregated `TrustedCABundleConfigMap` (4.20 used `UserCAConfigMap`). Own predicate/queue. | **SELF-CONTAINED** — plain CM CRUD on guest `openshift-config`; trust-bundle lifecycle widened (matches the 5.0 finding).

### resources (reconcile loop) — `controllers/resources/resources.go`
- **Reconcile restructured into step groups** | `reconcileRegistryAndIngress`, `reconcileAPIServicesAndOAuth`, `reconcileNetworkingAndSecrets`, `reconcileOLM`, `reconcileStorageAndMisc`, `reconcilePlatformSpecificResources`, `reconcileClusterRecovery`. Call order preserved vs 4.20's flat list; `usercabundle` and `ensureGuestAdmissionWebhooks` removed (moved to own controllers). | **SELF-CONTAINED** (refactor) — but the groups below are *new behavior*.
- **`reconcileKASConnectionChecker` (NEW)** | Creates guest **ServiceAccount + ConfigMap + 3-replica Deployment** in `kube-system`. The deployed pods run a curl loop that hits `kubernetes.default.svc/version` (or IBM `/livez?exclude=…`) and **PATCH the shared guest ConfigMap** (`strategic-merge-patch+json`) with `lastSucceeded`. Pods use `automountServiceAccountToken=true`, explicit UID 1000, autoscaler safe-to-evict. | **SELF-CONTAINED** API-version-wise (core/v1 + GA) but a **NEW guest workload** that self-mutates a guest ConfigMap — entirely absent in 4.20.
- **`reconcileDataPlaneConnectionAvailable` (NEW)** | `CountAvailableNodes` on guest; lists guest Pods `app=konnectivity-agent` in kube-system via **uncached** client; **reads pod logs** (`GetPodLogs` → `pods/log` subresource) to prove the tunnel; sets `DataPlaneConnectionAvailable` HCP condition via `statuspatching.PatchStatusCondition`. | **SELF-CONTAINED** (pods/log GA) — NEW guest read pattern (log subresource) absent in 4.20.
- **`reconcileControlPlaneConnectionAvailable` (NEW)** | Counts guest nodes → sets `ControlPlaneConnectionAvailable` HCP condition. | **SELF-CONTAINED.**
- **`reconcileMetricsForwarder` (NEW)** | Creates a guest **PodMonitor** (`monitoring.coreos.com/v1`), Deployment (haproxy TCP passthrough), ConfigMap, serving-CA CM. | **ASSUMES-NEWER-GUEST (low/UNCONFIRMED)** — requires the guest to serve `monitoring.coreos.com/v1` PodMonitor CRD (present on any OCP guest with the monitoring stack; risk only if monitoring CRDs absent). NEW vs 4.20.
- **`reconcilePlatformSpecificResources` → `reconcileGCPIdentityWebhook` (NEW, GCP only)** | Writes guest **ClusterRole** (`serviceaccounts` get/list/watch) + **ClusterRoleBinding** (SA `gcp-workload-identity-federation-webhook` in `openshift-authentication`) + **MutatingWebhookConfiguration** `pod-identity-webhook.gcp.mutate.io` (FailurePolicy=Ignore, on pod CREATE). | **SELF-CONTAINED** (RBAC v1 + admissionregistration/v1 GA) — new guest admission actor; GCP-only. Same category as the 5.0 GCP webhook finding, but **net-new vs 4.20**.
- **GCP operand creds — `resources/gcp/gcp.go:SetupOperandCredentials` (NEW, GCP only)** | Writes guest **Secrets** in `openshift-image-registry` and `openshift-cluster-csi-drivers` (workload-identity JSON; distinct keys `service_account.json` vs `application_default_credentials.json`). | **SELF-CONTAINED** (core Secrets) — NEW vs 4.20 (4.20 had only AWS + inline Azure operand creds).
- **Azure operand creds — `resources/azure/azure.go` (extracted)** | 4.20 had this inline in resources.go; moved to `azure/` package. Logic equivalent (managed vs self-managed client-ID selection). | **SELF-CONTAINED** (refactor).
- **`reconcileIngressController` + `ingress/reconcile.go`** | New `EffectiveEndpointPublishingStrategy` reads guest IngressController **Status/Spec `EndpointPublishingStrategy`** (and `HostNetwork.HTTPSPort`) to pick the KubeVirt passthrough target port; skips the guest NodePort-Service read for HostNetwork; hard-errors + returns early on NodePort-not-found. `ReconcileDefaultIngressController` gained an explicit `endpointPublishingStrategy` param (from HCP `OperatorConfiguration.IngressOperator`) and an OpenStack branch. | **SELF-CONTAINED** (operator.openshift.io/v1 fields present at 1.31) — same category as 5.0; KubeVirt-only new failure mode.
- **`destroyCloudResources`** | Condition write via `statuspatching.PatchStatusCondition`. | **SELF-CONTAINED.**
- **kas `admissionpolicies.go:reconcileRBACValidatingAdmissionPolicy` (NEW)** | Guest **ValidatingAdmissionPolicy** `kas.AdmissionPolicyNameRBAC` on `clusterrolebindings` (UPDATE/DELETE) scoped to resourceNames `hcco-cluster-admin`, `kas-bootstrap-container-cluster-admin`, CEL whitelist widened with `system:admin` + `KASBootstrapContainerUser`. | **SELF-CONTAINED (UNCONFIRMED)** — VAP `v1` GA @ k8s 1.30, so a 1.31 guest serves it; the other config/mirror/infra VAPs already shipped in 4.20 so VAP v1 is definitely present. New *enforcement object*, same as 5.0.
- **`reconcileRegistryAndIngress`** | Registry removed-management-state branch no longer gated on `ManagementState == Removed` prerequisite (now only platform-gated). | **SELF-CONTAINED** behavioral gate change.
- **`olm/catalogs.go`** | `RedHatMarketplace` catalog source **removed**; registry poll **10m → 240m**. | **SELF-CONTAINED** (guest CatalogSource cadence 24×; one fewer guest catalog).

### globalps — `controllers/globalps/globalps.go`
- **`reconcileGlobalPullSecret`** | Creates a new guest **ServiceAccount** (`global-pull-secret-syncer`) and sets it on the DaemonSet; **replaces the `nodeSelector{globalPSLabelKey:true}` with a node `Affinity`** that also matches Karpenter nodes (`karpenter.sh/nodepool` Exists) — DS now lands on Karpenter nodes too. New `syncCombinedPullSecret` writes a CP-side `combined-pull-secret` (skips if CPO hasn't created it). Defensive `auths`-key parsing (errors instead of panicking on malformed pull secret). | **SELF-CONTAINED** (guest DaemonSet/SA; Karpenter label is a scheduling selector, not an API call) — behavioral scope widening absent in 4.20.

### hcpstatus — `controllers/hcpstatus/hcpstatus.go`
- **`Reconcile`** | HCP status write switched from `Status().Update` to a **hand-built RFC-6902 JSON Patch** (`buildStatusPatch`) with an optimistic-lock `test /metadata/resourceVersion` op, specifically to preserve `+required +nullable` ClusterVersionStatus fields that JSON merge-patch would null-delete. | **SELF-CONTAINED** (HCP status on mgmt cluster) — changed conflict/requeue behavior.

### node / machine / nodecount / drainer
- **node/machine:** CAPI v1beta1→v1beta2 import bump only (+log line removed). | **ASSUMES-NEWER-OPERAND.**
- **nodecount:** `util.IsReconciliationPaused` → `reconcilerpolicy.*` (import move). | **SELF-CONTAINED.**
- **drainer:** const-extraction of the MCD pod name format only; logic identical. | **SELF-CONTAINED.**

### network / konnectivity / oauth
- **network `ReconcileNetworkOperator`** | New `hasIPv6Network` param → sets KubeVirt OVN `IPv6.InternalJoinSubnet` default; `applyOVNConfig` now also applies IPv6 join/transit subnets, V4/V6 internal subnets. | **SELF-CONTAINED** (guest Network operator fields).
- **konnectivity `ReconcileAgentDaemonSet`** | `HostNetwork` now platform-conditional (false for IBM Cloud); adds `--health-server-host` + probe `Host` switching between 127.0.0.1 (hostnetwork) and pod IP; new `konnectivity-agent` ServiceAccount. | **SELF-CONTAINED** (guest DaemonSet shape/behavior).
- **oauth** | Redirect URIs built via `net.JoinHostPort` (IPv6-safe) instead of `%s:%d`. | **SELF-CONTAINED.**

---

## (b) High-stakes state machines, diffed step-by-step

### reencryption — **NET-NEW 4.20→main** (vs UNCHANGED 5.0→main)

**4.20:** no HCCO reencryption controller; no rotation state machine; no StorageVersionMigration polling. The HCP controller only *generated* the static `EncryptionConfiguration` (`v2/kas/secretencryption.go`). There was **no observable `ReadOnlyDeploy/WritePromote/Migrating` progression and no guest SVM interaction**.

**main:** new controller with this derived-from-observable-state machine:
1. `Spec.SecretEncryption == nil` → clear TargetKey, remove `EtcdDataEncryptionUpToDate` condition.
2. Compute spec vs status fingerprints. ActiveKey empty → **InitialBootstrap** (set ActiveKey, condition True).
3. TargetKey set → **InProgressRotation**: read `kas-secret-encryption-config` Secret (CP) → `deriveTargetKeyRole` (Absent/ReadOnly/Write) + config hash; read KAS Deployment (CP) → `isKASConverged(hash)`. Transition table:
   - Absent → ReadOnlyDeploy, requeue 30s (waiting for CPO to add key).
   - ReadOnly & !converged → ReadOnlyDeploy, requeue 30s.
   - ReadOnly & converged → WritePromote.
   - Write & !converged → WritePromote, requeue 30s.
   - Write & converged → **Migrating** → `handleMigratingPhase`.
4. **Migrating** (the only guest coupling): `migrator.HasSynced()` gate; for each `encryptedResources` GR call `EnsureMigration` → **creates/polls `StorageVersionMigration` on the GUEST**; skip GRs "not found in discovery"; aggregate errors → condition Failed (requeue 60s) / InProgress (requeue 30s) / all-finished → **completeRotation** (promote TargetKey→ActiveKey, condition True).

**Skew verdict:** CP-side derivation is SELF-CONTAINED. The Migrating phase is **ASSUMES-NEWER-GUEST (UNCONFIRMED, low) + COORDINATION-RISK** against a 1.31 guest: needs `migration.k8s.io/v1alpha1` served *and* an SVM-processing controller in the guest. Because this entire machine is new since 4.20, a 4.20 guest has never had HCCO drive SVMs before — but OpenShift has shipped the migrator/API for years, so practical risk is low; the failure mode if absent is a *stuck Migrating phase*, not a crash.

### inplace-upgrade — **changed more than 5.0→main**

Protocol (preserved): compute target config hash → select candidates by config/capacity → ensure MCD pod per node on guest → cordon/drain/ack. 4.20→main diffs:
1. **Periodic 30s requeue** while upgrading (new) — self-heals missed force-deleted-pod events.
2. **`release-version` token key now mandatory** (new) — hard error if absent.
3. **Per-Machine `NodePoolReleaseVersionAnnotation`** written on completed nodes (new, mgmt/CAPI side).
4. **Idle-pod delete now also requires `MCDState == Done`** (new third condition) before tearing down the MCD pod.
5. **Terminal-pod retry** (new): Succeeded/Failed pod on a still-needs-upgrade node is deleted to force a fresh MCD pod.
6. Capacity clamp + `NodeRef.IsDefined()` v1beta2 (same two as 5.0).

All guest interactions use pre-existing verbs (Node get/update, Pod create/delete). Skew: **SELF-CONTAINED** on the guest side; **ASSUMES-NEWER-OPERAND** for CAPI v1beta2 + the Machine annotation write.

---

## (c) Every NEW guest-API interaction main-HCCO performs that 4.20-HCCO did NOT, with 1.31 skew assessment

| # | Where | New guest interaction | Verb/Resource (apiVersion) | 1.31 skew |
|---|-------|-----------------------|----------------------------|-----------|
| 1 | reencryption `handleMigratingPhase` | create/poll StorageVersionMigration on guest + guest **discovery** | create/list `migration.k8s.io/v1alpha1 StorageVersionMigration`; discovery | **ASSUMES-NEWER-GUEST (UNCONFIRMED, low)** — alpha group; OCP ships it, so likely served @1.31; failure = stuck Migrating |
| 2 | spotremediation `Reconcile` | watch/get guest Node, read NTH taints | watch/get `v1 Node` | SELF-CONTAINED (core GA) — new watch, no version risk |
| 3 | resources `reconcileKASConnectionChecker` | create guest SA+ConfigMap+Deployment in kube-system; **deployed pods self-PATCH a guest ConfigMap** | create `v1 ServiceAccount/ConfigMap`, `apps/v1 Deployment`; runtime `PATCH configmaps` | SELF-CONTAINED (GA) — brand-new guest workload that self-mutates guest state |
| 4 | resources `reconcileDataPlaneConnectionAvailable` | list guest konnectivity-agent pods + **read pod logs** | list `v1 Pod`, get `pods/log` subresource | SELF-CONTAINED (GA) — new log-read pattern |
| 5 | resources `reconcileMetricsForwarder` | create guest PodMonitor+Deployment+CMs | create `monitoring.coreos.com/v1 PodMonitor`, `apps/v1 Deployment`, `v1 ConfigMap` | **ASSUMES-NEWER-GUEST (low/UNCONFIRMED)** — needs PodMonitor CRD (present on OCP monitoring guests) |
| 6 | resources `reconcileGCPIdentityWebhook` (GCP) | create guest ClusterRole + CRB + MutatingWebhookConfiguration | create `rbac.authorization.k8s.io/v1`, `admissionregistration.k8s.io/v1 MutatingWebhookConfiguration` | SELF-CONTAINED (GA) — GCP-only new admission actor |
| 7 | resources `gcp.SetupOperandCredentials` (GCP) | create guest Secrets in image-registry / csi-drivers ns | create `v1 Secret` | SELF-CONTAINED (GA) — GCP-only |
| 8 | kas `reconcileRBACValidatingAdmissionPolicy` | create guest VAP + binding on clusterrolebindings | create `admissionregistration.k8s.io/v1 ValidatingAdmissionPolicy(+Binding)` | SELF-CONTAINED (VAP GA @1.30; other VAPs already in 4.20) — new enforcement object |
| 9 | resources `reconcileIngressController`/`EffectiveEndpointPublishingStrategy` | read guest IngressController Status/Spec HostNetwork field | get `operator.openshift.io/v1 IngressController` | SELF-CONTAINED — reads existing field; KubeVirt-only |
| 10 | globalps `reconcileGlobalPullSecret` | create guest ServiceAccount; DS node-affinity targets Karpenter nodes | create `v1 ServiceAccount` | SELF-CONTAINED (GA) |
| 11 | inplaceupgrader (mgmt-side, listed for completeness) | per-Machine release-version annotation | update CAPI `Machine` | ASSUMES-NEWER-OPERAND (not a guest call) |

**Count of NEW guest-API interactions vs 4.20: ~10** (items 1–10; #11 is management-side). **1.31-risky: effectively 1–2** — #1 StorageVersionMigration (alpha group, UNCONFIRMED but OCP-shipped) is the only one touching an apiVersion a minimal 1.31 guest might not serve; #5 PodMonitor is CRD-dependent (low). Everything else is GA core/RBAC/admissionregistration/v1 that a 1.31 guest serves. **The 5.0 baseline counted 5 new interactions (3 GCP-only, 1 VAP, 1 ingress read); 4.20→main roughly doubles that to ~10**, but the *version-skew-sensitive* subset stays small because a ~1.31 guest already serves the GA surfaces.

---

## (d) DELTA vs the 5.0 baseline

| Area | 5.0→main (baseline) | 4.20→main (this report) |
|------|---------------------|-------------------------|
| **reencryption** | UNCHANGED (inner protocol byte-identical; wrapper-only) | **NET-NEW controller** — full state machine + guest SVM polling introduced |
| **inplace-upgrade** | 2 changes (capacity clamp + v1beta2 NodeRef) | **6 changes** — +30s requeue, +mandatory `release-version`, +Machine annotation, +MCDState=Done gate, +terminal-pod retry |
| **spotremediation** | no logic change | **NET-NEW controller** |
| **webhookvalidation** | no change | NET-NEW package (logic pre-existing in 4.20 resources.go), +CP-Service-event trigger |
| **usercabundle** | new controller (extracted from resources) | same extraction + same gating widening |
| **KAS connection checker** | not noted (present in 5.0) | **NET-NEW vs 4.20** — guest pods self-PATCH ConfigMap |
| **DataPlaneConnection / pods/log read** | not noted | **NET-NEW vs 4.20** |
| **metrics-forwarder** | not noted | **NET-NEW vs 4.20** (guest PodMonitor) |
| **GCP identity webhook + operand creds** | GCP webhook was new | both NET-NEW vs 4.20 |
| **RBAC VAP** | new object | same (new object) |
| **globalps** | import-only | **CHANGED** — guest SA, Karpenter affinity, combined-pull-secret sync |
| **hcpstatus** | unchanged | **CHANGED** — JSON-Patch with optimistic lock |
| **konnectivity/network/oauth** | not in scope of 5.0 findings | CHANGED (HostNetwork, IPv6, redirect URI) |
| **New guest-API interactions** | 5 (1 risky: VAP, UNCONFIRMED) | **~10 (1–2 risky: SVM alpha + PodMonitor, both UNCONFIRMED-low)** |

**Net new hazards not present in the 5.0 baseline:** reencryption-as-new-controller, inplace-upgrade mandatory `release-version` + extra gates, spotremediation controller, KAS-connection-checker guest workload, DataPlaneConnection pods/log read, metrics-forwarder PodMonitor, globalps Karpenter/SA/combined-secret, hcpstatus JSON-Patch. **≈8 hazard families are NEW relative to the 5.0 baseline** (the 5.0 baseline's own top-5 — RBAC VAP, usercabundle gating, ingress HostNetwork, GCP webhook, inplace clamp — all still apply here too).

---

## (e) Ranked top behavioral hazards a manifest-diff would MISS (4.20→main)

1. **`reencryption/reencryption.go:handleMigratingPhase` — ASSUMES-NEWER-GUEST + COORDINATION-RISK (UNCONFIRMED).** An entire rotation state machine that *did not exist* in 4.20 now creates/polls `StorageVersionMigration` on the guest. If the 1.31 guest doesn't serve `migration.k8s.io/v1alpha1` or lacks an SVM processor, rotation **stalls in Migrating forever** with `EtcdDataEncryptionUpToDate=False`. Biggest new/unseen behavior since it was UNCHANGED 5.0→main.
2. **`resources.go:reconcileKASConnectionChecker` — SELF-CONTAINED but net-new guest workload.** Deploys 3 guest pods into `kube-system` with `automountServiceAccountToken=true` that **self-PATCH a guest ConfigMap** every 60s using the SA token. No 4.20 analogue; a manifest-shape diff sees static YAML, not that the controller now stands up a privileged-ish guest workload that writes guest state at runtime.
3. **`inplaceupgrader.go:reconcileInPlaceUpgrade` — COORDINATION-RISK.** New **mandatory** `release-version` token key (hard error if absent) + new `MCDState==Done` idle-pod gate + terminal-pod delete-for-retry. Changes upgrade progression and can error every loop under token skew. Pure control-flow, invisible to shape diffing.
4. **`resources.go:reconcileDataPlaneConnectionAvailable` — SELF-CONTAINED, new guest read pattern.** Reads **pod logs** (`pods/log`) from guest konnectivity-agent pods to drive a new HCP condition. New imperative guest read absent in 4.20.
5. **`resources/gcp/gcp.go:SetupOperandCredentials` + `resources.go:reconcileGCPIdentityWebhook` — SELF-CONTAINED (GCP-only), net-new.** Whole new GCP guest stack (workload-identity Secrets in two guest namespaces + MutatingWebhook + RBAC). New admission actor and credential writer on GCP HostedClusters with no 4.20 precedent.

**Honorable mentions:** globalps Karpenter-node affinity + guest SA + combined-pull-secret sync (behavior widening, shape-invisible); hcpstatus JSON-Patch optimistic-lock (changes conflict/requeue semantics); olm catalog poll 10m→240m (24× freshness) + RedHatMarketplace catalog removed; webhookvalidation now re-fires on CP-Service events.

---

## Notes / confidence
- **reencryption is net-new 4.20→main** — high confidence (package absent in 4.20 `git ls-tree`; no `EncryptionMigrationState`/`ReadOnlyDeploy`/`StorageVersionMigration`-polling logic anywhere in 4.20 HCCO).
- **spotremediation / webhookvalidation / usercabundle packages absent in 4.20** — high confidence (file-listing). webhookvalidation + usercabundle logic *did* exist inline in 4.20 `resources.go`; they are topology moves + small behavior changes, not purely new behavior.
- **Guest k8s minor (~1.31)** — UNCONFIRMED per task; GA-availability verdicts assume ≥1.31. If the real guest is newer, skew shrinks.
- **StorageVersionMigration group** — the setup uses `sigs.k8s.io/kube-storage-version-migrator` (`migration.k8s.io/v1alpha1`, the long-standing OpenShift one), not the upstream `storagemigration.k8s.io` 1.30 alpha. Practical availability on a 1.31 OCP guest is high; marked UNCONFIRMED-low rather than high-risk.
- **CAPI v1beta2** is a management/operand assumption (node/machine/inplaceupgrader/spotremediation), not guest skew — ASSUMES-NEWER-OPERAND throughout.
- Extracted packages (`configuration/`, `rbac/`, `azure/`) verified as functional-equivalent moves of 4.20 inline code; not counted as new behavior except where logic changed (noted inline).
