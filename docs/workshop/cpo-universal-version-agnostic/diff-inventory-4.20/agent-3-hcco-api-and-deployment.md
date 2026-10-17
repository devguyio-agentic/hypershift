# Agent-3 (4.20 DELTA) — HCCO / HyperShift-API / CPO-Deployment / Ignition diff inventory

**Diff basis:** `git diff upstream/release-4.20..upstream/main` (two-dot).
**Resolved refs:** `upstream/main = 220e0e73a4…` (matches task tip), `upstream/release-4.20 = 48fd596611…`.
**Baseline compared against:** `../diff-inventory/agent-3-hcco-api-and-deployment.md` (the 5.0→main inventory).
**Method:** pure diff analysis, read-only, no compile.
**Guest-skew lens:** a **4.20 guest runs k8s ~1.31** (UNCONFIRMED exact minor; 4.20 payload ≈ k8s 1.31/1.32) vs main management ≈ k8s 1.34/1.35. The 4.20 span is ~5 minors, so guest API-version/shape skew is **much more likely to bite** than the 5.0 (~1 minor) baseline.

Scope regions: HCCO (`control-plane-operator/hostedclusterconfigoperator/`), API (`api/hypershift/`), HO→CPO deployment (`hypershift-operator/controllers/hostedcluster/`), ignition (`ignition-server/`).

Diff magnitude vs 5.0 baseline: HCCO ~15k insertions (5.0: far smaller), API ~219k yaml/6.2k go-type insertions, hostedcluster ~22k, ignition ~2.7k. This is a **superset** of the 5.0 findings plus large 4.20-era additions (GCP platform, Azure PrivateLink, etcd backup/sharding, metrics forwarder, spot remediation, webhook validation, reencryption controller).

---

## (a) Summary table — region × category

| Region | C4 API-field | C6 Semantic-protocol | C7 CPO-image-res | C12 Guest-resource-shape | C10 Featuregate/cap |
|---|---|---|---|---|---|
| **api/hypershift** | ✅ VERY heavy (~193 new json field names; GCP platform tree, Azure PrivateLink/PSC, etcd backup+sharding, operatorConfiguration tree, HCP Initialization, resourceTags, karpenter Azure) | – | – | – | ✅ many gates: `GCPPlatform`, `HCPEtcdBackup`, `EtcdSharding`, `HCPUserFacingOperatorLogs`, `OpenStack`, `ExternalOIDC*`, `TLSAdherence`, `NetworkObservabilityInstall`, `ImageStreamImportMode`, DRA rename |
| **HCCO** | reads HC caps/channel; writes HCP status via reencryption/statuspatch | ✅ reencryption controller (NEW dir); globalps pkg move; servicemonitor shape rewrite | – | ✅ **BIG**: clusterversion, RequestCount CRD, namespaces, capability-gated RBAC, KAS RBAC VAP, **ServiceMonitor/PodMonitor at prom-operator v0.92 shape**, **MetricsForwarder PodMonitor/Deployment (NEW)**, usercabundle (NEW), webhookvalidation (NEW), spotremediation (NEW, mgmt-side), OVN IPv6 subnet fields, ingress operatorv1 shapes | ✅ ingress/RBAC/namespace capability gating |
| **hostedcluster (HO)** | – | – | ✅ CPO image + labels + release-image helpers moved `support/util`→`support/imageregistry`; registry-override flag plumbing; backward-compat CAPI image | – | ✅ GCP platform plumbing (new), karpenter Azure |
| **ignition-server** | – | – | ✅ same `util`→`imageregistry` pkg move (mechanical) | – | – |

---

## (b) New / changed API fields (`api/hypershift/v1beta1`)

**Count of added `json:` field names in diff: ~193 unique** (vs ~17 struct fields in the 5.0 baseline — an order of magnitude more, because the 4.20 span introduces whole new platform/feature trees). Grouped:

