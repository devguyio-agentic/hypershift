# Agent-3 — HCCO / HyperShift-API / CPO-Deployment / Ignition diff inventory

**Diff basis:** `git diff upstream/release-5.0..upstream/main` (two-dot).
**Resolved refs at analysis time:** `upstream/main = 220e0e73a47e…`, `upstream/release-5.0 = b9d6ae1c6345…`.
**NOTE (UNCONFIRMED):** task cited main commit `253c3f5c…`; the local `upstream/main` resolves to `220e0e73…`. Findings follow the local `upstream/main` as instructed. No material drift expected but flag if the exact SHA matters.
**Method:** pure diff analysis, read-only, no compile.

Scope regions: HCCO (`control-plane-operator/hostedclusterconfigoperator/`), API (`api/hypershift/`), HO→CPO deployment (`hypershift-operator/controllers/hostedcluster/`), ignition (`ignition-server/`).

---

## (a) Summary table — region × category

| Region | C4 API-field | C6 Semantic-protocol | C7 CPO-image-res | C12 Guest-resource-shape | C10 Featuregate/cap |
|---|---|---|---|---|---|
| **api/hypershift** | ✅ heavy (HCP status `Initialization`, HC `operatorConfiguration` tree, AWS/Azure/KubeVirt/endpoint resourceTags, conditions) | – | – | – | ✅ `HCPUserFacingOperatorLogs` gate on whole operatorConfiguration tree |
| **HCCO** | reads HC `Capabilities`/`Channel`; writes HCP `secretEncryption`/`EtcdDataEncryptionUpToDate` via new status-patching helper | ✅ reencryption status-patch rewrite; inplaceupgrader CAPI v1beta2; globalps manifest-pkg move | – | ✅ NEW guest writes: clusterversion, CRDs (RequestCount), namespaces, RBAC-capability-gated roles, new RBAC ValidatingAdmissionPolicy, usercabundle controller (NEW) | ✅ ingress-capability gating of guest namespaces/RBAC |
| **hostedcluster (HO)** | – | – | ✅ CPO image + labels resolution moved to `support/imageregistry` pkg; registry-override flag plumbing | – | – |
| **ignition-server** | – | – | ✅ same `util`→`imageregistry` package move (mechanical) | – | – |

---

## (b) New / changed API fields (`api/hypershift/v1beta1`)

### HCP status (HO/CPO-served CRD — NOT payload-governed)
1. **`HostedControlPlaneStatus.Initialization HostedControlPlaneInitializationStatus`** (`hosted_controlplane.go`) — new; satisfies CAPI v1beta2 ControlPlane provider contract.
2. **`HostedControlPlaneInitializationStatus.ControlPlaneInitialized *bool`** — written by the HCP controller (`hostedcontrolplane_controller.go:765`), a write-once latch.

### HostedCluster / HCP spec — `operatorConfiguration` tree (gated `HCPUserFacingOperatorLogs`)
New spec subtree (`hostedcluster_types.go` + `operator.go`), each component inlining `ComponentLogLevelSpec { LogLevel LogLevel }`:
3. `operatorConfiguration.kubeAPIServer` (`KubeAPIServerOperatorSpec`)
4. `operatorConfiguration.etcd` (`EtcdOperatorSpec`; CEL-restricted to Normal/Debug)
5. `operatorConfiguration.kubeControllerManager`
6. `operatorConfiguration.kubeScheduler`
7. `operatorConfiguration.openShiftControllerManager`
8. `operatorConfiguration.openShiftAPIServer`
9. `operatorConfiguration.openShiftOAuthAPIServer`
10. `operatorConfiguration.oauthServer`
   - New enum `LogLevel = Normal;Debug;Trace;TraceAll`. Legacy flat log-level field **deprecated** ("use spec.operatorConfiguration…; OperatorConfiguration takes precedence").

### Platform spec additions (reads by CPO/HO)
11. `AWSNodePoolPlatform.ResourceTags []AWSNodePoolResourceTag` (key/value) — `aws.go`
12. `AWSPlatformSpec.ResourceTags []AWSClusterResourceTag` + `OverridePolicy AWSResourceTagOverridePolicy` — `aws.go`
13. `AWSEndpointService ... ResourceTags []AWSEndpointServiceResourceTag` — `endpointservice_types.go`
14. AWS `CPUOptions` + `NestedVirtualizationPolicy` — `aws.go`
15. Azure `IPForwarding AzureIPForwarding` — `azure.go`
16. KubeVirt `CpuModelType Model` — `kubevirt.go`
17. Karpenter `Azure KarpenterAzureConfig` + `ClientID AzureClientID` (union AWS;Azure) — `operator.go`/`hostedcluster_types.go`

### Conditions / reason constants (status the CPO/HO may set)
18. HC condition `HostedClusterConfigurationDeprecated` + reasons (`DeprecatedConfigurationInUse`, `NotFound`, `WaitingForAvailable`, `SecretNotFound`, `WaitingForGracePeriod`, `Blocked`, `StatusUnknown`, `AsExpected`) — `hostedcluster_conditions.go`
19. NodePool conditions: `AWSResourceTagConflict` type + reasons (`TagConflictDetected`/`NoTagConflict`), `InvalidGCPMachineTemplate` — `nodepool_conditions.go`

