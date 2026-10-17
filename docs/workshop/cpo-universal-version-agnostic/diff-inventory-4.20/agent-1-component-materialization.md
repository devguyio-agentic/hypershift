# Agent-1 — Control-Plane Component Materialization Diff Inventory (4.20 DELTA)
**Region:** `control-plane-operator/controllers/hostedcontrolplane/v2/` (v2 component adapters + embedded assets + testdata fixtures)
**Diff semantics:** `git diff upstream/release-4.20..upstream/main` (two-dot; how main's tip differs from 4.20's tip)
**main tip inspected:** `220e0e73` (branch `upstream/main`)
**Baseline compared against:** `../diff-inventory/agent-1-component-materialization.md` (the release-5.0 run, 1 minor back)
**Mode:** read-only source/diff analysis, no compilation.

> **Skew model:** The CPO (built from main, targets 5.1) renders manifests/flags; operand **binaries** come from the guest **4.20** payload (~5 minors back). Any arg/env/config a main CPO emits that a **4.20-era** binary rejects is a skew hazard. Probes/resources/labels/securityContext are kubelet/management-evaluated → payload-agnostic (FINE) unless they target an endpoint/port the binary doesn't serve.
>
> **4.20-floor rule applied throughout:** For every C1 flag, I checked whether the emission is version-gated in the adapter `.go`. A gate `>=4.20` is a no-op for a 4.20 guest (4.20 satisfies it → flag still emitted). A gate `>=4.23`/`>=5.x` protects a 4.20 guest (flag suppressed). This gate-vs-4.20 distinction is the core output.

Shape: **345 changed files** in-region (vs 81 at 5.0); **40 component directories** touched (vs 21 material at 5.0). `assets/` alone: 87 non-test files. I did not enumerate every cosmetic diff; I prioritized C1 flag/env drift, component gain/loss, and probe changes, framed as DELTA vs 5.0.

---

## (a) Summary table — changed components × category (DELTA-focused)

