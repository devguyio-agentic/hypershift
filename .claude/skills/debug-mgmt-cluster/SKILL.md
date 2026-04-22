---
name: debug-mgmt-cluster
description: "Debug HyperShift management cluster issues causing CI test failures. Use when CI conformance jobs fail with DNS errors, konnectivity failures, mass test failures, liveness probe timeouts, or when multiple hosted clusters fail simultaneously on the same management cluster. Applies to aggregated and periodic hypershift conformance jobs."
---

# Debug HyperShift Management Cluster

Systematic methodology for diagnosing CI test failures caused by management cluster infrastructure issues — not product bugs. Developed from real-world RCA of OCPBUGS-82112.

## When to Use This Skill

Use when you see any of these in CI job logs:
- `lookup konnectivity-server-local on 172.30.0.10:53: no such host`
- `read udp ... ->172.30.0.10:53: i/o timeout`
- Mass test failures (>10 blocking failures per run)
- Multiple unrelated tests failing with `context deadline exceeded`
- Aggregated jobs where most/all underlying runs fail
- `503 Service Unavailable: error trying to reach service`

These symptoms often point to **management cluster DNS failure**, not a product bug. This skill walks you through proving it.

## Prerequisites

- `KUBECONFIG` for the management cluster (or `oc` access)
- Access to Prow job artifacts via GCS (HTTPS works without `gcloud`)
- The `ci:analyze-prow-job-test-failure` skill for initial triage

## Investigation Workflow

### Phase 1: Identify the Management Cluster

Every HyperShift CI job dumps artifacts including the hosted cluster's konnectivity-agent config. The `--proxy-server-host` flag reveals the management cluster domain.

```bash
# From job artifacts: find the management cluster
curl -sL "https://storage.googleapis.com/test-platform-results/<bucket-path>/artifacts/<job>/dump/artifacts/hostedcluster-<id>/namespaces/kube-system/pods/<konnectivity-agent-pod>/<pod>.yaml" \
  | grep -A1 "proxy-server-host"
# Output: konnectivity-server-<hcp-id>.apps.rosa.<mgmt-cluster>.openshiftapps.com
```

Confirm with: `oc whoami --show-console`

### Phase 2: Understand the Node Architecture

**This is the most important step.** Most management cluster issues trace to node sizing or scheduling problems.

```bash
# Node inventory with instance types, taints, and node pools
for node in $(oc get nodes -o jsonpath='{.items[*].metadata.name}'); do
  type=$(oc get node $node -o jsonpath='{.metadata.labels.node\.kubernetes\.io/instance-type}')
  pool=$(oc get node $node -o jsonpath='{.metadata.labels.hypershift\.openshift\.io/nodePool}')
  zone=$(oc get node $node -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}')
  taints=$(oc get node $node -o jsonpath='{.spec.taints[*].key}')
  echo "$node ($type, $pool, $zone): ${taints:-<none>}"
done
```

**What to look for:**
- Are there two classes of nodes? (e.g., small untainted + large tainted)
- Which nodes can infra pods (Prometheus, CoreDNS, router) schedule on?
- Which nodes can HCP pods schedule on?
- Can HCP pods schedule on infra nodes? (taint only blocks one direction)

```bash
# Check what Prometheus and CoreDNS can tolerate
oc get pod prometheus-k8s-0 -n openshift-monitoring -o jsonpath='{.spec.tolerations}' | python3 -m json.tool
oc get pod <dns-pod> -n openshift-dns -o jsonpath='{.spec.tolerations}' | python3 -m json.tool
```

### Phase 3: Check DNS Health

```bash
# DNS pod placement and restarts
oc get pods -n openshift-dns -o wide

# DNS query rate by response code
THANOS="https://thanos-querier-openshift-monitoring.$(oc get ingresses.config/cluster -o jsonpath='{.spec.domain}')"
TOKEN=$(oc whoami -t)
curl -sk -H "Authorization: Bearer $TOKEN" "${THANOS}/api/v1/query" \
  --data-urlencode 'query=sum by (rcode)(rate(coredns_dns_responses_total[5m]))'
```

