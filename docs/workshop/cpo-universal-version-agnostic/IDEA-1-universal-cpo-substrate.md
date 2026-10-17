# Design Idea 1 — The Universal CPO: a Version-Agnostic Control-Plane Hosting Substrate

Status: workshop design exploration (one of potentially several ideas). Companion to
`WORKSHOP-NOTES.md`; grounded in the four diff passes (5.0 + 4.20, rendering +
behavioral) and the live experiment recorded there.

Framing: imagine designing a CPO from scratch whose axiom, from line one, is that it
must work across ANY OCP version. Not "how do we patch today's CPO" — what SHAPE does
the thing take when version-agnosticism is a design axiom.

---

## 0. The root cause, stated precisely

Every skew bug the investigation found reduces to one latent assumption:

> **controller-version == target-version**

That assumption was historically TRUE BY CONSTRUCTION, because the CPO shipped INSIDE
the OCP release payload — it was always the same version as the operands it configures
and the guest it writes to. The entire skew problem is the cost of breaking that
equality (shipping the CPO independently) while leaving the assumption baked into
thousands of code sites.

So "universal CPO" is not a feature to add. It is the REMOVAL of one assumption,
everywhere, replaced by: every version is an explicit input, and it is structurally
impossible to emit something a target cannot accept.

---

## 1. The one organizing principle

> **Version knowledge must live on the same side of the boundary as the versioned
> artifact.**

This single rule explains all four investigation findings and dictates the whole
architecture:

- Operand FLAGS are known correctly only by the operand (it IS that version) → the
  operand should own its config, not the CPO.
- Guest API SHAPES are known correctly only by the guest API server → discover them,
  do not assume them.