| Component | C1 flag/arg/env | C2 comp-set | C3 dep-graph | C5 shape | C10 gating | Net skew @ 4.20 |
|---|---|---|---|---|---|---|
| machine_approver | ✅ `--tls-curve-preferences` | | | | ungated | **BREAKS** (same as 5.0) |
| ingress-operator | ✅ `--haproxy-image`,`--default-haproxy-version` | | | ✅ metrics port | ungated | **BREAKS? (NEW, UNCONFIRMED)** |
| cluster-storage-operator | ✅ `--config`,`--terminate-on-files`, `GCP_PD_DRIVER_CONTROL_PLANE_IMAGE` | ✅ GCP PD cfg | | ✅ serving-cert vol | | **BREAKS? (NEW, UNCONFIRMED)** |
| csi-snapshot-controller-operator | ✅ `--config`,`--terminate-on-files` | A controller-config | | ✅ serving-cert vol | | **BREAKS? (NEW, UNCONFIRMED)** |
| control-plane-pki-operator | ✅ `--config`,`--terminate-on-files` | A controller-config | | | | **BREAKS? (NEW, UNCONFIRMED)** |
| cluster-image-registry-operator | ✅ controller-config mount | A controller-config | | ✅ vol | | SILENT? (NEW) |
| cvo | ✅ `--tls-min-version`/`--tls-cipher-suites` (ungated) | | | | **ungated** | **BREAKS? (NEW, UNCONFIRMED)** |
| configoperator (HCCO) | ✅ `--controllers` += `user-ca-bundle`; `KUBE_FEATURE_WatchListClient` env | | | | | BREAKS? (same as 5.0, UNCONFIRMED) |
| kube_scheduler | ✅ `--tls-cert-file`/`--tls-private-key-file` (replaces `--cert-dir`); `--v` | A service+SM | | ✅ readinessProbe `/readyz:10259`, serving-cert vol | | **BREAKS? (NEW, UNCONFIRMED)** |
| kcm | ✅ `--allocate-node-cidrs=true/false`; `--v`; TLSArgs refactor | | | | gated on `AllocateNodeCIDRs` | FINE (standard flag) |
| oauth_apiserver | ✅ etcd/tls/audit flags moved to `.go` | | | ✅ `wait-for-etcd` initC | | FINE (flags pre-existed) |
| karpenter-operator | ✅ env rename `MY_*`→`NAMESPACE/POD_NAME`; Azure env; `WatchListClient` | ✅ | ✅ MonitorOperands | ✅ | Azure-gated | UNCONFIRMED (same as 5.0) |
| powervs-ccm | ✅ TLSArgs refactor; `--use-service-account-credentials` churn | | | | | FINE |
| olm (catalogs) | ✅ NO_PROXY drops `redhat-marketplace` | ✅ **redhat-marketplace-catalog REMOVED** | | | | FINE/SILENT |
| etcd | env `ETCD_METRICS`,`ETCD_LOG_LEVEL`; statefulset templatized | ✅ self-register SA/role | | ✅ POD_IP | | FINE/SILENT (same as 5.0) |
| kas | startupProbe `readyz:client`; bootstrap-container kubeconfig; GCP WIF | ✅ GCP sidecar, bootstrap initC | ✅ | ✅ probes | GCP-gated | FINE (non-GCP) (same as 5.0) |
| kube-controller-manager | (see kcm) | | | | | — |
| oauth-openshift | removed `NO_PROXY` env | | | | | FINE |
| ingution-server-proxy | | | | ✅ haproxy-config vol | | FINE |
| **endpoint-resolver** | | ✅ **NEW component** | | | | FINE (new, no 4.20 operand) |
| **metrics-proxy** | | ✅ **NEW component** | | | | FINE (new) |
| **aws-node-termination-handler** | | ✅ **NEW component** | | | | FINE (new) |
| **gcp-cloud-controller-manager** | | ✅ **NEW component** | | | GCP-gated | FINE (GCP-only, new) |
| **kube-storage-version-migrator** | | ✅ **NEW component** | | | | FINE (new) |
| nto/dnsoperator/autoscaler/cno/clusterpolicy/router/registryoperator | (refactor/test) | | | | | FINE |

**Net:** 40 component dirs changed. 5 brand-new components (C2). ~15 of the remainder are import/package refactors (`util`→`podspec`/`netutil`/`config`) that materialize no operand difference.

---

## (b) C1 — OPERAND-FLAG / ARG / ENV findings vs a 4.20 GUEST

### 1. machine_approver — `--tls-curve-preferences=<ids>`  ⚠️ **BREAKS (CONFIRMED, carried from 5.0)**
- File: `machine_approver/deployment.go` → `config.CurvePreferences(cpContext, profile)` → `--tls-curve-preferences=23,24,25,...`
- **Version-gate:** NONE (emitted whenever `len(curveIDs)>0`; `CurvePreferences` is new in `support/config/groups.go`, absent on 4.20).
- **SKEW @4.20: BREAKS** — the 4.20 cluster-machine-approver binary has no such flag → `unknown flag: --tls-curve-preferences` → crash-loop. Same proven exploit as 5.0; still a hazard at 4.20. (Not new; the 5.0 finding applies unchanged.)

