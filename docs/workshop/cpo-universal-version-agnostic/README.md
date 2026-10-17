# Workshop — Universal (version-agnostic) CPO

Design workshop exploring whether a single Control Plane Operator built from
HyperShift `main` can drive guest clusters whose component binaries and guest API come
from an older OCP release payload (a "version-agnostic CPO"), and what an ideal
from-scratch design would look like.

This is an exploratory design artifact, not a proposal or a committed plan.

## Contents

| File | What it is |
|------|------------|
| `WORKSHOP-NOTES.md` | Rolling session notes: what the CPO is, the skew-boundary model, both diff exercises (main vs 5.0, main vs 4.20), the invariant-core / distance-variable-shell conclusion. |
| `IDEA-1-universal-cpo-substrate.md` | The from-scratch design: a version-agnostic hosting Substrate (Tier 1) + version-matched Authorship (Tier 2) split by a negotiated Environment Contract, with three self-adapting outbound ports. |
| `IDEA-1-universal-cpo-architecture.html` | Standalone dark-themed SVG diagram of Idea 1 (open in a browser). |
| `IDEA-1-render.png` | Rendered PNG of the diagram. |
| `cpo-summary.txt` | In-depth read-only architecture analysis of CPO/HCCO/PKI at a pinned commit. |
| `diff-inventory/` | main vs `release-5.0` (1 minor of skew): rendering + behavioral category inventories (6 agents). |
| `diff-inventory-4.20/` | main vs `release-4.20` (~5 minors of skew): same inventories, framed as a delta vs the 5.0 findings. |

## Key findings (one-paragraph)

The skew surface has an **invariant core** (ungated config emission is a hazard at any
distance — e.g. `--tls-curve-preferences`; the framework has no version-floor safety
net; the HCP self-API is a false skew surface) and a **distance-variable shell** (which
guest-API shapes prune rotates with k8s minors; how much framework/PKI/protocol logic
has moved grows with distance). Version gates that read the live operand version
self-track and are correct at any distance, so the operand-config boundary is fixable
by making gating mandatory; the guest-API boundary does not self-track and needs
discovery-gated writes. The hazard set is a function of the specific (CPO, guest)
version pair, so "validate against one old guest" is insufficient.

## Grounding

Analysis performed read-only against `openshift/hypershift` branches at the commits
recorded in each document. One live experiment (main CPO on a 5.0 guest) is recorded
in `WORKSHOP-NOTES.md`.
