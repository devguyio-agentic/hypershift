# CPO Version-Agnosticism — Architecture Workshop (rolling notes)

Living notes for the workshop on whether a single, version-agnostic Control Plane
Operator (built from HyperShift `main`) can drive guest clusters whose component
binaries + guest API come from an older OCP release payload.

- Status: ACTIVE workshop. These are rolling notes, updated as we go.
- Goal of the session: understand the NATURE of the problem and explore options.
  NOT to short-circuit to a JIRA/OCPSTRAT/PR. Design exploration only.
- Repo under study: `~/workspace/hypershift`.
- Branches compared: `upstream/release-5.0` vs `upstream/main`.
- `main` targets 5.1 (`support/supportedversion`: `LatestSupportedVersion = 5.1.0`).
- Investigation commit reference: `253c3f5c790d08668d4aa365688e88a0ab950af8`
  (note: `upstream/main` tip has since moved to `220e0e7…`; categorical findings
  stable, exact counts are as-of-current-main).

---

## 0. Orientation / ground rules (from Ahmed)

- This is a long architecture workshop. Explore, don't execute.
- Forget OCPSTRAT / JIRA / PRs for now. Understand the problem first.
- "It's NOT just about rendering manifests" — behavioral/logic differences matter
  as much as or more than rendered-config differences. (Correction logged; drove
  the second, behavioral, agent batch.)
- Read the source for exact behavior; evidence over assertion. Verify self-reports
  from subagents against source/live before trusting.

---

## 1. What the CPO is (shared understanding)

The CPO is a **per-HostedControlPlane controller-manager** — one instance per hosted
cluster, running in that cluster's control-plane namespace on the MANAGEMENT cluster.
The HO spawns it; its root desired-state object is the `HostedControlPlane` (HCP) CR.

Key structural facts:
- **binary ≠ process ≠ controller ≠ managed component** (the critical distinction).
  - The `control-plane-operator` image is multi-binary: `run` (CPO), plus ~18
    argv-dispatched subcommands (HCCO, konnectivity proxies, availability-prober,
    token-minter, ignition-server, etcd-defrag/backup/upload, kas-bootstrap,
    sync-global-pullsecret, metrics-proxy, dns-resolver, …). Many run as SIDECARS
    inside the component pods the CPO deploys.
  - **HCCO** = same binary, different subcommand, SEPARATE process/Deployment,
    targets the GUEST API (second client/cache), leader-elects on mgmt cluster.
  - **PKI operator** (`control-plane-pki-operator`) = SEPARATE binary in the same
    image (built via `make control-plane-pki-operator`, COPYed to /usr/bin). NOT a
    CPO subcommand. Specialized admin/break-glass trust lifecycle.
- The CPO holds LIVE cloud SDK clients (`r.ec2Client`, Azure ARM clients). It is a
  cloud-API actor, not just a Kubernetes-API actor.
- Reconcile = fan-out over a registered component list (~40 `NewComponent(...)` in
  `registerComponents`, run via the v2 framework) + a long sequence of hand-written
  imperative `reconcile*` methods (PKI, infra, certs, KMS, SG, kubeadmin, ignition).

### The CPO's work, in layers (merged taxonomy)
Reference deep-dive: `cpo-summary.txt` (traced at 253c3f5c, 188 citations).
1. **Materialize an OpenShift control plane** — render Deployments/STS/Jobs/Services/
   Routes/RBAC/config/creds/monitoring from HCP spec. (Component IMAGES come from the
   guest release payload; CPO authors the CONFIG.)
2. **Coordinate bootstrap & dependencies** — infra-first, then PKI/config/creds, then
   component adapters; v2 framework gates on availability + rollout + desired version.
3. **Coordinate release transitions** — resolve release metadata/images, guard an old
   CPO from reconciling a different desired release, aggregate ControlPlaneVersion
   status/history. (NOT atomic, no auto-rollback.)
4. **Bridge mgmt ↔ guest state** — HCCO: guest config/bootstrap, cred/trust sync,
   observe guest version/auth, worker ops.
5. **Execute operational protocols** — encryption migration, cert/trust transitions,
   node drain/upgrade, etcd defrag, token renewal, private endpoint/DNS.