### Whole new type files (new subsystems)
- `gcp.go` (+572), `gcpprivateserviceconnect_types.go` (+171) — **GCP platform** (gated `GCPPlatform`): `GCPPlatformSpec`, bootDisk, osImageStream, serviceAccount, workloadIdentity, privateServiceConnect, forwardingRuleName, serviceAttachment*, etc.
- `azureprivatelinkservice_types.go` (+310) — Azure PrivateLink Service / PSC: privateLinkServiceID/Alias, consumerAcceptList, privateEndpoint*, internalLoadBalancerID.
- `etcdbackup_types.go` (+364) — **etcd backup** (gated `HCPEtcdBackup`) + **etcd sharding** (gated `EtcdSharding`): `shards`, s3/azureBlob/swift/pv targets, snapshotURL, lastSuccessfulEtcdBackupURL, completionTime.

### operatorConfiguration tree (carried from 5.0, gated `HCPUserFacingOperatorLogs`)
- `operatorConfiguration.{kubeAPIServer,etcd,kubeControllerManager,kubeScheduler,openShiftControllerManager,openShiftAPIServer,openShiftOAuthAPIServer,oauthServer}` each inlining `logLevel`; plus `operatorConfiguration.clusterNetworkOperator.ovnKubernetesConfig` (new CEL guard). Legacy flat log-level field **deprecated** ("OperatorConfiguration takes precedence").

### HCP status
- `HostedControlPlaneStatus.Initialization` + `ControlPlaneInitialized *bool` (CAPI v1beta2 ControlPlane-provider contract; same as 5.0 baseline — already landed within the 4.20 span).
- `nodesInfo`, `readyNodeCount`, `unreadyNodeCount`, `nodeVersions`, `kubeletVersion`, `nodeClaimCount` (karpenter/nodepool status surfacing).

### Platform spec additions (reads by CPO/HO)
- AWS: `ResourceTags`, `OverridePolicy`, `CPUOptions`, `NestedVirtualizationPolicy`, `marketType`, `spot`/`maxPrice`.
- Azure: `IPForwarding`, `marketType`, managed-identity/keyVault expansions, `diskType`.
- KubeVirt: `CpuModelType`/`model`, `qosClass`, `networkConfig`.
- Karpenter: `Azure KarpenterAzureConfig` + `ClientID` (union AWS;Azure).
- PowerVS/IBMCloud/OpenStack field expansions.

### Conditions / reason constants
- HC: `HostedClusterConfigurationDeprecated` family; many new condition/reason consts (`hostedcluster_conditions.go` +154).
- NodePool: `AWSResourceTagConflict`, `InvalidGCPMachineTemplate` + reasons (`nodepool_conditions.go` +20).

> **Grade for api/hypershift fields: FINE for CPO-from-main.** HCP/HC/NodePool CRDs are **HO/CPO-served**, not drawn from the 4.20 guest payload → their availability tracks the main binaries, not the guest. The only residual is **C10 coordination**: 4.20-era tooling/consumers reading the now-deprecated flat log-level field, and feature-gated fields invisible unless the gate is on. No payload-skew break here.

---

## (c) HCCO guest-write changes — skew assessment vs a **4.20 GUEST** (k8s ~1.31)

> HCCO writes INTO the guest, whose CRDs/API come from the **4.20 payload**. A main HCCO writing a field/shape the 4.20 guest prunes or rejects is the HCCO analogue of operand-flag skew. Items below are graded against k8s ~1.31 + 4.20 OpenShift CRDs.