**What to look for:**
- High NXDOMAIN rate (>70% of queries) — normal for `ndots:5` but reduces margin
- How many DNS pods? If only 3, losing 1 is a 33% capacity drop
- Any DNS pods with high restart counts?

### Phase 4: Check Memory Pressure During CI Window

This is where you prove whether the failure is memory-related.

```bash
# Memory available on all nodes during the CI job window
curl -sk -H "Authorization: Bearer $TOKEN" "${THANOS}/api/v1/query_range" \
  --data-urlencode 'query=node_memory_MemAvailable_bytes / 1024 / 1024 / 1024' \
  --data-urlencode "start=<job-start-time>" \
  --data-urlencode "end=<job-end-time>" \
  --data-urlencode 'step=300'

# Memory requests vs allocatable per node at a specific time
curl -sk -H "Authorization: Bearer $TOKEN" "${THANOS}/api/v1/query" \
  --data-urlencode 'query=sum by (node)(kube_pod_container_resource_requests{resource="memory"}) / 1024 / 1024 / 1024' \
  --data-urlencode "time=<failure-time>"
```

**Critical:** `MemAvailable` is misleading. Check `MemFree` too:

```bash
# Actual free pages vs reclaimable cache
curl -sk -H "Authorization: Bearer $TOKEN" "${THANOS}/api/v1/query" \
  --data-urlencode 'query={__name__=~"node_memory_MemFree_bytes|node_memory_MemAvailable_bytes|node_memory_Cached_bytes",instance=~".*<node-ip>.*"}' \
  --data-urlencode "time=<failure-time>"
```

If `MemFree` is under ~1 GB while `MemAvailable` shows 4+ GB, the kernel is in direct reclaim — processes freeze on every memory allocation.

### Phase 5: Correlate Liveness Probe Failures with Memory

```bash
# DNS liveness probe failures at 1-minute resolution
curl -sk -H "Authorization: Bearer $TOKEN" "${THANOS}/api/v1/query_range" \
  --data-urlencode 'query=rate(prober_probe_total{result="failed", pod=~"dns-default.*", probe_type="Liveness"}[1m]) > 0' \
  --data-urlencode "start=<window-start>" \
  --data-urlencode "end=<window-end>" \
  --data-urlencode 'step=60'

# How many DNS pods were up at each minute?
curl -sk -H "Authorization: Bearer $TOKEN" "${THANOS}/api/v1/query_range" \
  --data-urlencode 'query=count(up{job="dns-default", namespace="openshift-dns"} == 1)' \
  --data-urlencode "start=<window-start>" \
  --data-urlencode "end=<window-end>" \
  --data-urlencode 'step=60'
```

**What to look for:**
- Does the DNS pod that fails always live on the node with lowest memory?
- Do multiple DNS pods fail simultaneously? (thundering herd from same CI batch)
- Do other pods on the same node also fail probes at the same time? (node-level, not pod-level)

```bash
# Check if ALL pods on a node fail probes simultaneously (proves node-level issue)
# Use kubelet logs:
oc debug node/<node-name> -- chroot /host \
  journalctl -u kubelet --since "<time>" --until "<time+1min>" \
  | grep -E "Probe failed.*context deadline|Probe failed.*timed out"
```

If every pod on the node fails probes within a 5-second window, it's the kubelet/kernel, not the individual pods.

### Phase 6: Kernel Evidence

```bash
# vmstat counters — look for allocstall (direct reclaim stalls)
oc debug node/<node-name> -- chroot /host bash -c '
  grep -E "allocstall|pgsteal_direct|pgscan_direct|pgmajfault|compact_stall" /proc/vmstat
'

# dmesg — OVS drops, journald failures, OOM kills
oc debug node/<node-name> -- chroot /host \
  dmesg --time-format iso | grep -iE "oom|openvswitch|deferred|journald.*Failed"
```