6. **Observe / report / tear down** — validation, health probing, status, guarded
   async cloud cleanup on delete.

### Problem categories it solves (not one oversized templating problem)
Hosted topology; distribution assembly; SAFE TRANSITIONS (intermediate states matter);
state preservation/availability (etcd, worker upgrades); cross-cluster authority/sync;
external resource lifecycle; truthful lifecycle reporting.

### Operands with behavior beyond "reconcile the manifest shape"
etcd (defrag algo, member-reset init script, restore, backup); KAS (status, kms,
bootstrap, LB health); machine-approver (TLS-profile-driven config — the bug site);
CVO (mgmt metrics access); ignition (build-and-serve pipeline); KMS (live cloud
validation); storage/CSI (platform env). Plus operand-less imperative cloud work
(default SG, private-link endpoints, LB health).

### Operator family / analogues
Hosted-control-plane lifecycle operator with delegated operators. Closest analogues:
**Gardener** (system-level, Seed/Shoot hosting), **Kamaji** (TenantControlPlane in
pods — closest focused analogue), **CAPI KubeadmControlPlane** (but machine-hosted,
not workload-hosted), **OpenShift KAS static-pod operator** (single-component
analogue), **Crossplane** (conceptual composition only). One-line: a per-tenant HCP
operator that is simultaneously a relocated CVO, a cloud-infra provisioner, and a PKI
authority — no single mainstream operator occupies all four roles at once.

---

## 2. The empirical result that anchors the workshop

Live experiment: main-built (5.1) CPO pinned via `--control-plane-operator-image`
onto a 5.0 guest payload (`5.0.0-rc.5-multi`), 3 workers, on `devguyio-aabdelre-mgmt`.

- **35/36 control-plane components came up healthy** under the 5.1 CPO on the 5.0
  payload (etcd, KAS, KCM, scheduler, CVO, network, storage, ingress-operator, …).
  The feared etcd `--listen-tls-min-version` skew did NOT occur.
- **1 component broke: machine-approver.** The main CPO renders
  `--tls-curve-preferences=4588,29,23,24`; the 5.0 machine-approver BINARY (from the
  payload) rejects it: `unknown flag: --tls-curve-preferences` → CrashLoopBackOff.
- Cascade: machine-approver down → bootstrap CSRs never approved → 0 nodes join →
  guest ClusterVersion never completes → HCP `Available=False`.
- Verified independently against the live cluster + source (not just subagent report).
- Report: `/tmp/ocpstrat-3588-qe-report.md`. Images:
  HO `quay.io/abdalla/hypershift:main-253c3f5c`,
  CPO `quay.io/abdalla/control-plane-operator:main-253c3f5c`.
- The HC `cpo-main-50` + 3 EC2 machines are STILL RUNNING on the mgmt cluster (left
  up for inspection; destroy with `bin/hypershift destroy cluster aws --name
  cpo-main-50 --namespace clusters --aws-creds /tmp/aws-creds.txt --region us-east-1`).

Takeaway: binary-decoupling is MOSTLY viable; the failure is config-authorship skew —
the CPO emitting config for a payload binary it no longer shares a version with.

---

## 3. Working model: the skew BOUNDARIES (central framing)

The CPO's version-agnosticism question is really: **where does the CPO's logic hold a
hidden assumption about the version of the thing on the other end of a call/emission?**

Three boundaries, with a severity gradient confirmed by the rendering diff pass:

| Boundary | What's on the other end | Travels with | Failure mode | Severity |
|---|---|---|---|---|
| **CPO → operand binary** | component binaries from the PAYLOAD | guest payload (old) | hard crash (`unknown flag`/field) | LOUD |
| **CPO/HCCO → guest API** | guest cluster API/CRDs from the PAYLOAD | guest payload (old) | silent prune / ownership conflict | QUIET, nastier |
| **CPO → own HCP API** | HCP/HC CRDs | the HO/CPO itself (moves together) | none — can't skew | FALSE surface |