| # | Cat | File(s) | What changed | SKEW (vs 4.20 guest) |
|---|---|---|---|---|
| G1 | C12 | `monitoring/servicemonitor.go`, `monitoring/metricsforwarder.go` (NEW), `resources.go` | **prometheus-operator apis bumped v0.74.0 → v0.92.1.** `ReconcileKubeAPIServerServiceMonitor` rewritten to new nested `HTTPConfigWithProxyAndTLSFiles{HTTPConfigWithTLSFiles{TLSConfig,TLSFilesConfig}}` shape + `Scheme`/`Port` as typed pointers. NEW MetricsForwarder writes guest `PodMonitor` (prom-operator) + Deployment/ConfigMaps into `openshift-monitoring`. All written to **guest** (`r.client`). | **BREAKS / COORDINATION-RISK (NEW, top hazard).** The guest's ServiceMonitor/PodMonitor CRDs ship from the **4.20 payload** (prom-operator ~0.74-era schema). A main HCCO serializing the v0.92 struct — new/renamed/relocated fields (`scheme`, nested `httpConfig`, `tlsFilesConfig`) — will have those fields **pruned by the older guest CRD** (structural-schema prune) → silently dropped TLS/scheme config, or SSA field-ownership churn. If a field moved to a required/renamed position, apply may error. **This is the single most 4.20-specific guest-shape hazard and has NO analogue in the 5.0 baseline.** |
| G2 | C12 | `kas/admissionpolicies.go` | NEW `reconcileRBACValidatingAdmissionPolicy` (guards `clusterrolebindings` UPDATE/DELETE; whitelists `system:admin`, KAS-bootstrap SA, CNO SA); existing Config/Mirror/Infra/NTO-mirrored VAPs retained. All `admissionregistration.k8s.io/v1 ValidatingAdmissionPolicy`. | **COORDINATION-RISK / possibly FINE.** VAP v1 is GA at k8s 1.30; a **4.20 guest at ~1.31 should serve VAP v1**, so unlike the 5.0-baseline worry this is *likely fine* at 4.20 — **UNCONFIRMED** on exact minor. Residual risk: if any guest is at the low edge (<1.30) VAP v1 apply is pruned → RBAC lockdown + NTO policies silently absent. Net new vs 5.0: the extra RBAC VAP + `system:admin`/KAS-bootstrap whitelist. |
| G3 | C12 | `resources/registry/admissionpolicies.go` | Image-registry managementState VAP: error-wrap `%v`→`%w`, log removal, signature tidy. apiVersion unchanged (`imageregistry.operator.openshift.io/v1` + `admissionregistration/v1`). | **FINE** — no shape/version change; same VAP-v1 caveat as G2 but no new surface. |
| G4 | C12 | `configuration/cluster_version.go` (NEW) | HCCO reconciles guest `ClusterVersion "version"`: `Spec.ClusterID`, `Capabilities{BaselineCapabilitySet:None, AdditionalEnabledCapabilities}` filtered by guest `Status.Capabilities.KnownCapabilities`, Upstream/Channel, clears DesiredUpdate. | **FINE** — `config.openshift.io/v1 ClusterVersion` GA since forever; caps filtered against the **guest's own** KnownCapabilities before write, so unknown-to-4.20 caps are dropped. |
| G5 | C12 | `configuration/crds.go` (NEW) | HCCO applies **RequestCount CRD** into the guest. | **COORDINATION-RISK** — installing/owning a CRD in a 4.20 guest; if the 4.20 payload ships its own copy at a different version, SSA/ownership/schema conflict. UNCONFIRMED whether 4.20 payload ships RequestCountCRD. (Same as 5.0 baseline G2.) |
| G6 | C12 | `configuration/namespaces.go` (NEW) | Reconciles fixed `openshift-*` namespaces; gates `openshift-ingress` on ingress capability. | **FINE** — plain `v1/Namespace` + standard pod-security/monitoring labels, all GA at 1.31. |
| G7 | C12 | `resources/rbac/reconcile.go` (+203) | RBAC roles/bindings capability-gated (`RbacCapabilityMap`; ingress-to-route-controller roles only when ingress cap on); param-driven. | **FINE / COORDINATION-RISK** — `rbac.authorization.k8s.io/v1` GA. Risk only when ingress cap disabled against a 4.20 guest that expects those roles unconditionally → route regression. |
| G8 | C12 | `network/reconcile.go` (+80) | Writes guest `operatorv1.Network`: new **OVN IPv6 config** (`IPv6OVNKubernetesConfig.InternalJoinSubnet/InternalTransitSwitchSubnet`), `applyOVNConfig` user-OVN plumbing, dual-stack defaults. | **COORDINATION-RISK (NEW vs 5.0).** `operator.openshift.io/v1 Network` is guest-served from the 4.20 payload. IPv6 OVN subnet fields are relatively recent in openshift/api; a 4.20 CNO CRD may **prune `internalJoinSubnet`/`internalTransitSwitchSubnet`/ipv6 block** → dual-stack config silently dropped. UNCONFIRMED whether 4.20 operatorv1.Network schema carries these fields. |
| G9 | C12 | `ingress/reconcile.go` (+212), `ingress/params.go` | Guest `operatorv1.IngressController` reconcile heavily reworked (endpointPublishingStrategy passthrough, NLB/NodePort/HostNetwork shapes, component-route labels gate `IngressComponentRouteLabels`). | **FINE / COORDINATION-RISK** — `operator.openshift.io/v1 IngressController` GA; standard endpointPublishingStrategy fields exist at 4.20. Risk only if a newly-referenced sub-field is 4.20-absent (not observed). Grade FINE pending confirmation. |
| G10 | C6 | `reencryption/` (NEW dir: reencryption.go +598, setup.go, metrics.go) | NEW controller: secret re-encryption state machine; writes **HCP status** (`SecretEncryption`, `EtcdDataEncryptionUpToDate`) via `guestClient`+`cpClient`; reads guest apiserver encryption. | **FINE** — HCP status is HO/CPO-served (not 4.20 payload). Guest reads are of GA resources. Behavior is additive. |
| G11 | C12 | `usercabundle/` (NEW dir +121) | NEW controller: writes guest `v1/ConfigMap` user CA bundle from CPO-aggregated trust bundle; pause/delete handling. | **FINE** — plain ConfigMap, stable shape. (Same as 5.0 baseline G6.) |
| G12 | C6/C12 | `webhookvalidation/` (NEW dir +168) | NEW controller: lists guest `Validating/MutatingWebhookConfiguration` (admissionregistration/v1) and **deletes** any whose `clientConfig.url` is not allow-listed (anti-exfiltration guard). | **FINE** — `admissionregistration.k8s.io/v1 {Validating,Mutating}WebhookConfiguration` GA since 1.16; reads/deletes only, no new-shape write. Deletion of guest webhooks is a **behavioral** change (C6) but version-safe at 1.31. |
| G13 | C6 | `spotremediation/` (NEW dir +126) | NEW controller: watches guest `Node` taints, looks up CAPI `Machine` and updates/deletes it. CAPI objects live in the **management** namespace, not guest payload. | **FINE** — guest read is `v1/Node` (GA); writes target mgmt-side CAPI. No guest-shape skew. |
| G14 | C6 | `globalps/globalps.go` (+125) | Global-pull-secret manifests moved to `pkg/manifests/hcco`; still writes kube-system secret + DaemonSet. | **FINE** — package move; guest objects unchanged. (Same as 5.0 baseline G8.) |
| G15 | C12 | `configuration/cluster_operators.go` (renamed) | `ClusterOperator` status reconcile refactor. | **FINE** — `config.openshift.io/v1 ClusterOperator` GA; refactor only. |

