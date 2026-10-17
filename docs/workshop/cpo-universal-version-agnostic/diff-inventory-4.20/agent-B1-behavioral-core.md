# Agent B1 — Behavioral diff (control-flow / algorithm / protocol): CPO imperative core + v2 framework, **4.20 → main**

**Scope:** `control-plane-operator/controllers/hostedcontrolplane/*.go`, `.../{pki,cloud,infra}/` (NOT v2/), and `support/controlplane-component/` (v2 framework).
**Method:** two-dot Go-logic diff `git diff upstream/release-4.20..upstream/main -- <path>`, plus `git show <ref>:<file>` to pin whether a change landed in the 4.20→5.0 window or the 5.0→main window. zz_fixture YAML deliberately NOT used as a finding source.
**Branch tips:** `upstream/release-4.20` = `48fd596611`, `upstream/release-5.0` = `b9d6ae1c`, `upstream/main` = `220e0e73`.
**Baseline compared against:** `/home/sandbox-test/workspace/cpo-from-main/diff-inventory/agent-B1-behavioral-core.md` (the 5.0→main run).
**Skew axis:** version-agnostic CPO from main (~5.1) driving a **4.20 guest payload** (~5 minors back). FLOOR = 4.20.

---

## HEADLINE (answers the KEY QUESTION)

**The v2 framework gating logic DID change between 4.20 and main — unlike 5.0, where it was byte-identical.**

The exact files the 5.0 run proved unchanged — `status.go`, `builder.go`, `deployment.go`, `statefulset.go`, `generic-adapter.go` — **all differ 4.20→main** while remaining **byte-identical 5.0→main**. Proof:

| framework file | 5.0..main | 4.20..main |
|---|---|---|
| `status.go` | **0 diff** | **CHANGED** (etcd dependency exclusion; ObservedGeneration; AssetDirName) |
| `builder.go` | **0 diff** | **CHANGED** (`WithAssetDir`, `WithTemplateData`) |
| `deployment.go` | **0 diff** | **CHANGED** (`LoadManifestTemplated`) |
| `statefulset.go` | **0 diff** | **CHANGED** (`LoadManifestTemplated`) |
| `generic-adapter.go` | **0 diff** | **CHANGED** (`SkipManifest`, `NewGenericControllerConfigAdapter`) |
| `common.go` | CHANGED | CHANGED (PDB predicate-gating) |
| `defaults.go` | CHANGED | CHANGED (short tolerations + **GCP restricted SC**, latest-tag pull policy) |

So the structural "quiet-area" the 5.0 run relied on (framework convergence rules identical) **no longer holds at a 4.20 floor**. The dependency-gating and status-derivation engine moved in the 4.20→5.0 window. Details in section (c).

Second headline: **AWS SDK v1 → v2 migration.** At 4.20 the CPO uses `aws-sdk-go` (`ec2iface.EC2API`, `*session.Session`, `*ec2.Filter`, `kms.New(session,...)`). At 5.0/main it uses `aws-sdk-go-v2` (`awsapi.EC2API`, `*aws.Config`, `ec2types.Filter`, context-first calls). Every AWS imperative path (SG create/tag-diff/describe/delete, KMS probe, EC2 client construction) changed call shape and error typing. Invisible to a manifest diff; present across the whole cloud surface.

---

## (a) Reconcile-loop delta, chronological (4.20 → main)

The top-level loop was **re-architected** in the 4.20→5.0 window; main inherits that shape. Against a 4.20 floor the ordering/structure changes are much larger than the 5.0→main run saw.

1. **Deletion extracted + reordered.** 4.20 handled deletion *inline* at the top of `Reconcile` (`if !DeletionTimestamp.IsZero()`). Main calls a dedicated `reconcileDeletion(ctx, hcp)`. Inside it:
   - SG cleanup moved onto concurrency-safe `statuspatching.PatchStatusCondition` (was capture-then-`Status().Patch`).
   - **NEW teardown step**: for AWS, `deleteAWSEndpointServices` lists+deletes `AWSEndpointService` objects and **returns `RequeueAfter: awsEndpointServiceDeletionRequeueInterval` (10s) until the CPO finalizer is gone / per-object timeout** — HCP deletion now BLOCKS on VPC-endpoint cleanup. Did NOT exist at 4.20.