### 2. ingress-operator — `--haproxy-image` / `--default-haproxy-version`  ⚠️ **BREAKS? (NEW at 4.20, UNCONFIRMED)**
- Files: `assets/ingress-operator/deployment.yaml` (static args), `ingressoperator/deployment.go` (adds env `HAPROXY_28_IMAGE`, `HAPROXY_32_IMAGE`, `DEFAULT_HAPROXY_VERSION=3.2`).
- Exact args: `--haproxy-image`, `2.8=$(HAPROXY_28_IMAGE)`, `--haproxy-image`, `3.2=$(HAPROXY_32_IMAGE)`, `--default-haproxy-version`, `$(DEFAULT_HAPROXY_VERSION)`.
- **Version-gate:** NONE — args are **static in the asset**; confirmed **absent in the 4.20 asset** (0 matches). Multi-haproxy-version selection is a 5.1-era feature.
- **SKEW @4.20: BREAKS-candidate** — if the 4.20 cluster-ingress-operator binary predates multi-version haproxy support, `--haproxy-image <ver>=<img>` / `--default-haproxy-version` are unknown flags → crash-loop. UNCONFIRMED (couldn't verify the 4.20 ingress-operator flag set in-region). **This is a NEW hazard not present at 5.0** (5.0 ingress-operator is new enough to accept these).

### 3. cluster-storage-operator / csi-snapshot-controller-operator / control-plane-pki-operator — `--config` + `--terminate-on-files`  ⚠️ **BREAKS? (NEW at 4.20, UNCONFIRMED)**
- Files: `assets/{cluster-storage-operator,csi-snapshot-controller-operator,control-plane-pki-operator}/deployment.yaml` (+ new `controller-config.yaml` assets).
- Exact args: `--config=/var/run/configmaps/.../config.yaml`, `--terminate-on-files=/var/run/configmaps/.../config.yaml` (+ `--terminate-on-files=<serving-cert tls.crt/tls.key>` on snapshot-controller).
- **Version-gate:** NONE — static in assets; **all confirmed absent in the 4.20 assets** (0 matches each). This is a new "config-file + hot-reload-on-change" operator contract.
- **SKEW @4.20: BREAKS-candidate** — if the 4.20 operand binaries (cluster-storage-operator, csi-snapshot-controller-operator, control-plane-pki-operator) don't implement `--config`/`--terminate-on-files`, they reject the unknown flags → crash-loop. These are library-go `operator`-pattern flags; a 4.20 binary *may* already support `--config` but `--terminate-on-files` is newer. UNCONFIRMED. **NEW hazard class at 4.20** (absent/not-flagged at 5.0; 5.0 binaries accept them).

### 4. cvo — `--tls-min-version` / `--tls-cipher-suites` (UNGATED)  ⚠️ **BREAKS? (NEW at 4.20, UNCONFIRMED)**
- File: `cvo/deployment.go` — `config.TLSArgs(...)` appended to the **CVO main container** (`ComponentName`), **no version gate**.
- **Version-gate:** NONE. CVO `.go` on 4.20 emitted no TLS args (confirmed: 0 matches for `TLSArgs`/`tls-min-version` in 4.20 `cvo/`).
- **SKEW @4.20: BREAKS-candidate** — the cluster-version-operator `start` binary from a 4.20 payload may not accept `--tls-min-version`/`--tls-cipher-suites`. If unsupported → unknown flag → crash. UNCONFIRMED (CVO flag set at 4.20 not verified in-region). **NEW vs 5.0** (the 5.0 run did not flag CVO; the TLSArgs wiring to CVO is a main-era addition and is ungated, unlike CCO which is gated ≥4.23).

### 5. configoperator / HCCO — `--controllers` += `user-ca-bundle`; env `KUBE_FEATURE_WatchListClient=false`  ⚠️ **BREAKS? (carried from 5.0, UNCONFIRMED)**
- File: `configoperator/deployment.go` (IBMCloud path): `--controllers=controller-manager-ca,resources,user-ca-bundle,inplaceupgrader,drainer,hcpstatus` (adds `user-ca-bundle`).
- Asset adds env `KUBE_FEATURE_WatchListClient: "false"` (feature-gate env, benign/ignored if unknown).
- **SKEW @4.20: BREAKS-potential** — a 4.20 HCCO without a registered `user-ca-bundle` controller rejects the explicit entry ("unknown controller"). Even more likely to break at 4.20 than 5.0 (5 minors of HCCO drift). UNCONFIRMED. (Same category as 5.0; severity rises at 4.20.)

### 6. kube_scheduler — `--tls-cert-file`/`--tls-private-key-file` replace `--cert-dir`  ⚠️ **BREAKS? (NEW at 4.20, UNCONFIRMED)**
- Files: `assets/kube-scheduler/deployment.yaml` — removes `--cert-dir=/var/run/kubernetes`, adds `--tls-cert-file=/etc/kubernetes/certs/tls.crt`, `--tls-private-key-file=/etc/kubernetes/certs/tls.key`; new serving-cert volume + readinessProbe `/readyz:10259`.
- **Version-gate:** NONE (static asset change).
- **SKEW @4.20: likely FINE, flagged UNCONFIRMED** — `--tls-cert-file`/`--tls-private-key-file` are long-standing kube-scheduler flags (both existed well before 4.20), so a 4.20 kube-scheduler accepts them. Risk is only behavioral (serving-cert path/probe wiring), not unknown-flag. The `--v` addition is a universal klog flag → FINE.

### 7. kcm — `--allocate-node-cidrs=true/false`  ✅ FINE
- File: `kcm/deployment.go` — now conditionally emits `--allocate-node-cidrs=true` (when `Networking.AllocateNodeCIDRs==Enabled`) else `=false`. 4.20 asset already carried `--allocate-node-cidrs=false` statically.
- **SKEW @4.20: FINE** — `--allocate-node-cidrs` is a core kube-controller-manager flag present in every build; only the value toggles. TLSArgs refactor emits the same `--tls-min-version`/`--tls-cipher-suites` kcm already emitted at 4.20 → FINE.

### 8. oauth_apiserver — etcd/tls/audit flags relocated into `.go`  ✅ FINE
- File: `oauth_apiserver/deployment.go` now assembles `--etcd-servers`, `--etcd-cafile/certfile/keyfile`, `--tls-min-version`, `--tls-cipher-suites`, `--audit-webhook-*`, `--accesstoken-inactivity-timeout`, `--api-audiences`. These **pre-existed** at 4.20 (2 matches in 4.20 `.go`; the rest were in the 4.20 asset). This is a template→code move, not new flags.
- **SKEW @4.20: FINE** — same flag strings, same openshift-oauth-apiserver binary contract.

### 9. powervs-cloud-controller-manager — TLSArgs refactor + `--use-service-account-credentials` churn  ✅ FINE
- `cloud_controller_manager/powervs/*`: `--use-service-account-credentials` toggling and `--tls-cipher-suites` now via `config.TLSArgs`. Flags unchanged; powervs CCM supports them. → FINE.

### 10. konnectivity_agent — `--agent-identifiers` (test-asserted)  ✅ FINE
- Only test code references appeared in the diff; the `--agent-identifiers` flag itself is long-standing. → FINE.

### 11. etcd — `ETCD_METRICS=extensive`, `ETCD_LOG_LEVEL`, statefulset templatization  ✅ FINE/SILENT
- Same as 5.0: long-standing etcd env vars. Statefulset `name`/`serviceName` templatization + `POD_IP` env are CPO-side rendering → FINE.

### 12. karpenter-operator — env rename `MY_NAMESPACE`→`NAMESPACE`, `MY_NAME`→`POD_NAME`; Azure env; `WatchListClient`  ⚠️ UNCONFIRMED (carried from 5.0)
- Same analysis as 5.0: `--namespace` flag name unchanged; risk is only if the 4.20 karpenter-operator binary reads identity from the old env names. Management-side operand. UNCONFIRMED.

**Total C1 (flag/arg/env) findings: 12** (machine_approver, ingress-operator, 3× operator-`--config`, cvo, HCCO, kube_scheduler, kcm, oauth_apiserver, powervs-ccm, karpenter-operator; `--v`/konnectivity rolled in as FINE).

---

## (c) DELTA section — newly-BREAKS at 4.20 that was FINE/absent at 5.0

| # | Finding | 5.0 verdict | 4.20 verdict | Why the delta |
|---|---|---|---|---|
| D1 | **ingress-operator `--haproxy-image` / `--default-haproxy-version`** | not flagged (5.0 binary accepts) | **BREAKS? (NEW)** | Multi-haproxy-version selection is 5.1-era; a 4.20 ingress-operator predates these flags. Static, ungated. |
| D2 | **`--config` + `--terminate-on-files`** on cluster-storage-operator, csi-snapshot-controller-operator, control-plane-pki-operator | not flagged | **BREAKS? (NEW class)** | New config-file+hot-reload operator contract; absent in 4.20 assets. `--terminate-on-files` especially likely unknown to a 4.20 binary. Ungated. |
| D3 | **cvo `--tls-min-version`/`--tls-cipher-suites` (ungated)** | not flagged | **BREAKS? (NEW)** | CVO TLSArgs wiring is main-era and **ungated** (contrast CCO which gates ≥4.23). A 4.20 CVO may reject the flags. |
| D4 | **HCCO `--controllers=…,user-ca-bundle,…`** | BREAKS? (UNCONFIRMED) | **BREAKS? (higher confidence)** | Same flag; 5 minors of HCCO controller-registry drift make a 4.20 HCCO more likely to lack `user-ca-bundle`. |
| D5 | **machine_approver `--tls-curve-preferences`** | BREAKS (proven) | **BREAKS (unchanged)** | Not new, but still the top confirmed hazard; carries over identically. |

**Findings that are NOT new (FINE at both 4.20 and 5.0):** all `--v` klog additions; kcm/scheduler/powervs/oauth_apiserver TLSArgs refactors (same flag strings, binaries support them at 4.20); kas GCP/startupProbe (GCP-gated / endpoints exist); etcd env; olm NO_PROXY trim.

---

## (d) C2 / C3 / C5 / C10 findings

### C2 — Component-set (NEW at 4.20, mostly absent from the 5.0 delta)
- **5 brand-new components** (dirs absent in 4.20): `endpoint-resolver`, `metrics-proxy`, `aws-node-termination-handler`, `gcp-cloud-controller-manager` (GCP-only), `kube-storage-version-migrator`. These render new manifests but there is **no corresponding 4.20 operand being reconfigured** — the CPO ships their images from the payload; if the 4.20 payload lacks the image the Deployment simply won't pull (ImagePullBackOff), not a flag-rejection. → FINE for skew purposes (new capability, not a reconfiguration of an existing 4.20 binary). *Note: these are a much larger C2 surface than 5.0, which only added the kas GCP sidecar.*
- **redhat-marketplace-catalog REMOVED** (`olm/catalogs/component.go`; assets `redhat-marketplace-catalog/{deployment,service}.yaml` deleted; NO_PROXY trimmed). → FINE/SILENT (dropping a catalog source).
- **kas:** new `kas-bootstrap-container` init container + `kas-bootstrap-container-kubeconfig` secret/RBAC (`system:kas-bootstrap-container`). Runs in the KAS/cli image. → FINE (new bootstrap wiring, CPO-managed).
- **etcd:** new self-register ServiceAccount + Role/RoleBindings (`etcd-self-register-*`). → CPO/management-side RBAC → FINE.
- **kas GCP WIF webhook sidecar** — same as 5.0, GCP-only.

### C3 — Dependency-graph / predicates
- `karpenteroperator/component.go`: `.MonitorOperandsRolloutStatus()`, Azure predicate, AWS-only credentials secret (same as 5.0).
- `kube_scheduler/component.go`: new `service.yaml` + `servicemonitor.yaml` manifests (metrics wiring).
- New-component wiring for the 5 C2 adds (predicates/platform gates). → CPO-side, FINE.

### C5 — Manifest shape (volumes / probes / resources / securityContext)
- **kube-apiserver:** new `startupProbe` (`readyz` on port `client`) + bootstrap-container kubeconfig volume — same as 5.0; `readyz`/`livez` endpoints exist in 4.20 KAS → FINE.
- **kube-scheduler:** new `readinessProbe` `/readyz:10259` + serving-cert volume (replaces `cert-work`) — `/readyz` served by 4.20 scheduler on secure port → FINE (probe); see C1 #6 for the cert-flag swap.
- **openshift-oauth-apiserver:** `wait-for-etcd` init container rewritten (bash/curl TLS health probe, securityContext hardening) — runs in `cli` image, no operand-binary contract → FINE (same as 5.0).
- **ingress-operator:** new `metrics` port `containerPort: 60000` → kubelet-side, FINE.
- Operator serving-cert/config volumes (storage, snapshot, pki, image-registry) → paired with the C1 `--config` flags; the volumes themselves are FINE, the flags are the hazard (see C1 #3).

### C10 — Feature-gate / capability / version gating (gate-vs-4.20 audit)
- **cloud_credential_operator:** TLS args gated `≥4.23 || ≥5.x` → **protects a 4.20 guest** (flag suppressed; 4.20 < 4.23). FINE. *(Contrast cvo, which does the same thing UNGATED — see D3.)*
- **capi_manager:** `--skip-crd-migration-phases` gated `version.GE(4.20)`. ⚠️ **4.20 FLOOR ALERT:** a 4.20 guest **satisfies** `GE(4.20)` → the flag **IS emitted** to a 4.20 capi binary. This was FINE at 5.0 (gate protects <4.20) but at exactly 4.20 the gate is a no-op. The 4.20 cluster-api binary is the one shipped with 4.20 and should support `--skip-crd-migration-phases` (the gate exists precisely because 4.20 introduced it), so **FINE** — but note the gate provides **zero** protection at the 4.20 floor.
- **kcm `--allocate-node-cidrs`:** gated on `Networking.AllocateNodeCIDRs` (not version) → core flag → FINE.
- **kas GCP WIF:** `EnableForPlatform(GCP)` → non-GCP unaffected.
- **karpenter Azure:** gated on `StandaloneKarpenterOperatorEnabled`.
- **cvo TLSArgs: UNGATED** → the key C10 gap (D3).

---

## (e) TOP skew hazards for a 4.20 guest (ranked)

1. **machine_approver `--tls-curve-preferences=<ids>`** — `machine_approver/deployment.go`, ungated via `config.CurvePreferences`. **BREAKS (CONFIRMED)**. 4.20 machine-approver rejects the flag → crash-loop. (Carried from 5.0.)
2. **operator `--config` / `--terminate-on-files`** on `cluster-storage-operator`, `csi-snapshot-controller-operator`, `control-plane-pki-operator` — static assets, ungated, **absent in 4.20 assets**. **BREAKS? (NEW, UNCONFIRMED)** — `--terminate-on-files` likely unknown to a 4.20 binary → crash-loop. Highest-value NEW hazard (3 operands).
3. **ingress-operator `--haproxy-image <ver>=<img>` / `--default-haproxy-version`** — `assets/ingress-operator/deployment.yaml`, ungated, absent in 4.20. **BREAKS? (NEW, UNCONFIRMED)** — multi-haproxy-version flags predate a 4.20 ingress-operator.
4. **cvo `--tls-min-version` / `--tls-cipher-suites` (UNGATED)** — `cvo/deployment.go`. **BREAKS? (NEW, UNCONFIRMED)** — unlike CCO's ≥4.23 gate, CVO's TLSArgs are emitted to a 4.20 CVO unconditionally.
5. **HCCO `--controllers=…,user-ca-bundle,…`** (IBMCloud) — `configoperator/deployment.go`. **BREAKS? (UNCONFIRMED, higher confidence at 4.20)** — 5-minor HCCO controller-registry drift makes rejection more likely than at 5.0.

> **4.20-floor note:** `capi_manager --skip-crd-migration-phases` is gated `GE(4.20)`, so the gate provides **no protection** at the 4.20 floor (flag still emitted) — benign because 4.20's own capi binary introduced the flag, but worth flagging as a gate that only protects *below* 4.20.
> All `--v` klog additions (kcm, scheduler, oapi, oauth, oauth_apiserver, ocm, kas) remain **FINE**. kcm/scheduler/powervs/oauth_apiserver TLSArgs *refactors* emit the same flag strings 4.20 already used → FINE.

### UNCONFIRMED items (could not verify operand binary flag contract in-region)
- Hazards #2, #3, #4, #5 above (need the 4.20 operand binaries' `--help` / flag sets).
- karpenter-operator env-rename identity contract (same as 5.0).