---

## (d) CPO-image-resolution / label / env changes (HO + ignition)

| # | Cat | File(s) | What changed | SKEW |
|---|---|---|---|---|
| D1 | C7 | `hostedcluster_controller.go` | `GetControlPlaneOperatorImage` + `GetControlPlaneOperatorImageLabels` **moved** `support/util/util.go` → `support/imageregistry/imageregistry.go` (confirmed by `git grep`: fn lives in `support/util` at 4.20, `support/imageregistry` at main). Call signatures identical. | **FINE** — pure package relocation; CPO image still resolved from release payload w/ HO fallback, labels still read from CPO image. Same conclusion as 5.0 baseline D1 (the 5.0 report already saw the completed move). |
| D2 | C7 | `hostedcluster_controller.go` | `reconcileControlPlaneOperator` param list changed: **`utilitiesImage` loses `defaultIngressDomain` param** (dropped), registry-override converters now `imageregistry.ConvertRegistryOverridesToCommandLineFlag` / `ConvertOpenShiftImageRegistryOverridesToCommandLineFlag`. | **COORDINATION-RISK (low).** Self-consistent for main-CPO deploy. Skew only under mixed HO/CPO pairing (older CPO not parsing new override-flag format) — not the homogeneous-main case. |
| D3 | C7 | `hostedcluster_controller.go` | Release-image helpers centralized: `imageregistry.HCControlPlaneReleaseImage/HCPControlPlaneReleaseImage` drive rollout-complete + upgrade gating. | **FINE** — refactor of existing selection logic; same inputs. |
| D4 | C7/C10 | `internal/platform/platform.go` (+80), `internal/platform/gcp/gcp.go` (NEW +620) | **GCP platform** CPO/CAPI provider plumbing added (gated `GCPPlatform`); backward-compat CAPI image fallback retained. | **FINE / COORDINATION-RISK** — new platform path, gate-guarded; affects CAPI provider image, not CPO binary. Benign for CPO skew. |
| D5 | C7 | `ignition-server/cmd/start.go`, `run_local_ignitionprovider.go`, `controllers/local_ignitionprovider.go`, `cache.go` | `util.RegistryClientImageMetadataProvider` + `util.ConvertImageRegistryOverrideStringToMap` → `imageregistry.*`. Mechanical. | **FINE** — package move only; ignition image-metadata/override behavior unchanged. (Same as 5.0 baseline D5.) |