2. **Status derivation split into discrete methods.** 4.20 computed etcd/KAS/degraded status inline in `Reconcile`. Main calls, in order: `reconcileEtcdStatus` → `reconcileKASStatus` → `reconcileDeprecatedConfigurationStatus` → `reconcileDegradedStatus` → `reconcileInfrastructureStatusCondition` → `reconcileExternalDNSStatusCondition` → `reconcileAvailabilityAndReadyStatus` → `reconcileKubeadminPasswordStatus` → `reconcileControlPlaneVersionStatus`. Several of these are **new named functions that did not exist at 4.20** (`reconcileEtcdStatus`, `reconcileKASStatus`, `reconcileDeprecatedConfigurationStatus`, `reconcileControlPlaneVersionStatus`). The split landed 4.20→5.0.
3. **NEW normal-path step**: `reconcileDeprecatedConfigurationStatus` → `HostedClusterConfigurationDeprecated` condition (message-only; triggers on `kube-apiserver-verbosity-level` annotation). Absent at 4.20.
4. **NEW write**: `Status.Initialization.ControlPlaneInitialized = ptr.To(true)` (CAPI v1beta2 contract). Absent at 4.20.
5. **PKI orchestration split.** 4.20 had a monolithic `reconcilePKI`. Main decomposes it into ~12 sub-reconcilers called in sequence: `reconcileCombinedPullSecret` → `reconcileEtcdCerts` → `reconcileKASCerts` → `reconcileOpenshiftCerts` → `reconcileKonnectivityCerts` → (`reconcileManagedTrustedCABundle`) → `reconcileOAuthCerts` → `reconcileOLMAndMiscCerts` → `reconcileNetworkServingCerts` → `reconcilePlatformSpecificCerts` (→ AWS/Azure/GCP). Same cert set, but reordered/regrouped and each is a separate failure/requeue boundary now.
6. **Version aggregation** (`ensureControlPlaneVersionPartial` / `reconcileControlPlaneVersion`) moved from inline-in-`Reconcile` (4.20) into the new `reconcileControlPlaneVersionStatus` method (main). Same algorithm (list `ControlPlaneComponentList`, aggregate, partial fallback on list error).
7. **Package churn** (behavior-preserving): `util.*` → `reconcilerpolicy.*` / `imageregistry.*` / `k8sutil.*` / `podspec.*` / `netutil.*` / `cpomanifests.*`; `sharedingress.*` → `netutil.*`.

**Verdict: Reconcile loop ORDERING CHANGED, and more deeply than at 5.0** — deletion extracted + new blocking endpoint-service step; status derivation and PKI both decomposed into many new ordered sub-steps. This is a structural re-architecture relative to the 4.20 floor.

---

## (b) Full behavioral findings (file:func | what-changed | CLASS | why, vs a 4.20 FLOOR)

### AWS SDK migration (NEW category not in the 5.0 baseline)
1. `hostedcontrolplane_controller.go:GetEC2Client` + all SG funcs + `validateAWSKMSConfig` | **SDK v1→v2.** `ec2iface.EC2API`→`awsapi.EC2API`; `*session.Session`→`*aws.Config`; `*ec2.Filter`→`ec2types.Filter`; `*ec2.SecurityGroup`→`*ec2types.SecurityGroup`; all EC2/KMS calls now take `ctx` first; error inspection switches from `awserr.Error` to v2 `smithy` APIErrors. | **SELF-CONTAINED** (management-side) | Pure CPO↔AWS protocol/SDK change; does not touch the 4.20 guest. But it rewrites every cloud imperative path and its error/retry typing — a whole behavioral surface a manifest diff cannot see, and a regression-risk concentration point.