- Orchestration / infra / PKI / cross-cluster bridging carry NO target-version
  knowledge → they are already agnostic (the live run's 35/36 proved it) → they stay
  in the universal core.
- The HCP API is the one surface guaranteed same-version (HO-served, moves with the
  CPO) → it is the reliable channel, so USE it as the contract carrier.

---

## 2. Architecture: a Version Firewall splitting two tiers

The ideal CPO is not one program that is "careful about versions." It is TWO TIERS
separated by an explicit contract; the contract IS the firewall.

### Tier 1 — the Universal Substrate (ships with HyperShift, truly version-agnostic)

A control-plane HOSTING RUNTIME. It does exactly the work the investigation found is
already version-independent (this is the ~4/5 of today's CPO that already works across
versions):

- placement — run these workloads, topology, priority classes, PDBs
- identity — mint/rotate the full PKI tree, CAs, kubeconfigs, SA tokens
- datastore — provide an etcd endpoint + client creds (or an abstract KV)
- connectivity — konnectivity tunnel, guest API endpoint, the dialer
- cloud infra — security groups, LBs, private-link, KMS validation
- orchestration — dependency ordering, rollout aggregation, status, teardown
- the cross-cluster BRIDGE — mgmt↔guest state sync (the plumbing, not the payloads)

Mental model: the substrate is the kubelet/CRI of control planes — a stable runtime
interface that does not know or care what flags its workloads use.

### Tier 2 — Version-matched Authorship (ships with / versioned to the payload)

The part that is INTRINSICALLY version-coupled — "what flags does 5.1's KAS take,"
"what shape is a 1.34 guest's ServiceMonitor" — lives on the payload side, where it is
version-matched BY CONSTRUCTION. The operand configures ITSELF against the substrate's
primitives, exactly as in standalone OCP (where every operator is self-hosted and
version-matched).

### The firewall — a narrow, explicitly-versioned Environment Contract

The ONLY thing that crosses the tiers. It carries PRIMITIVES, not flags:
"here is your etcd endpoint, serving certs for these SANs, a kubeconfig to the guest,
konnectivity, your normalized config inputs; report your health here."

Properties:
- add-only / negotiated, like a protobuf schema or the OCI runtime spec
- versioned by CONTRACT version, not by OCP version → it changes SLOWLY

This is the crux: you replace a surface that churns every release (per-flag coupling)
with one that churns rarely (the primitive set).

### Why this dissolves the OCPSTRAT tension

You do not choose between "ships independently" and "version-safe." The substrate
ships independently AND is universal; authorship stays version-matched AND rides the
payload. The seam between them is exactly the version-coupling seam the diffs traced.

---

## 3. Boundary-by-boundary: the ideal mechanism for each

Decisive lesson from the gates: the gates that read `ReleaseImageProvider.Version()`
SELF-TRACK and are correct at every distance; the bugs are all absence-of-gate or
absence-of-discovery. So the ideal makes the correct mechanism the ONLY path.

### 3a. Operand-config port
North star: the operand owns its config (Tier-2). Where not achievable, the fallback
is VERSION-INDEXED DECLARATIVE DATA, never imperative branches — the config a 4.20 KAS
gets is a different DATA BUNDLE than a 5.1 KAS, keyed by version, centralized,
reviewable, testable.

The curve-4588 bug becomes impossible two ways: (a) the operand never asks for a curve
its own binary rejects, or (b) the curve lives in the ≥5.1 bundle only.

Structural choke point: there is ONE way to emit operand config, and it REQUIRES a
version constraint as a MANDATORY argument. You cannot construct a flag without
declaring the version window it applies to — a required constructor arg, not an
optional builder method. Omission is a COMPILE ERROR, not a silent runtime crash.

### 3b. Guest-API port
The guest boundary does NOT self-track today (HCCO writes at the vendored client's
apiVersion unconditionally). The ideal gives it the self-tracking it lacks using
machinery Kubernetes already ships: DISCOVERY + OpenAPI v3 + SERVER-SIDE APPLY.

Every guest write goes through ONE discovery-gated writer that resolves the highest
apiVersion the guest actually serves and drops fields the guest schema does not have.
This is FEATURE DETECTION, not version detection — the web platform's hard-won lesson
(detect capabilities, do not sniff the UA).

The prometheus-CRD-prune and OVN-IPv6 hazards vanish because you never write a shape
the guest cannot hold. One choke point: you physically cannot do an unconditional
guest write.

### 3c. Protocol port (the quiet stalls)
Reencryption, PKI rotation, inplace-upgrade, konnectivity trust — multi-step state
machines. Failure mode: a step that ASSUMES the other side did its part.

The ideal makes every step CONVERGENCE-GATED, IDEMPOTENT, and MONOTONIC: it advances
only on OBSERVED convergence of the real target, with a declared timeout and a
declared fallback — never "I wrote X, therefore Y happened." Protocol VERSION is
negotiated and recorded. This is expand-contract / migration discipline generalized;
reencryption already half-does it (it observes KAS convergence) — the ideal makes
"observe, don't assume" MANDATORY for all of them.

---

## 4. The meta-layer: enforcement + the conformance matrix

Two findings make these non-negotiable, not nice-to-haves.

### 4a. Structural enforcement over convention
The problem is BINARY (gated/ungated) and failures are sins of OMISSION. So the design
must make omission IMPOSSIBLE, not discouraged — mandatory version constraints at the
config port, structurally un-skippable discovery at the guest port. A developer who
forgets is the actual threat; defeat them with the type system, not a review checklist.

### 4b. The conformance matrix = the tested definition of "universal"
The hazard set is a function of the specific (CPO, guest) PAIR and is NON-MONOTONIC —
VAP-v1 was the #1 guest hazard at 1-minor and VANISHED by 5-minor, while
prometheus-prune APPEARED. This kills "validate against one old guest and call it
proven."

So the supported version window is a first-class, TESTED contract: a matrix that
renders every component's config and every guest write against EVERY supported guest
version and asserts no-unknown-flag / no-pruned-field / no-stalled-protocol — ideally
against the real old binaries and real old API schemas. The matrix IS the definition
of "universal."

Honest companion: declare a window [min, max], negotiate within it, and REFUSE —
loudly, at admission/preflight — for pairs outside it. Converting every silent skew
failure (prune, stall) into a loud early rejection is itself a feature, because the
investigation showed the quiet failures are the dangerous ones.

### 4c. The HCP API as the control channel
The one guaranteed same-version surface becomes the asset that ties it together: the
substrate records the resolved guest version, the negotiated contract version, and the
computed capability set into HCP status; every component reads from there. The "false
skew surface" turns into the reliable control channel.

---

## 5. Honest tradeoffs — what the ideal actually costs

- **Tier-2 authorship is a massive cross-team contract.** Making every payload
  operator self-configure against a substrate interface is an org-wide, multi-release
  negotiation with teams that do not report to HyperShift. The purist "operand owns its
  config" is the north star; the PRAGMATIC version is version-indexed declarative
  bundles owned by HyperShift — ~80% of the safety for ~20% of the political cost.
- **Discovery-gated guest writes add latency and a failure mode** (discovery can be
  stale/unavailable). Mitigable (cache in HCP status, refresh on guest version change),
  but real.
- **The conformance matrix is expensive to build and run.** It is the price of
  honesty; the evidence says there is no cheaper substitute.
- **A from-scratch two-tier split is not where the code is.**

### Incremental path (each step independently valuable, each closes a hazard class)
1. Make the config port MANDATORY-GATED; move existing scattered gates into it.
2. Wrap ALL guest writes in the discovery-gated writer.
3. Make protocols CONVERGENCE-GATED.
4. Stand up the conformance matrix.
5. THEN, over releases, migrate authorship toward Tier-2.

---

## 6. The one-sentence version

The ideal universal CPO is a VERSION-AGNOSTIC CONTROL-PLANE HOSTING SUBSTRATE that
provides primitives through a narrow, negotiated contract, while all version-coupled
authorship lives on the payload side where it is version-matched by construction — and
the two boundaries that must cross versions (operand config, guest API) are made
self-adapting by structural enforcement (mandatory version-gating) and feature
detection (discovery), with a conformance matrix as the tested definition of
"universal."

The quietly profound part: this is MOSTLY WHAT THE SYSTEM ALREADY DOES — the substrate
work is already agnostic, the gates already self-track, reencryption already observes
convergence. The ideal is not exotic. It is the existing good patterns made MANDATORY
and STRUCTURAL instead of present-where-someone-remembered, plus one genuinely new
thing (discovery-gated guest writes) for the one boundary that cannot self-track today.

---

## 7. Open sub-threads for this idea

- [ ] Make the Environment Contract PRIMITIVE SET concrete — the whole two-tier split
      stands or falls on whether this interface can actually be made narrow + stable.
      Enumerate: etcd endpoint+creds, serving-cert issuance (SANs in / cert out),
      guest kubeconfig, konnectivity dial, normalized config inputs, health-report
      sink. What else MUST cross? What must NOT?
- [ ] Config port: design the mandatory-version-constraint API so omission can't
      compile. Required constructor arg vs. typed per-version bundle registry.
- [ ] Guest port: single discovery-gated writer design (discovery + OpenAPI v3 + SSA);
      caching strategy; behavior when discovery is unavailable.
- [ ] Protocol port: a reusable convergence-gated step primitive (observe/timeout/
      fallback/record-version) that all state machines are refactored onto.
- [ ] Conformance matrix: can it run against REAL old binaries + REAL old API schemas
      cheaply (vendored CRD schemas? recorded discovery docs? containerized old
      operands)?
- [ ] Where is the min/max supported window declared and enforced (admission? preflight
      condition on HCP?), and how is an out-of-window pair rejected loudly?