**No new CPO image LABEL keys** observed being read to gate CPO behavior; label-read path unchanged except package. (UNCONFIRMED: deep semantics inside `support/imageregistry` not diffed — outside assigned regions; relocation is the visible seam.)

---

## (e) DELTA vs the 5.0 baseline

**Carried over unchanged from 5.0 (already landed within the 4.20 span, same grade):**
- CPO-image/label/release-image move to `support/imageregistry` + registry-override plumbing (D1–D3, D5) — still FINE.
- clusterversion write (G4), RequestCount CRD (G5), namespaces (G6), capability-gated RBAC (G7), usercabundle (G11), globalps move (G14), cluster_operators refactor (G15), HCP `Initialization` status, operatorConfiguration tree + log-level deprecation.
- KAS RBAC VAP (G2) — present in both; but the skew **grade improves at 4.20**: the 5.0 baseline flagged VAP-v1 as the top BREAKS risk (older guest might lack VAP v1); at a **4.20 guest (~1.31) VAP v1 is GA**, so this drops from "highest risk" to COORDINATION-RISK/likely-FINE.

**NEW at 4.20 (not in the 5.0 inventory) — these are the DELTA:**
1. **G1 — prometheus-operator apis v0.74→v0.92 + new MetricsForwarder guest writes.** Entirely new; ServiceMonitor/PodMonitor struct shape changed against an older guest CRD. **This replaces VAP as the top guest-shape hazard at 4.20.**
2. **G8 — OVN IPv6 subnet fields on guest `operatorv1.Network`.** New dual-stack/IPv6 write surface vs older CNO CRD.
3. **G9 — large IngressController reconcile rework** (endpointPublishingStrategy passthrough, component-route-labels gate).
4. **G10 — reencryption controller (new dir).** HCP-status only → FINE, but net-new subsystem.
5. **G12 — webhookvalidation controller (new dir).** New behavioral guest-webhook deletion.
6. **G13 — spotremediation controller (new dir).** Mgmt-side CAPI, FINE.
7. **API: ~193 new field names** (vs ~17 at 5.0) — GCP platform, Azure PrivateLink/PSC, etcd backup+sharding trees, plus many new feature gates (`GCPPlatform`, `HCPEtcdBackup`, `EtcdSharding`, `OpenStack`, `ExternalOIDC*`, `TLSAdherence`, `ImageStreamImportMode`, `NetworkObservabilityInstall`). All HO/CPO-served → FINE for payload-skew, C10 coordination only.