### Reconcile core — `hostedcontrolplane_controller.go`
2. `reconcileDeletion` + **NEW** `deleteAWSEndpointServices` | On AWS deletion, lists `AWSEndpointServiceList` in the HCP ns, deletes each, and **requeues every 10s until the CPO finalizer `hypershift.openshift.io/control-plane-operator-finalizer` is removed or a per-object timeout elapses**. New k8s LIST/DELETE + new blocking wait in teardown. Absent at 4.20. | **COORDINATION-RISK** | HCP deletion now stalls on another controller (awsprivatelink) removing a finalizer. A 4.20-era management environment's awsprivatelink controller may not remove this finalizer → teardown hangs until timeout. (Same hazard the 5.0 run flagged; now confirmed NEW relative to 4.20 too.)
3. **NEW** `reconcileDeprecatedConfigurationStatus` | Adds `HostedClusterConfigurationDeprecated` condition. Absent at 4.20. | **SELF-CONTAINED** | Reads HCP annotations only; message-only.
4. `Reconcile` | **NEW** always sets `Status.Initialization.ControlPlaneInitialized=true`. Absent at 4.20. | **SELF-CONTAINED (w.r.t. guest)** | Consumed by management-side CAPI v1beta2; only risk is an old *management* CAPI, outside this skew axis.
5. `validateAWSKMSConfig` + **NEW** `validateKMSKeyDirect` + **NEW** `defaultGetGuestToken`/`getGuestToken` seam | At 4.20: GETs guest client, mints SA token, builds `kms.New(session,...)`, probes; empty Region tolerated. At main: (a) **checks `KMS.AWS.Region==""` FIRST → `ValidAWSKMSConfig=False/InvalidConfigurationReason`**; (b) token acquisition behind injectable `r.getGuestToken`; (c) on token failure calls **new `validateKMSKeyDirect`** which fires a direct `kms.Encrypt` with the CPO's OWN creds and maps AWS error codes; (d) v2 KMS client with explicit region. All new at 4.20. | **COORDINATION-RISK** | A 4.20 HCP that legitimately omitted `KMS.AWS.Region` now flips to `ValidAWSKMSConfig=False` with no manifest change; and the CPO now issues a cloud `kms.Encrypt` it never made at 4.20 (new IAM surface + new Unknown/False semantics).
6. `reconcileAvailabilityStatus` / `reconcileAvailabilityAndReadyStatus` | At BOTH 4.20 and main the component-availability recompute guard is `if !alreadyAvailable` — the `|| healthCheckErr != nil` extension the 5.0 run reported landed in the **5.0→main** window and is present in main. Against a 4.20 floor this is still a delta (main recomputes on health-check error; 4.20 did not), but it is NOT new relative to 5.0. | **SELF-CONTAINED** | Pure internal status recompute; can flip `HostedControlPlaneAvailable`/message when a 4.20 guest's KAS LB health flaps.
7. `reconcileKonnectivityCerts` + `pki/konnectivity.go` + `pki/ca.go:ReconcileKonnectivityConfigMap` | **Protocol change.** 4.20 used ONE `konnectivity-signer`; `ReconcileKonnectivityConfigMap(cm, ownerRef, konnectivityCA)` (single CA). Main adds 4 dedicated signers (`ServerServing/ClusterServing/ServerAuth/ClientAuth`), re-signs server/cluster/client/agent certs from their own signers, and the configmap signature is now `(…, signers ...*Secret)` aggregating N. Present 4.20→main AND 5.0→main (ongoing migration). | **COORDINATION-RISK** | konnectivity-agent runs on the 4.20 guest data plane; mis-sequenced rotation could transiently drop the konnectivity tunnel. Legacy signer retained to soften. (Same as 5.0 finding; confirmed also-moving at 4.20.)
8. `reconcileDefaultSecurityGroup` & `reconcileValidIDPConfigurationCondition` | **NEW staleness guard** (`evaluatedGeneration := hcp.Generation`; return error to force requeue if `hcp.Generation` changed mid-closure) **and** migration to `statuspatching.PatchStatus*` re-fetch/retry closures. Both new at 4.20. | **SELF-CONTAINED** | New requeue/error behavior on generation races; converges to same conditions.
9. `reconcileGCPPlatformCerts` + `pki/gcp_workload_identity_federation_webhook.go` | **NEW** GCP WIF webhook serving cert reconciler. Additive. | **SELF-CONTAINED** | GCP-only, additive.
10. `healthCheckKASEndpoint`/`healthCheckKASLoadBalancers` + new `kasHealthCheckURL`/`parseFailingHealthChecks` | Now take `ctx` and `*kas.KASHealthMetrics`; emit request-duration/available gauges; health-check body parsing extracted. | **SELF-CONTAINED** | Observability + ctx plumbing; probe semantics preserved.
11. `update`/`reconcileDeletion`/`reconcileDefaultSecurityGroup`/`removeCloudResources`/`reconcileValidIDPConfigurationCondition`/`removeHCPIngressFromRoutes` | Migrated from capture-then-`Status().Patch(MergeFromWithOptimisticLock)` to `statuspatching.PatchStatus[Condition]` re-fetch/retry closures with "don't clobber concurrent True/ingress" re-checks. | **SELF-CONTAINED** | Conflict-handling improvement; retry/requeue-on-conflict behavior changed.