### New labels/annotations referenced by controllers
20. `hypershift.openshift.io/nodepool-supports-kubevirt-architecture`
21. `hypershift.openshift.io/kubeletconfig-config`
22. `hypershift.openshift.io/mirrored-config` (NTOMirroredConfigLabel — used by new guest admission policy, see (c))

**Count of new/changed API fields (spec+status struct fields, excl. pure condition/label consts): ~17.**
**Including new condition types, reason consts, and labels: ~22 named additions.**

---

## (c) HCCO guest-write changes — skew assessment vs a **5.0 GUEST** API

> Reminder: HCCO writes INTO the guest cluster, whose CRDs/API come from the **5.0 payload**. A main HCCO writing a field/shape the 5.0 guest prunes or rejects is the HCCO analogue of operand-flag skew.

| # | Cat | File(s) | What changed | SKEW |
|---|---|---|---|---|
| G1 | C12 | `resources/configuration/cluster_version.go` (NEW) | HCCO now reconciles guest `ClusterVersion "version"`: sets `Spec.ClusterID`, `Spec.Capabilities{BaselineCapabilitySet:None, AdditionalEnabledCapabilities}` (filtered by guest's `Status.Capabilities.KnownCapabilities`), Upstream/Channel. | **FINE** — `config.openshift.io/v1 ClusterVersion` is a stable GA core API; capabilities filtered against the *guest's own* KnownCapabilities, so unknown caps are dropped before write. Low risk. |
| G2 | C12 | `resources/configuration/crds.go` (NEW) | HCCO now applies a **RequestCount CRD** into the guest. | **COORDINATION-RISK** — installing/owning a CRD in a 5.0 guest is new behavior; if the 5.0 payload also ships/owns this CRD a different version, server-side-apply/ownership conflict or schema drift is possible. UNCONFIRMED whether 5.0 payload ships RequestCountCRD. |
| G3 | C12 | `resources/configuration/namespaces.go` (NEW) | HCCO reconciles a fixed set of `openshift-*` guest namespaces, gating `openshift-ingress` on the ingress capability. | **FINE** — plain `v1/Namespace`; label reconcilers add standard pod-security/monitoring labels, all GA. |
| G4 | C12 | `resources/rbac/reconcile.go` (major rewrite) | RBAC roles/bindings now capability-gated via `RbacCapabilityMap` (ingress-to-route-controller roles only when ingress cap enabled); param-driven `ReconcileParams{IngressEnabled,IsAROHCP}`. | **FINE/COORDINATION-RISK** — `rbac.authorization.k8s.io/v1` is GA. Net effect vs 5.0: fewer roles when ingress cap disabled. If a 5.0 guest expects those route-controller roles unconditionally, disabling could regress routes (COORDINATION-RISK only when ingress capability is off). |
| G5 | C12 | `resources/kas/admissionpolicies.go` | NEW guest `ValidatingAdmissionPolicy` for RBAC (`clusterrolebindings` UPDATE/DELETE, names `hcco-cluster-admin`,`kas-bootstrap-container-cluster-admin`); new NTO mirrored-config policy keyed on `hypershift.openshift.io/mirrored-config` label; CEL whitelist incl. `system:admin`, CNO SA. | **BREAKS (potential)** — `admissionregistration.k8s.io/v1 ValidatingAdmissionPolicy` only GA at k8s 1.30. A **5.0 guest** on an older k8s may serve VAP as v1beta1 or not at all → HCCO apply 404/pruned → policy silently absent or reconcile error. **Highest-risk guest-shape item.** UNCONFIRMED exact 5.0 guest k8s minor. |
| G6 | C6/C12 | `usercabundle/usercabundle.go` + `setup.go` (NEW controller) | NEW HCCO controller syncing user CA bundle; writes guest configmap, honors `operateOnReleaseImage` gating and pause. | **FINE** — writes a `v1/ConfigMap`; shape is stable. New controller presence is additive. |
| G7 | C12 | `resources/configuration/cluster_operators.go` (renamed from `resources/clusteroperators.go`) | `ClusterOperator` status reconcile refactored (free funcs, `versions` param). Guest-written shape essentially unchanged. | **FINE** — refactor only; `config.openshift.io/v1 ClusterOperator` GA. |
| G8 | C6 | `globalps/globalps.go` | Global-pull-secret manifests moved to `pkg/manifests/hcco`; still writes guest kube-system secrets + DaemonSet. Logic parity. | **FINE** — package move; guest objects (Secret/DaemonSet) unchanged. |

---

## (d) CPO-image-resolution / label / env changes (HO + ignition)

| # | Cat | File(s) | What changed | SKEW |
|---|---|---|---|---|
| D1 | C7 | `hostedcluster_controller.go` | `GetControlPlaneOperatorImage` + `GetControlPlaneOperatorImageLabels` **moved** `support/util` → new `support/imageregistry` pkg. Call signatures identical (hcluster, releaseProvider, HO image, pullSecret / metadataProvider). | **FINE** — pure package relocation; resolution semantics unchanged. CPO image still resolved from release payload w/ HO fallback, labels still read from the CPO image. |
| D2 | C7 | `hostedcluster_controller.go` | New registry-override plumbing passed to CPO deployment params: `RegistryOverrideCommandLine = imageregistry.ConvertRegistryOverridesToCommandLineFlag(...)`, `OpenShiftRegistryOverrides = ...ConvertOpenShiftImageRegistryOverridesToCommandLineFlag(...)`. | **COORDINATION-RISK (low)** — CPO deployment now receives override flags via these converters; if a 5.0 CPO binary didn't parse the new flag format there'd be skew, but here the **main** CPO is deployed, so self-consistent. FINE for main-CPO case. |
| D3 | C7 | `hostedcluster_controller.go` | Release-image helpers centralized: `imageregistry.HCControlPlaneReleaseImage/HCPControlPlaneReleaseImage` drive rollout-complete + upgrade gating logic. | **FINE** — refactor of existing release-image selection; same inputs (HC/HCP release image vs status). |
| D4 | C7 | `internal/platform/platform.go` + per-platform | CAPI provider image resolution adds `backwardcompat.GetBackwardCompatibleCAPIImage(releaseVersion)` fallback when override empty. | **FINE/COORDINATION-RISK** — adds backward-compat CAPI image mapping; affects CAPI provider image, not CPO itself. Benign for CPO skew. |
| D5 | C7 | `ignition-server/cmd/start.go`, `run_local_ignitionprovider.go`, `controllers/local_ignitionprovider.go` | `util.RegistryClientImageMetadataProvider` + `util.ConvertImageRegistryOverrideStringToMap` → `imageregistry.*`. Mechanical. | **FINE** — package move only; ignition image-metadata/override behavior unchanged. |

**No new image LABEL keys** were observed being *read* to gate CPO behavior; the label-read path (`GetControlPlaneOperatorImageLabels`) is unchanged except for its package. (UNCONFIRMED: deep semantics inside `support/imageregistry` not diffed — it is outside assigned regions; relocation is the visible change at the seam.)

---

## (e) Ranked top skew / coordination hazards

1. **G5 — HCCO writes `ValidatingAdmissionPolicy` into the guest (admissionregistration.k8s.io/v1).** If the 5.0 guest payload runs an older k8s where VAP v1 isn't served/GA, the main HCCO's apply is pruned/rejected → RBAC-lockdown + NTO mirrored-config policies silently missing or reconcile errors. **Highest guest-shape skew risk.** [BREAKS/SILENT-NOOP — verify 5.0 guest k8s minor ≥1.30.]
2. **G2 — HCCO now installs/owns the RequestCount CRD in the guest.** New CRD ownership against a 5.0 guest risks schema/ownership conflict if the 5.0 payload ships its own copy. [COORDINATION-RISK — confirm 5.0 payload CRD provenance.]
3. **G4 — Capability-gated guest RBAC (ingress-to-route-controller roles now conditional).** A 5.0 guest expecting those roles unconditionally could regress route handling when the ingress capability is disabled by the main HCCO. [COORDINATION-RISK.]
4. **API C10 — `operatorConfiguration` tree gated on `HCPUserFacingOperatorLogs` + deprecation of flat log-level fields.** HCP/HC CRDs are HO/CPO-served (not payload), so field availability tracks the main CPO — FINE for the CPO itself, but any 5.0-era tooling/consumer reading the old flat field sees it deprecated/overridden. [COORDINATION-RISK, not a payload-skew break.]
5. **D1/D2 — CPO image + override-flag resolution relocated to `support/imageregistry` with new registry-override command-line plumbing.** Self-consistent when deploying the main CPO; becomes a skew surface only if a mixed HO/CPO version pairing is attempted (older CPO not parsing new override flag format). [COORDINATION-RISK, low for homogeneous-main.]

### Lower-tier / FINE
- G1 clusterversion write (caps filtered by guest KnownCapabilities), G3 namespaces, G6 usercabundle, G7 clusteroperators, G8 globalps — all GA guest APIs, additive/refactor.
- HCP `Initialization.ControlPlaneInitialized` status — HCP CRD is HO/CPO-served, so field availability is governed by the HO/CPO version, **not** the 5.0 payload → FINE for CPO-from-main.
- Reencryption status-patch rewrite (C6): now uses `support/statuspatching.PatchStatus` + `SyncCondition` against HCP (HO-served CRD); behavior-preserving, writes only `secretEncryption`/`EtcdDataEncryptionUpToDate`. FINE.
- inplaceupgrader/node/machine: CAPI `v1beta1`→`v1beta2` import bump (`NodeRef.IsDefined()`), capacity-clamp bugfix — operates on guest CAPI objects in the **management** cluster namespace, not guest payload API. FINE.

### UNCONFIRMED items to verify
- Exact k8s minor of the 5.0 guest payload (decides G5 BREAKS vs FINE).
- Whether the 5.0 payload already ships/owns RequestCountCRD (G2).
- Internal semantics of `support/imageregistry` (outside assigned regions; only the call-site relocation was diffed).