The third IS a false skew surface: HCP/HC CRDs travel with the CPO, so a new field is
always readable by the CPO that added it. Graded FINE/low and it holds. (Kind C items
in §5 are condition-semantics changes on the CPO's own reconcile path, not skew.)

### Updated model: THREE real skew failure modes (+ a non-skew aside)

| Failure mode | Where | Visible to | Example |
|---|---|---|---|
| **Loud reject** | operand binary (payload) | manifest diff | `--tls-curve-preferences` |
| **Silent prune / ownership** | guest API (payload) | manifest diff (shape) | guest VAP-v1 |
| **Quiet stall** | protocol across a boundary | BEHAVIOR diff only | AWSEndpointService delete hang; konnectivity rotation |

Non-skew aside (Kind C in §5): a handful of condition-semantics changes on the CPO's
OWN reconcile path (KMS-region condition, availability flap). Real but NOT skew — they'd
affect a same-version cluster too. Do not inflate these into the skew story.

Reframing of "version-agnostic CPO": every OUTBOUND interaction (to an operand binary,
the guest API, or a cross-boundary protocol) must be **capability/version-gated, not
version-assumed**. The guest version is already in hand (see note below) — the gap is
that gating is applied unevenly, not that the signal is missing.

> Version signal is NOT an open question. The guest OCP version is
> `HCP.spec.releaseImage` (CP side: `spec.controlPlaneReleaseImage`), resolved by the
> CPO and exposed to EVERY component adapter as `cpContext.ReleaseImageProvider.Version()`
> — available before any node exists. The existing Kind-B gates (CCO `>=4.23`) read
> exactly this. So "how does the CPO know the version" was a non-question; the signal is
> present and already plumbed to the gating site.

---

## 4. Diff inventory — RENDERING pass (what the CPO emits)

Batch `deleg_37dc4450`, 3 agents. Artifacts:
`~/workspace/cpo-from-main/diff-inventory/agent-{1,2,3}-*.md`.
Category schema C1–C12 (flag/arg/env; component-set; dep-graph; API-consumption;
manifest-shape; semantic-protocol; image-resolution; infra/cloud; pki/cert;
feature-gate; shared-config-emission; guest-resource-shape).

Key signal: **most diffs are FINE under skew** and agents were disciplined about it
(`--v=` klog everywhere, probe retunes evaluated by kubelet against endpoints 5.0
already serves, platform-gated sidecars, 7/21 "changed" components are pure
package-refactors). The genuine hazard surface is SMALL and CONCENTRATED.

### Confirmed/ranked rendering hazards
1. **`--tls-curve-preferences=4588` (X25519MLKEM768, post-quantum)** — BREAKS
   (live-proven). ROOT: `support/config/groups.go::CurvePreferences()` is BRAND NEW on
   main (absent on 5.0). One source, many blast radii (machine-approver + any other
   `--tls-curve-preferences` consumer). Ciphers + MinTLSVersion UNCHANGED — curve axis
   is the only new TLS value.
2. **HCCO writes guest `ValidatingAdmissionPolicy` (admissionregistration/v1)** —
   BREAKS/SILENT-NOOP vs an older-k8s 5.0 guest (UNCONFIRMED on 5.0 k8s minor).
3. **Konnectivity PKI: 5 signer CAs replace 1 single konnectivity-signer** —
   COORDINATION-RISK (consumer lives in v2/, cross-check pending).
4. **AWS SG status moved `Status.Platform.*` → `Status.Platform.AWS.*`** —
   SILENT-NOOP (empty read by old readers).
5. **guest RequestCount CRD ownership + capability-gated guest RBAC** —
   COORDINATION-RISK.

### Important meta-finding (thread to pull)
The codebase ALREADY version-gates SOME emissions (CCO TLS args gated ≥4.23/5.x; capi
`--skip-crd-migration-phases` gated ≥4.20) but NOT the new curve. So "gate emissions
on guest version" is an ESTABLISHED pattern, just UNEVENLY applied. The framework adds
no systematic safety net that would stop a new emission from reaching an old operand.
→ Candidate design question: what would a SYSTEMATIC emission-gating mechanism be?

### API field churn (api/hypershift)
~17 spec+status fields (~22 incl. conditions/reasons/labels). Headliners:
`Initialization.ControlPlaneInitialized` (CAPI v1beta2, HO-served → FINE),
`operatorConfiguration` per-component log-level tree (gated `HCPUserFacingOperatorLogs`),
AWS/Azure/KubeVirt resourceTags + CPUOptions/IPForwarding/Model, new conditions.
HCP/HC CRDs are HO/CPO-served → the self-API boundary → FINE for CPO skew.

---

## 5. Diff inventory — BEHAVIORAL pass (what the CPO DOES)

Batch `deleg_55f6e1d8`, 3 agents. Artifacts:
`~/workspace/cpo-from-main/diff-inventory/agent-B{1,2,3}-*.md`. Read `.go` control
flow, NOT fixtures.

### STRUCTURAL headline: the v2 framework did NOT change
`support/controlplane-component` version/rollout/dependency gating is BYTE-IDENTICAL
across branches (`status.go`, `builder.go`, `generic-adapter.go`). Implication: the
orchestration ENGINE is stable; ALL skew risk lives in imperative per-component /
per-reconciler logic. There is NO systematic safety net, and the framework didn't grow
one. The version-gating that exists (CCO, capi) is hand-rolled, scattered, inconsistent.
(Only framework deltas: PDB predicate for SingleReplica; node-failure tolerations —
both SELF-CONTAINED.)

### Behavioral hazards sort into THREE kinds

Kind A — COORDINATION-RISK (quiet stalls in cross-boundary protocols; invisible to
manifest diff):
- `hostedcontrolplane_controller.go:deleteAWSEndpointServices` — new teardown step
  BLOCKS HCP deletion (10s requeue / 10-min timeout) waiting for a (5.0-era)
  awsprivatelink controller to clear a finalizer; stuck finalizer under skew.
- `reconcileKonnectivityCerts` + `pki/ca.go` — re-signs konnectivity certs from 4 NEW
  dedicated signers + aggregate CA; the agent runs on the 5.0 DATA PLANE, so
  mis-sequenced rotation can transiently drop the guest↔CP tunnel. (Confirmed from BOTH
  the rendering pass (§4 #3) and the behavior pass — same hazard, two lenses.)
- `support/konnectivityproxy/dialer.go:DialContext` — new multi-IP fallback loop under
  a 90s dial budget; changes failure latency for every HCP→guest connection.

Kind B — ASSUMES-NEWER-OPERAND but ALREADY SELF-GATED (the GOOD pattern):
- CCO TLS args gated `>=5 || >=4.23`; capi `--skip-crd-migration-phases` gated `>=4.20`;
  GCP WIF webhook platform-gated. FINE for a 5.0 guest — these show the codebase CAN
  gate emissions on guest version; it just does so unevenly.

Kind C — SELF-INFLICTED (CPO condition/behavior changes on its own reconcile path):
- `validateAWSKMSConfig` — flips `ValidAWSKMSConfig=False` when `KMS.AWS.Region` empty;
  a 5.0 HCP that omitted Region gets a NEW invalid condition under a main CPO.
- `reconcileAvailabilityAndReadyStatus` — availability recomputed on every KAS-probe
  error; can FLAP `HostedControlPlaneAvailable` against a blipping 5.0 KAS LB.
- `validateKMSKeyDirect` — new `kms.Encrypt` call on CPO's own creds when guest API is
  down (new side effect + condition semantics).
- (DEMOTED — minor, not a real hazard) `resolveKASVerbosity` enum mapper: reads the new
  `OperatorConfiguration.*.LogLevel`, which is a CEL-constrained enum the CPO's OWN
  admission validates, reached only via a new optional field. Code-hygiene nit, not a
  skew finding. Do not tunnel-vision on it.

### Confirmed NEGATIVES (bound the problem — these are SAFE)
- reencryption state machine UNCHANGED (only status-patch wrapper moved).
- PKI trust-replacement / CSR / revocation UNCHANGED (import moves only).
- etcd defrag/restore/backup UNCHANGED (test-only diffs).
- inplace-upgrade: only a panic-guard clamp + CAPI v1beta2 ref read.
- webhookvalidation match/scope UNCHANGED; drainer/cmca/hcpstatus/node/globalps
  logic unchanged (import-only).
- Sleeper (behavioral, benign for skew): OLM CatalogSource poll 10m→240m (24×).

### Reconcile loop ordering DID change
Normal path adds: `reconcileDeprecatedConfigurationStatus`; always-set
`Status.Initialization.ControlPlaneInitialized=true`. Deletion path adds the blocking
AWSEndpointService cleanup/requeue step (Kind A hazard above).

### New guest-API interactions main-HCCO performs that 5.0 did not: 5
3 are GCP-platform-only (SELF-CONTAINED). The skew-sensitive one: HCCO writes a guest
RBAC `ValidatingAdmissionPolicy` (admissionregistration/v1) hardening two cluster-admin
CRBs — VAP-v1 availability on an old guest UNCONFIRMED (matches rendering §4 #2).

---

## 6. Open threads / parking lot

- [ ] Systematic emission-gating: the code gates SOME emissions on guest version (CCO,
      capi — Kind B) but not others (the curve flag). Framework did NOT add a safety
      net (§5). Design question: what is a principled, systematic
      capability/version-negotiation mechanism for all outbound CPO interactions?
- [ ] Map the CPO's full OUTBOUND interaction surface (guest API, operand pods, cloud
      APIs, HCP API, etcd, KAS) as THE skew boundary set.
- [RESOLVED §5] Are the QUIET COORDINATION-RISK items real? reencryption + PKI +
      etcd-maintenance state machines UNCHANGED (safe). Real quiet risks are:
      AWSEndpointService delete-finalizer stall + konnectivity cert-rotation tunnel drop.
- [RESOLVED] Guest-version signal is NOT an open question: `HCP.spec.releaseImage` →
      `cpContext.ReleaseImageProvider.Version()`, available in every adapter, pre-nodes.
      The signal is present and already plumbed to the gating site; the gap is uneven
      APPLICATION of gating, not a missing signal.
- [ ] Decide fate of the live `cpo-main-50` cluster (still running).

---

## 7a. Second diff exercise: main vs release-4.20 (~5 minors of skew)

Rationale: widen the skew from 1 minor (5.0) to ~5 minors (4.20) to see how the skew
surface GROWS and CHANGES. Agents framed as DELTA vs the 5.0 inventory (not re-dump).
Scale: 4.20 diff is 4–6× larger per region (v2: 81→345 files; top reconciler 241→2177).
Artifacts: `~/workspace/cpo-from-main/diff-inventory-4.20/agent-{1,2,3}-*.md`.
main tip `220e0e73`, 4.20 tip `48fd5966`.

### RENDERING pass — key result: the skew surface CHANGES SHAPE with distance

1. **Framework "stability" partially collapses at 5-minor distance.** At 5.0 the v2
   framework gating was byte-identical to main. At 4.20 the CORE equality gate
   (`RolloutComplete && Status.Version == desiredVersion`) is STILL unchanged and STILL
   has NO version-floor safety net — but surrounding framework logic MOVED (etcd
   dependency-exclusion, `Status.ObservedGeneration` write, `NativeSidecarContainersEnabled`
   gating, asset-dir rendering). So "stable engine" holds at 1-minor, erodes at 5-minor;
   the DANGEROUS part (missing version-floor net) is invariant at both distances.
2. **Curve-4588 bug is DISTANCE-INVARIANT (#1 at both 5.0 and 4.20).**
   `support/config.CurvePreferences` → `--tls-curve-preferences=4588`; at 4.20 an even
   cleaner break (flag absent from 4.20 testdata: 0→8 files). NEW corroborating finding:
   **`cvo` TLSArgs is UNGATED** while the identical operation on CCO IS gated (`>=4.23`).
   Same hazard class, gated on one component, ungated on its neighbor = "uneven gating"
   thesis made concrete.
3. **Guest-API hazard class ROTATES with distance (deepest finding).** Skew is NOT
   monotonic: a hazard present at 1-minor can VANISH at 5-minor, and new ones appear
   that the narrow diff can't see.
   - 5.0 #1 guest hazard (admissionregistration VAP-v1) → FINE at 4.20 (VAP GA'd k8s
     1.30; a ~1.31 guest serves it).
   - NEW at 4.20: prometheus-operator CRD apis v0.74→v0.92 (HCCO writes SM/PodMonitor
     at v0.92 nested shape → ~v0.74 guest CRD PRUNES fields); OVN IPv6 subnet fields on
     guest `operatorv1.Network` (older CNO CRD prunes → dual-stack silently lost).
   - IMPLICATION: you CANNOT extrapolate the skew surface linearly from one comparison.
     The 5.0 diff would have named the WRONG top guest hazard for 4.20.
4. **Version-floor arithmetic flips individual outcomes (as hypothesized).**
   - CCO TLS args `>=4.23` → PROTECTS a 4.20 guest (the gate that looked redundant at
     5.0 is load-bearing at 4.20).
   - capi `--skip-crd-migration-phases` `>=4.20` → no-op at the floor (emits, but 4.20's
     own capi introduced it, so benign).
   - One 5.0 hazard DISAPPEARS: SG status `.Platform.*→.Platform.AWS.*` predates 4.20.
5. **New 4.20 rendering hazards (UNCONFIRMED, need 4.20 binary flag-set check):**
   `--config` + `--terminate-on-files` on cluster-storage-operator /
   csi-snapshot-controller-operator / control-plane-pki-operator (absent in 4.20
   assets, ungated); ingress-operator `--haproxy-image`/`--default-haproxy-version`
   (5.1-era multi-haproxy, ungated); HCCO `--controllers=…,user-ca-bundle` (5.0 HCCO
   may lack the controller — escalated confidence at 4.20).
6. **PKI protocol: NOW changed at 4.20** (was byte-neutral at 5.0) — revocation rework
   (`SyntheticRequeueError`→`AddAfter`, new 10-min revocation-timeout condition, step
   requeue fixes). COORDINATION-RISK. Konnectivity 1→5 signer split PLUS a new KAS
   bootstrap-container signer (0→3 refs).
7. **API field churn: ~193 new `json:` fields vs ~17 at 5.0** (whole new trees: GCP,
   Azure PrivateLink/PSC, etcd backup/sharding, operatorConfiguration). ALL HCP/HC/NP
   CRDs HO/CPO-served → FINE. Confirms the self-API false-skew-surface holds at ANY
   distance: volume ≠ hazard.

### BEHAVIORAL pass (4.20) — every 5.0 "invariant" MOVED at 5-minor distance
Artifacts: `agent-B{1,2,3}-*.md` in the 4.20 dir. The behavioral pass confirms the
distance-variable thesis and corrects one hypothesis.

1. **Everything "stable" at 1-minor had moved by 5-minor:**
   | Subsystem | 5.0→main | 4.20→main |
   |---|---|---|
   | v2 framework gating | UNCHANGED | CHANGED (dep graph: etcd shards excluded from KAS dep; ObservedGeneration; asset-dir loading) |
   | PKI protocol | UNCHANGED | CHANGED (revocation requeue/timeout overhaul) |
   | reencryption (HCCO) | UNCHANGED | ENTIRELY NEW CONTROLLER (4.20 had no rotation SM at all) |
   | etcd maintenance | UNCHANGED | PARTIAL (defrag/restore same; backup SDK-rewritten; quorum helper removed) |
   | Reconcile ordering | +2 steps | deletion extracted; status split per-domain; reconcilePKI monolith→~12 sub-reconcilers |
   | new guest-API interactions | 5 | ~10 |
   | new behavioral hazard families | ~5 | ~8–9 (≈2× the movement) |
   => "Stable orchestration engine" was a SMALL-SKEW ARTIFACT, not a system property.
   Also: AWS SDK v1→v2 migration landed in the 4.20→5.0 window (whole cloud call/error
   surface rewritten) — SELF-CONTAINED but a large behavioral delta.

2. **HYPOTHESIS CORRECTED — gates do NOT flip; they SELF-TRACK. (count = 0)**
   Every version gate reads the OPERAND'S OWN `ReleaseImageProvider.Version()`, so a
   `>=4.23` gate goes INERT at a 4.20 guest (correctly suppresses) and `>=4.20` fires at
   the floor against the matching operand. So WHERE THE CODE GATES AT ALL, gating is
   correct and version-pair-aware BY CONSTRUCTION. The hazard is never a mis-calibrated
   gate — it is the TOTAL ABSENCE of a gate on new behavior. Sharpens the thesis:
   **the problem is BINARY — gated = safe-at-any-distance; ungated = hazard-at-any-distance.**
   ("Uneven gating" was imprecise; it's "gated vs not-gated," and the live-version read
   makes every existing gate self-calibrating.)

3. **BUT guest-API skew does NOT self-track.** HCCO writes to the guest at whatever
   apiVersion/shape the VENDORED client uses, unconditionally — not gated on
   `ReleaseImageProvider.Version()`. That is why the guest-API boundary stays dangerous
   (prometheus-operator CRD prune, OVN IPv6) while the operand-flag boundary is "just add
   a gate." Two structurally different fixes for the two outbound boundaries.

4. **New behavioral hazards at 4.20 (top, file:func | class):**
   - `certificaterevocationcontroller.go:syncCertificateRevocationRequest` | COORD-RISK
     (PKI revocation requeue rework + 10-min terminal timeout; ungated)
   - `reencryption/reencryption.go:handleMigratingPhase` | ASSUMES-NEWER-GUEST+COORD-RISK
     (NEW controller; guest StorageVersionMigration `migration.k8s.io/v1alpha1` polling)
   - `kas/auth.go:generateUserValidationRules` | ASSUMES-NEWER-OPERAND
     (`ExternalOIDCWithUpstreamParity` default-on @main, absent @4.20; CEL/UserValidationRules
     an older KAS may reject)
   - `etcd-backup/etcdbackup.go` | COORD-RISK (AWS SDK v1→v2, cert path, tag encoding)
     and `token-minter-container.go` | ASSUMES-NEWER-GUEST (native-sidecar init pattern, k8s≥1.29)
   - konnectivity `--prefer-ipv4` passed to proxy (UNCONFIRMED: 4.20 binary may predate flag)
   - new guest workloads: KAS-connection-checker, DataPlaneConnection `pods/log` read.
   New guest-API interactions ~10 (vs 5 at 5.0); only 1–2 are ~k8s-1.31-risky
   (StorageVersionMigration, PodMonitor) — the rest are GA core/RBAC/VAP-v1 a 1.31 guest serves.

### Cross-cutting conclusion (both distances, both lenses)
The skew surface has an INVARIANT CORE and a DISTANCE-VARIABLE SHELL:
- INVARIANT (hazard at every distance): UNGATED new emissions (curve-4588 archetype);
  the framework's missing version-FLOOR safety net; the self-API false skew surface; the
  three-boundary model.
- DISTANCE-VARIABLE: WHICH guest-API shapes prune (rotates with k8s minors — VAP-v1
  vanishes by 4.20, prometheus-CRD-prune appears); HOW MUCH framework/PKI/reencryption/
  etcd logic has moved (nearly nothing at 1-minor, a lot at 5-minor); how many controllers
  simply don't exist in the old branch.
- KEY MECHANISM: version gates SELF-TRACK (read live operand version), so the fix for the
  OPERAND-FLAG boundary is "make gating mandatory, not optional." The GUEST-API boundary
  does NOT self-track (unconditional vendored-client writes) and needs a different fix.
- CONSEQUENCE: a version-agnostic CPO cannot be validated against ONE reference guest;
  the hazard set is a function of the specific (CPO, guest) version PAIR. BUT the
  ENGINEERING fix is tractable because it's binary (gate present/absent), and existing
  gates prove the mechanism (live-version read) already works.

---

## 7. Decisions / conclusions log

(empty — to be filled as we converge on anything)

---

## 8. Design ideas (separate files)

- **Idea 1** — `IDEA-1-universal-cpo-substrate.md`: from-scratch universal CPO as a
  version-agnostic hosting SUBSTRATE (Tier 1) + version-matched AUTHORSHIP (Tier 2)
  split by a narrow negotiated Environment Contract; per-boundary self-adapting
  mechanisms (mandatory version-gating at the operand-config port; discovery-gated
  writes at the guest-API port; convergence-gated steps at the protocol port);
  structural enforcement + a (CPO,guest)-pair conformance matrix as the tested
  definition of "universal". Grounded in the §4/§5/§7a findings.