### Infra — `infra/infra.go`
12. `reconcileAPIServerServiceStatus` / route reconcilers | **NEW route status-writer handshake**: for HCP-labeled routes without `RouteStatusWriterAnnotation == "hcp-router"`, the CPO now PATCHES the annotation onto the route (0 occurrences at 4.20, 2 at main). Also shared-ingress rewritten: `sharedingress.UseSharedIngress()/KasRouteHostname/ExternalDNSLBPort` → `netutil.UseSharedIngressHCP(hcp) || netutil.UseSwiftNetworkingHCP(hcp)` with new OpenStack-Swift branch. | **COORDINATION-RISK (mild)** | New imperative PATCH verb on route objects + new routing predicate (`UseSwiftNetworkingHCP`) that didn't exist at 4.20; changes which ingress hostname/port path is chosen and introduces a status-writer ownership contract the 4.20 router may not honor.

### PKI
13. `pki/etcd.go` | **NEW etcd-shard cert reconcilers** (`ReconcileEtcdShardServerSecret`, `ReconcileEtcdShardPeerSecret`) + `discoveryService` constant. Supports etcd sharding (e.g. `etcd-events`). Absent at 4.20. | **SELF-CONTAINED** | Additive cert material tied to framework `isEtcdComponent` sharding (see #16).
14. `pki/*` CSI/driver/operator serving certs | NEW serving-cert reconcilers across aws-ebs-csi, azure-disk/file, gcp-pd, cluster-storage, csi-snapshot, scheduler. Additive. | **SELF-CONTAINED** | Additive CA/serving material.
15. `pki/ca.go`, `pki/kas.go`, `cloud/aws|azure|openstack/providerconfig.go` | Package/constant relocations + ctx plumbing; no logic change confirmed. | **SELF-CONTAINED** | Refactor.

### v2 framework — `support/controlplane-component/`  *(the structural story — see (c))*
16. `status.go:checkDependencies` | **Gating-condition change.** 4.20: KAS auto-added as a dependency unless in `componentsExcludedFromKubeAPIServerDependency`. Main adds `&& !isEtcdComponent(c.Name())` — **any etcd shard (`etcd-*`) is now excluded from the KAS dependency**. New `isEtcdComponent` prefix-matcher used throughout. | **SELF-CONTAINED** | Changes which components gate on KAS availability during rollout; internal to management-side ordering, but it IS a change to the dependency-gating engine the 5.0 run found frozen.
17. `status.go:reconcileComponentStatus` | **NEW** `component.Status.ObservedGeneration = cpContext.HCP.Generation`; manifest iteration now keyed on `c.AssetDirName()` and `c.loadManifest` (template-aware). | **SELF-CONTAINED** | New status field + templated/asset-dir manifest resolution.
18. `builder.go` + `controlplane-component.go` + `deployment.go`/`statefulset.go` | **NEW template/asset-dir machinery**: `WithAssetDir`, `WithTemplateData`, `AssetDirName()`, `loadManifest`/`LoadManifestTemplated`, Go-template rendering of asset YAML before decode. Lets multiple components share an asset dir. Absent at 4.20. | **SELF-CONTAINED** | Alters how manifests are loaded/named; the delete path explicitly no longer calls adapt funcs (nil-panic fix). Convergence target unchanged but load/delete control-flow changed.
19. `generic-adapter.go` | **NEW `SkipManifest()`** (neither create nor delete a shared-dir manifest) + `NewGenericControllerConfigAdapter` (TLS-profile-driven config.yaml). | **SELF-CONTAINED** | New create/delete gating per-manifest.
20. `common.go:AdaptPodDisruptionBudget` | **Logic rewrite.** 4.20 always wrote a PDB. Main gates behind predicate `ControllerAvailabilityPolicy != SingleReplica` — SingleReplica gets NO PDB and existing one is deleted; HA clears minAvailable + sets maxUnavailable=1. Plus new `IsStorageAndCSIManaged` (IBM/PowerVS → false). | **SELF-CONTAINED** | Changes which objects the framework creates/deletes per HCP spec. (Same as 5.0 finding.)
21. `defaults.go` | **NEW algorithm** `setControlPlaneIsolation` short NoExecute tolerations (10s api-critical / 60s etcd) via real `ToleratesTaint`; **NEW GCP** `EnforceRestrictedSecurityContextToContainers` for all containers incl. sidecars; `enforceImagePullPolicy` now `PullAlways` for `:latest`; nil-map guards in `applyRequestsOverrides`; `isEtcdComponent` used for FSGroup/priorityClass/replicas. | **SELF-CONTAINED** | Management-side pod defaulting; affects HA eviction timing + GCP SC, not guest/operand behavior.
22. `konnectivity-container.go` | **NEW** `PreferIPv4` arg injection for single-stack-IPv4 data planes (`isSingleStackIPv4`), and **NEW** `proxy.SetEnvVars` when `connectsDirectlyToCloudAPIs()`. | **ASSUMES-NEWER-OPERAND (mild)** | Emits `--prefer-ipv4=` flag to the konnectivity-proxy/socks5 binary. If a 4.20 payload's konnectivity binary predates that flag, the flag is unknown → container could fail to start. **UNCONFIRMED** whether 4.20's konnectivity-proxy accepts `--prefer-ipv4`.
23. `token-minter-container.go` | **NEW native-sidecar path**: when `NativeSidecarContainersEnabled`, token-minter injected as a `RestartPolicy=Always` init container with a StartupProbe gating main containers; else legacy sidecar. New `injectContainer` helper. | **SELF-CONTAINED (management-side)** | K8s≥1.29 management feature; depends on management cluster, not guest.

---

## (c) Framework-gating behavior change (4.20 → main) and skew implications

**This is the single most important structural finding and it INVERTS the 5.0 result.**

- At **5.0**, the gating/rollout/availability engine (`status.go`, `builder.go`, `deployment.go`, `statefulset.go`, `generic-adapter.go`) was **byte-identical** to main.
- At **4.20**, **all five of those files differ from main**. The changes landed in the **4.20→5.0 window**:
  - `status.go:checkDependencies` — **etcd shards excluded from the KAS dependency** (new `isEtcdComponent`). This is a genuine change to the dependency-gating condition, not cosmetics.
  - `status.go:reconcileComponentStatus` — new `ObservedGeneration` status write; manifest resolution now template/asset-dir aware.
  - `builder.go` / `controlplane-component.go` / `deployment.go` / `statefulset.go` — new template-rendering + shared-asset-dir manifest loading; delete path no longer runs adapt funcs.
  - `generic-adapter.go` — `SkipManifest` (per-manifest create/delete gating) + generic-controller-config adapter.
- `common.go` (PDB predicate-gating) and `defaults.go` (tolerations, GCP SC) also changed — those changed in BOTH windows.

**Rollout-complete / at-desired-version derivation (`checkOperandsRolloutStatus`, deployment/statefulset readiness) is algorithmically unchanged** (only `util.IsDeploymentReady`→`podspec.IsDeploymentReady` relocations). So the *convergence math* is stable; what moved is **(1) the dependency graph (etcd↔KAS gating), (2) manifest loading/skip/templating, and (3) PDB/toleration defaulting**.

**Skew implication:** a main CPO driving a 4.20 payload will apply a **different dependency-gating graph** than a 4.20 CPO would (etcd shards no longer wait on KAS), plus templated/shared-asset manifest loading and `SkipManifest` semantics the 4.20 components were not authored against. The "framework introduces no new assumes-newer gating" conclusion from the 5.0 run **does NOT carry to 4.20** — the framework itself moved. The gating change is still SELF-CONTAINED (management-side ordering), but the structural assumption the 5.0 run leaned on is void at this floor.

---

## (d) DELTA vs the 5.0 behavioral baseline — what NEWLY moved at 4.20

**Carried over from 5.0 (also moving at 4.20, same class):** AWSEndpointService teardown wait (#2), KMS region guard + validateKMSKeyDirect (#5), konnectivity dedicated-signer migration (#7), availability recompute-on-health-error (#6, but landed 5.0→main), SG/IDP staleness guards + statuspatching (#8, #11), deprecated-config + controlPlaneInitialized (#3, #4), PDB predicate-gating (#20), short tolerations (#21), GCP WIF webhook cert (#9).

**NEW at 4.20 that the 5.0 run did NOT report (did not exist in the 5.0→main diff):**
1. **AWS SDK v1→v2 migration** (#1) — entire cloud imperative surface changed call shape + error typing. Biggest new category.
2. **Framework gating CHANGED** (#16–#19, section c) — 5.0 reported this frozen; at 4.20 it moved (etcd↔KAS dependency exclusion, template/asset-dir loading, SkipManifest, ObservedGeneration). **Most important structural inversion.**
3. **Reconcile status-derivation split** — `reconcileEtcdStatus`/`reconcileKASStatus`/`reconcileDeprecatedConfigurationStatus`/`reconcileControlPlaneVersionStatus` are new functions vs 4.20 (landed 4.20→5.0); the 5.0 run saw them already present.
4. **PKI monolith → ~12 ordered sub-reconcilers** (#a.5) — reorganized failure/requeue boundaries vs 4.20's single `reconcilePKI`.
5. **infra.go route status-writer handshake + Swift-networking predicate** (#12) — new imperative route PATCH + new routing branch absent at 4.20.
6. **etcd sharding** (#13, #16) — shard cert reconcilers + framework etcd-prefix handling; absent at 4.20.
7. **konnectivity `--prefer-ipv4` flag + proxy env propagation** (#22) — new operand flag; potential assumes-newer-operand.
8. **Native-sidecar token-minter** (#23) — new injection mode vs 4.20.
9. **GCP restricted security-context enforcement** (#21) — new all-container SC pass.

**Count: 9 NEW hazard categories at 4.20 that were not in the 5.0 baseline** (vs ~5 carried-over). The 4.20 floor exposes roughly double the behavioral movement.

---

## (e) Ranked top behavioral hazards a manifest diff would MISS (vs a 4.20 guest, under a main CPO)

1. **Framework dependency-gating change — `support/controlplane-component/status.go:checkDependencies` (SELF-CONTAINED, but STRUCTURAL).** etcd shards no longer gate on KAS; manifest loading is now template/asset-dir/SkipManifest driven. 5.0 proved this frozen; at 4.20 it moved. Invisible to fixtures; invalidates the "framework is quiet" assumption. **The single most important finding.**
2. **AWS SDK v1→v2 migration — `hostedcontrolplane_controller.go:GetEC2Client` + all SG/KMS funcs (SELF-CONTAINED).** Every cloud call + error/retry path rewritten. A whole regression surface no YAML diff reveals.
3. **AWSEndpointService teardown wait — `hostedcontrolplane_controller.go:deleteAWSEndpointServices` (COORDINATION-RISK).** New 10s-requeue/timeout loop blocking HCP deletion on another controller removing a finalizer; a 4.20-era awsprivatelink controller may stall teardown.
4. **AWS KMS region guard + direct-probe fallback — `validateAWSKMSConfig`/`validateKMSKeyDirect` (COORDINATION-RISK).** Flips `ValidAWSKMSConfig=False` for a 4.20 HCP that omitted Region; issues a new `kms.Encrypt` on CPO creds the 4.20 CPO never made.
5. **Konnectivity dedicated-signer cert migration — `reconcileKonnectivityCerts` + `pki/konnectivity.go`/`pki/ca.go` (COORDINATION-RISK).** Re-signs server/cluster/client/agent certs from 4 new signers; the konnectivity-agent runs on the 4.20 guest — mis-sequenced rotation can transiently drop the tunnel. Legacy signer mitigates.

Runner-up (watch): **konnectivity `--prefer-ipv4` flag — `konnectivity-container.go` (ASSUMES-NEWER-OPERAND, UNCONFIRMED)**: a new CLI flag passed to the konnectivity-proxy binary; if the 4.20 payload's binary predates it, the container could fail to start.

---

## Confidence / UNCONFIRMED notes
- Findings derived from Go diff logic; no compilation (per instructions).
- Whether `ValidAWSKMSConfig=False` (#5) hard-blocks KAS vs merely annotates is **UNCONFIRMED** (depends on degraded/validity aggregation elsewhere).
- Konnectivity rotation-ordering break (#7) is reasoned, not proven — **UNCONFIRMED** severity.
- Whether 4.20's konnectivity-proxy binary accepts `--prefer-ipv4` (#22) — **UNCONFIRMED**; it is the one credible assumes-newer-operand flag.
- etcd reencryption / etcd-maintenance / backup *state machines*: not inspected line-by-line here beyond cert/sharding; `manifests/etcd.go` changed substantially (sharding), but the reencryption/maintenance control flow is likely in a different file — **UNCONFIRMED unchanged** at this floor (5.0 run claimed unchanged; not re-verified against 4.20).
- Exact window-attribution (4.20→5.0 vs 5.0→main) confirmed via `git show` for the headline items (SDK, framework files, status/PKI split); treat secondary items as "present in 4.20..main" unless window is stated.