**Key counters:**
- `allocstall_*` > 0: processes were frozen waiting for memory
- `pgsteal_direct` high: pages reclaimed synchronously (blocking callers)
- No OOM kills but high allocstall = the worst case: everything slow, nothing killed

### Phase 7: Cross-Verify Across Jobs

The root cause is confirmed when multiple jobs on the same management cluster show the same pattern, regardless of OCP version.

```bash
# Check multiple job build logs for DNS errors
for RUN_ID in <id1> <id2> <id3>; do
  count=$(curl -sL "https://storage.googleapis.com/test-platform-results/logs/<job-name>/${RUN_ID}/build-log.txt" \
    | grep -c "konnectivity-server-local\|no such host\|i/o timeout")
  echo "Run ${RUN_ID}: ${count} DNS errors"
done
```

If different OCP versions, different test suites, but the same DNS errors on the same management cluster → it's infrastructure, not product.

## Common Root Causes

### 1. Undersized Infra Nodes + HCP Spillover

**Pattern:** Small untainted nodes run all infra (Prometheus 8-15 GB) AND receive HCP pods because taints only block one direction.

**Evidence:** Memory requests near allocatable, MemFree < 1 GB, all probes fail simultaneously.

**Fix:** Upsize the untainted pool, or prevent HCP pods from scheduling there.

### 2. Insufficient DNS Capacity

**Pattern:** 3 DNS pods for 10+ hosted clusters. Losing 1 = 33% capacity drop under 1,500+ qps.

**Evidence:** `count(up{job="dns-default"} == 1)` drops below 3 during failures.

**Fix:** More DNS pods, or reduce `ndots` to lower NXDOMAIN volume.

### 3. Thundering Herd

**Pattern:** CI batch creates 10+ hosted clusters at once, hitting all infra nodes simultaneously.

**Evidence:** Multiple DNS pods fail probes within minutes of each other, driven by the same CI batch.

**Fix:** Rate-limit hosted cluster creation, or size nodes for peak concurrent load.

## Key Metrics Reference

| Metric | Query | What It Tells You |
|--------|-------|-------------------|
| Node memory | `node_memory_MemAvailable_bytes` | Overall headroom (but misleading — check MemFree too) |
| Actual free pages | `node_memory_MemFree_bytes` | Real free memory — under 1 GB means direct reclaim |
| Memory requests | `kube_pod_container_resource_requests{resource="memory"}` | How much the scheduler committed |
| DNS probe failures | `prober_probe_total{result="failed", pod=~"dns-default.*"}` | When and where DNS probes fail |
| DNS pod availability | `up{job="dns-default"}` | How many DNS pods are serving |
| DNS query rate | `coredns_dns_responses_total` by rcode | Load and NXDOMAIN ratio |
| Pod count per node | `kubelet_running_pods` | Scheduling density |

## Anti-Patterns: What It's NOT

Before blaming the product, rule out management cluster infrastructure:

- **"konnectivity-server-local: no such host" is NOT a konnectivity bug.** The Service exists. DNS resolution failed.
- **"etcd leader changes too often" is NOT an etcd bug.** etcd raft consensus stalls when the node hosting etcd can't allocate memory for network buffers.
- **Different test failures across runs is NOT test flakiness.** If the failing tests are random but all use `kubectl exec`, the common denominator is the konnectivity tunnel, which depends on DNS.
- **One "sick node" is NOT a hardware problem.** If the failure rotates across nodes on different nights, it's scheduling/sizing, not hardware.

## Reference

- [RCA Report: OCPBUGS-82112](../../../team/projects/hypershift/reports/2026-04-21-ocpbugs-82112-mass-ci-failures.md)
- [debug-cluster skill](../debug-cluster/SKILL.md) — for hosted cluster issues (not management cluster)
- `ci:analyze-prow-job-test-failure` — for initial test failure triage