**Grade shifts 5.0 → 4.20:**
- KAS VAP: **down** (BREAKS→COORDINATION-RISK/likely-FINE) because 1.31 serves VAP v1.
- Overall guest-shape risk **up**, but the dominant risk moved from admissionregistration (G2) to **prometheus-operator CRD shape (G1)** and **operatorv1.Network IPv6 (G8)** — the wider 5-minor span surfaces *CRD-schema prune* risks that the ~1-minor 5.0 span did not.

---

## (f) Ranked top hazards (vs a 4.20 guest)

1. **G1 — ServiceMonitor/PodMonitor written at prometheus-operator v0.92 shape into a 4.20 guest whose monitoring CRDs are ~v0.74-era.** New nested `httpConfig`/`tlsFilesConfig`/typed `scheme` fields risk being **pruned by the older guest CRD** → dropped TLS/scrape config or SSA churn; new MetricsForwarder PodMonitor same exposure. **[BREAKS/COORDINATION-RISK — NEW vs 5.0. #1 hazard.]** Verify 4.20 payload's prometheus-operator CRD version.
2. **G8 — OVN IPv6 subnet fields (`internalJoinSubnet`/`internalTransitSwitchSubnet`/ipv6 block) on guest `operatorv1.Network`.** Older 4.20 CNO CRD may prune → dual-stack config silently lost. **[COORDINATION-RISK — NEW vs 5.0.]**
3. **G5 — HCCO installs/owns RequestCount CRD in the guest.** Ownership/schema conflict if the 4.20 payload ships its own. **[COORDINATION-RISK — carried from 5.0; confirm 4.20 provenance.]**
4. **G2 — KAS RBAC ValidatingAdmissionPolicy (admissionregistration/v1).** Likely FINE at 4.20 (~1.31 ≥ 1.30 GA), but still the apiVersion-gated write to watch if any guest sits <1.30. **[COORDINATION-RISK — grade improved from 5.0's BREAKS.]**
5. **G7/G9 — capability-gated RBAC + IngressController rework.** Disabling ingress cap drops route-controller roles; reworked endpointPublishingStrategy passthrough could reference a 4.20-absent operatorv1 sub-field. **[COORDINATION-RISK.]**

### Lower-tier / FINE
- G3 registry VAP (no shape change), G4 clusterversion (caps filtered by guest KnownCaps), G6 namespaces, G10 reencryption (HCP status), G11 usercabundle, G12 webhookvalidation (GA webhook APIs), G13 spotremediation (mgmt CAPI), G14 globalps, G15 cluster_operators.
- All api/hypershift field additions — HCP/HC/NodePool CRDs are HO/CPO-served, not 4.20-payload-governed → FINE for CPO-from-main; residual is C10 (deprecated flat log-level, gate-hidden fields).
- CPO image resolution D1–D5 — package relocation + override plumbing; self-consistent for homogeneous main.

### UNCONFIRMED items to verify
- Exact k8s minor of the 4.20 guest payload (decides G2 FINE vs BREAKS; assumed ~1.31).
- prometheus-operator CRD version shipped in the 4.20 payload (decides G1 severity — the #1 hazard).
- Whether 4.20 `operator.openshift.io/v1 Network` CRD carries the OVN IPv6 subnet fields (G8).
- Whether the 4.20 payload ships/owns RequestCountCRD (G5).
- Internal semantics of `support/imageregistry` (outside assigned regions; only the call-site relocation diffed).
