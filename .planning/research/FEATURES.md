# Feature Research — cpg v1.3 (Cluster Health Surfacing)

**Domain:** Drop-reason classification and cluster health reporting in a policy-from-traffic CLI
**Researched:** 2026-04-26
**Confidence:** HIGH on Cilium drop reason enum (direct proto + source); HIGH on bucket taxonomy (justified per-reason below); MEDIUM on cluster-health.json schema (design choice informed by analogues); LOW on competitor drop-filtering behavior (limited public documentation)

> **Scope reminder.** v1.3 ships **cluster-health surfacing only**. OpenMetrics/Prometheus export,
> semantic policy intersection, `cpg apply`, policy consolidation, and L7-FUT-* are explicitly
> **out** (deferred per `.planning/PROJECT.md`). This document supersedes the v1.2 FEATURES.md
> for the current milestone.

---

## Trigger (Production Bug)

`mmtro-adserver` ingress drop with `drop_reason_desc = CT_MAP_INSERTION_FAILED` (Cilium conntrack
map full — infra issue) caused cpg to generate a useless `cpg-mmtro-adserver` CNP. The bug class:
**cpg trusts every Hubble DROPPED verdict as a policy-fixable event**, which is wrong.

Approximately 15–20% of Cilium drop reasons are infra/datapath failures that no CNP can fix.
Generating policies for them is actively harmful — the policy is never applied usefully and hides
the real cluster problem from the operator.

---

## 1. Drop Reason Taxonomy — CANONICAL CLASSIFICATION TABLE

**Source:** `api/v1/flow/flow.proto` (DropReason enum) + `pkg/monitor/api/drop.go` (string names)
from `github.com/cilium/cilium` main branch, verified 2026-04-26.

**Bucket definitions:**

- **POLICY** — The drop is a direct consequence of an absent or misconfigured CiliumNetworkPolicy.
  Adding or correcting a CNP *will* fix the drop. cpg MUST generate a policy.
- **INFRA** — The drop is a datapath, map, routing, encryption, or service-mesh infrastructure
  failure. No CNP can fix it. cpg MUST NOT generate a policy; the SRE needs cluster-level action.
- **TRANSIENT** — The drop is expected during startup/teardown races or normal Cilium datapath
  operation (identity allocation lag, CT state transitions). cpg SHOULD NOT generate a policy;
  the drop typically resolves without operator action.
- **NOISE** — Internal Cilium datapath bookkeeping events that surface as DROPPED in Hubble but
  are not errors. cpg MUST ignore them entirely.
- **SCOPED** — Requires a CiliumClusterwideNetworkPolicy (reserved identities). cpg already
  detects and warns via `isActionableReserved`; these are not policy-fixable by cpg (namespace-
  scoped CNP only). Map to infra for health reporting; keep existing warn path.

| Enum Constant (flow.proto) | Code | Human-readable (drop.go) | Bucket | Rationale |
|----------------------------|------|--------------------------|--------|-----------|
| `DROP_REASON_UNKNOWN` | 0 | unknown | TRANSIENT | No signal; treat as transient, do not generate policy |
| `INVALID_SOURCE_MAC` | 130 | Invalid source mac | INFRA | Layer 2 hardware/overlay misconfiguration; CNP cannot fix |
| `INVALID_DESTINATION_MAC` | 131 | Invalid destination mac | INFRA | Layer 2 hardware/overlay misconfiguration; CNP cannot fix |
| `INVALID_SOURCE_IP` | 132 | Invalid source ip | INFRA | Spoofed or misconfigured source; datapath enforcement |
| `POLICY_DENIED` | 133 | Policy denied | **POLICY** | Primary signal: L3/L4 deny due to absent allow rule |
| `INVALID_PACKET_DROPPED` | 134 | Invalid packet | INFRA | Malformed packet; datapath protection |
| `CT_TRUNCATED_OR_INVALID_HEADER` | 135 | CT: Truncated or invalid header | INFRA | Conntrack BPF map corruption or malformed TCP |
| `CT_MISSING_TCP_ACK_FLAG` | 136 | Fragmentation needed | INFRA | TCP state machine issue; not policy |
| `CT_UNKNOWN_L4_PROTOCOL` | 137 | CT: Unknown L4 protocol | INFRA | Unknown L4 in conntrack; datapath gap |
| `CT_CANNOT_CREATE_ENTRY_FROM_PACKET` | 138 | _(deprecated)_ | INFRA | CT map write failure; deprecated but keep bucket |
| `UNSUPPORTED_L3_PROTOCOL` | 139 | Unsupported L3 protocol | INFRA | Non-IP traffic; datapath does not support |
| `MISSED_TAIL_CALL` | 140 | Missed tail call | INFRA | BPF tail-call table miss; kernel/cilium version mismatch |
| `ERROR_WRITING_TO_PACKET` | 141 | Error writing to packet | INFRA | BPF packet write failure; datapath bug |
| `UNKNOWN_L4_PROTOCOL` | 142 | Unknown L4 protocol | INFRA | Unrecognized L4 in policy engine |
| `UNKNOWN_ICMPV4_CODE` | 143 | Unknown ICMPv4 code | INFRA | Unexpected ICMP variant; datapath gap |
| `UNKNOWN_ICMPV4_TYPE` | 144 | Unknown ICMPv4 type | INFRA | Unexpected ICMP variant; datapath gap |
| `UNKNOWN_ICMPV6_CODE` | 145 | Unknown ICMPv6 code | INFRA | Unexpected ICMPv6 variant |
| `UNKNOWN_ICMPV6_TYPE` | 146 | Unknown ICMPv6 type | INFRA | Unexpected ICMPv6 variant |
| `ERROR_RETRIEVING_TUNNEL_KEY` | 147 | Error retrieving tunnel key | INFRA | Tunnel/overlay metadata failure |
| `ERROR_RETRIEVING_TUNNEL_OPTIONS` | 148 | _(deprecated)_ | INFRA | Tunnel option lookup failure |
| `INVALID_GENEVE_OPTION` | 149 | _(deprecated)_ | INFRA | Geneve overlay misconfiguration |
| `UNKNOWN_L3_TARGET_ADDRESS` | 150 | Unknown L3 target address | INFRA | Next-hop resolution failure; routing issue |
| `STALE_OR_UNROUTABLE_IP` | 151 | Stale or unroutable IP | TRANSIENT | Pod restart / IP reuse lag; resolves when CT entries age out |
| `NO_MATCHING_LOCAL_CONTAINER_FOUND` | 152 | _(deprecated)_ | TRANSIENT | Pre-endpoint-ID-table era; legacy |
| `ERROR_WHILE_CORRECTING_L3_CHECKSUM` | 153 | Error while correcting L3 checksum | INFRA | Hardware offload / BPF checksum bug |
| `ERROR_WHILE_CORRECTING_L4_CHECKSUM` | 154 | Error while correcting L4 checksum | INFRA | Hardware offload / BPF checksum bug |
| `CT_MAP_INSERTION_FAILED` | 155 | CT: Map insertion failed | **INFRA** | **The triggering prod bug.** Conntrack BPF map full; fix: raise `bpf-ct-global-tcp-max` / lower GC interval. CNP cannot fix. |
| `INVALID_IPV6_EXTENSION_HEADER` | 156 | Invalid IPv6 extension header | INFRA | Unsupported IPv6 extension; datapath gap |
| `IP_FRAGMENTATION_NOT_SUPPORTED` | 157 | IP fragmentation not supported | INFRA | Fragmented packets; MTU or overlay config |
| `SERVICE_BACKEND_NOT_FOUND` | 158 | Service backend not found | INFRA | Cilium kube-proxy LB map stale; re-create backends or check EndpointSlice sync |
| `NO_TUNNEL_OR_ENCAPSULATION_ENDPOINT` | 160 | No tunnel/encapsulation endpoint (datapath BUG!) | INFRA | Overlay routing gap; CNP cannot fix |
| `FAILED_TO_INSERT_INTO_PROXYMAP` | 161 | NAT 46/64 not enabled | INFRA | NAT46/64 feature disabled; cluster config |
| `REACHED_EDT_RATE_LIMITING_DROP_HORIZON` | 162 | Reached EDT rate-limiting drop horizon | INFRA | BPF bandwidth manager rate limit hit; tune `bandwidth-manager` or check NIC limits |
| `UNKNOWN_CONNECTION_TRACKING_STATE` | 163 | Unknown connection tracking state | INFRA | CT state machine inconsistency; Cilium agent restart may help |
| `LOCAL_HOST_IS_UNREACHABLE` | 164 | Local host is unreachable | INFRA | Node-level routing gap |
| `NO_CONFIGURATION_AVAILABLE_TO_PERFORM_POLICY_DECISION` | 165 | No configuration available for policy decision | **TRANSIENT** | Endpoint not yet fully programmed; normal during pod startup race. Resolves without action within seconds. |
| `UNSUPPORTED_L2_PROTOCOL` | 166 | Unsupported L2 protocol | INFRA | Non-Ethernet L2; datapath gap |
| `NO_MAPPING_FOR_NAT_MASQUERADE` | 167 | No mapping for NAT masquerade | INFRA | SNAT table miss; NAT config issue |
| `UNSUPPORTED_PROTOCOL_FOR_NAT_MASQUERADE` | 168 | Unsupported protocol for NAT masquerade | INFRA | Protocol not supported by SNAT engine |
| `FIB_LOOKUP_FAILED` | 169 | FIB lookup failed | INFRA | Missing kernel route / ARP neighbor; routing misconfiguration |
| `ENCAPSULATION_TRAFFIC_IS_PROHIBITED` | 170 | Encapsulation traffic is prohibited | INFRA | Tunnel-in-tunnel blocked; overlay config |
| `INVALID_IDENTITY` | 171 | Invalid identity | TRANSIENT | Identity not yet allocated during pod startup; resolves when kvstore propagates. Also seen on Egress Gateway misconfiguration — see remediation. |
| `UNKNOWN_SENDER` | 172 | Unknown sender | TRANSIENT | Source identity not yet known to this node; propagation lag |
| `NAT_NOT_NEEDED` | 173 | NAT not needed | NOISE | Internal Cilium bookkeeping; not an error |
| `IS_A_CLUSTERIP` | 174 | Is a ClusterIP | NOISE | Expected datapath short-circuit for ClusterIP traffic |
| `FIRST_LOGICAL_DATAGRAM_FRAGMENT_NOT_FOUND` | 175 | First logical datagram fragment not found | INFRA | IP fragment reassembly failure |
| `FORBIDDEN_ICMPV6_MESSAGE` | 176 | Forbidden ICMPv6 message | INFRA | ICMPv6 type blocked by datapath policy |
| `DENIED_BY_LB_SRC_RANGE_CHECK` | 177 | Denied by LB src range check | **POLICY** | LoadBalancer `spec.loadBalancerSourceRanges` intentional deny — IS a policy-fixable event: operator must add source CIDR to Service. Not a CNP but a Service field. cpg cannot auto-fix but SHOULD surface it. |
| `SOCKET_LOOKUP_FAILED` | 178 | Socket lookup failed | INFRA | BPF socket-LB table miss |
| `SOCKET_ASSIGN_FAILED` | 179 | Socket assign failed | INFRA | BPF socket assignment error |
| `PROXY_REDIRECTION_NOT_SUPPORTED_FOR_PROTOCOL` | 180 | Proxy redirection not supported for protocol | INFRA | Protocol not interceptable by Envoy proxy |
| `POLICY_DENY` | 181 | Policy denied by denylist | **POLICY** | Explicit `denylist` rule in CNP hit; separate from POLICY_DENIED (133). Both are policy-fixable (review/remove the deny rule). |
| `VLAN_FILTERED` | 182 | VLAN traffic disallowed by VLAN filter | INFRA | VLAN filter config; not CNP |
| `INVALID_VNI` | 183 | Incorrect VNI from VTEP | INFRA | VXLAN overlay misconfiguration |
| `INVALID_TC_BUFFER` | 184 | Failed to update or lookup TC buffer | INFRA | TC BPF map failure |
| `NO_SID` | 185 | No SID was found for the IP address | INFRA | SRv6 segment ID missing; SRv6 config issue |
| `MISSING_SRV6_STATE` | 186 | _(deprecated)_ | INFRA | SRv6 state missing |
| `NAT46` | 187 | L3 translation from IPv4 to IPv6 failed (NAT46) | INFRA | NAT46 translation failure; NAT config |
| `NAT64` | 188 | L3 translation from IPv6 to IPv4 failed (NAT64) | INFRA | NAT64 translation failure; NAT config |
| `AUTH_REQUIRED` | 189 | Authentication required | **POLICY** | Mutual authentication (SPIFFE/SPIRE) required but not established. Policy intent: add `authentication.mode: required` CNP, OR it may indicate mTLS infra not provisioned. Classify as POLICY because the trigger is a policy `require authentication` directive, but flag for human review — could be infra if SPIRE is misconfigured. |
| `CT_NO_MAP_FOUND` | 190 | No conntrack map found | INFRA | CT BPF map completely absent; severe Cilium agent issue |
| `SNAT_NO_MAP_FOUND` | 191 | No nat map found | INFRA | NAT BPF map absent; severe Cilium agent issue |
| `INVALID_CLUSTER_ID` | 192 | Invalid ClusterID | INFRA | ClusterMesh misconfiguration |
| `UNSUPPORTED_PROTOCOL_FOR_DSR_ENCAP` | 193 | Unsupported packet protocol for DSR encapsulation | INFRA | DSR encap config issue |
| `NO_EGRESS_GATEWAY` | 194 | No egress gateway found | INFRA | Egress gateway policy matched but no gateway node; EgressGatewayPolicy misconfiguration |
| `UNENCRYPTED_TRAFFIC` | 195 | Traffic is unencrypted | INFRA | WireGuard strict mode: unencrypted traffic blocked. Fix: verify encryption is enabled on all nodes. |
| `TTL_EXCEEDED` | 196 | TTL exceeded | TRANSIENT | Normal network behavior; routing loop detection |
| `NO_NODE_ID` | 197 | No node ID found | INFRA | Node identity not yet allocated; severe init issue |
| `DROP_RATE_LIMITED` | 198 | Rate limited | INFRA | API rate limiting in cilium-agent; tune `--api-rate-limit` |
| `IGMP_HANDLED` | 199 | IGMP handled | NOISE | IGMP multicast join/leave; expected datapath event |
| `IGMP_SUBSCRIBED` | 200 | IGMP subscribed | NOISE | IGMP subscription; expected |
| `MULTICAST_HANDLED` | 201 | Multicast handled | NOISE | Multicast handled internally; not an error |
| `DROP_HOST_NOT_READY` | 202 | Host datapath not ready | **TRANSIENT** | Cilium agent starting up; drops during node init. Resolves without action. Flag if sustained (>60s after agent ready). |
| `DROP_EP_NOT_READY` | 203 | Endpoint policy program not available | **TRANSIENT** | Pod endpoint being programmed (common on new pod start). Resolves within seconds. Flag if sustained. |
| `DROP_NO_EGRESS_IP` | 204 | No Egress IP configured | INFRA | EgressGateway policy: no IP assigned to gateway interface; check EgressGatewayPolicy |
| `DROP_PUNT_PROXY` | 205 | Punt to proxy | NOISE | Traffic redirected to Envoy proxy; this is a redirect, not a drop error |

### Bucket Summary Counts (approx. from table above)

| Bucket | Count | Examples |
|--------|-------|---------|
| POLICY | 4 | POLICY_DENIED, POLICY_DENY, AUTH_REQUIRED, DENIED_BY_LB_SRC_RANGE_CHECK |
| INFRA | ~50 | CT_MAP_INSERTION_FAILED, FIB_LOOKUP_FAILED, SERVICE_BACKEND_NOT_FOUND, UNENCRYPTED_TRAFFIC |
| TRANSIENT | ~8 | DROP_HOST_NOT_READY, DROP_EP_NOT_READY, NO_CONFIGURATION_AVAILABLE, INVALID_IDENTITY, UNKNOWN_SENDER, STALE_OR_UNROUTABLE_IP, TTL_EXCEEDED, DROP_REASON_UNKNOWN |
| NOISE | 5 | NAT_NOT_NEEDED, IS_A_CLUSTERIP, IGMP_HANDLED, IGMP_SUBSCRIBED, MULTICAST_HANDLED, DROP_PUNT_PROXY |

### Edge Cases and Ambiguities

**AUTH_REQUIRED (189):** Could be POLICY (operator intended mTLS, policy is correct, just
authentication infrastructure not set up) or INFRA (SPIRE agent down, certificates expired).
Recommendation: classify as POLICY with a special `needs_review: true` flag in the health JSON,
and include a remediation hint for both paths.

**DENIED_BY_LB_SRC_RANGE_CHECK (177):** This is a real intentional policy block, but it is a
Kubernetes Service field (`spec.loadBalancerSourceRanges`), not a CiliumNetworkPolicy. cpg cannot
generate a fix. Classify as POLICY for health reporting (operator action needed), but suppress
CNP generation with a distinct hint: "Fix: add source CIDR to Service.spec.loadBalancerSourceRanges".

**INVALID_IDENTITY (171) and UNKNOWN_SENDER (172):** Transient under normal conditions (identity
propagation lag, startup). Infra indicator if sustained at high volume on stable pods. Recommendation:
classify as TRANSIENT; health JSON should include count + a time-window check hint.

---

## 2. How Comparable Tools Handle Non-Policy Drops

Research confidence: LOW (limited public docs; most tools are closed-source or do not expose filtering logic).

### Inspektor Gadget `advise networkpolicy`
- Captures TCP/UDP traffic via eBPF tracepoints on `connect()` / `accept()` syscalls, NOT on Cilium drop events.
- **Does not see Cilium drop reasons at all.** Generates policies from observed allowed connections, not from drops.
- Result: zero exposure to the infra-vs-policy problem. Different data model entirely.
- Source: [inspektor-gadget.io/docs advise_networkpolicy](https://inspektor-gadget.io/docs/main/gadgets/advise_networkpolicy/) — observes activity, not drops.

### Otterize Network Mapper
- Similarly flow-based (not drop-based): maps what IS connected, then recommends allow rules.
- No drop-reason classification needed — it never consumes DROP verdicts.
- Source: [github.com/otterize/network-mapper](https://github.com/otterize/network-mapper)

### Calico / Tigera `calicoctl`
- No public `policy recommend` feature in OSS calicoctl (only in Tigera Enterprise via Flow Visualization UI).
- Tigera Enterprise "Policy Recommendation Engine" (closed-source) is described as operating on
  flow logs from the Calico node agent. No public documentation on drop classification.
- **Conclusion:** No useful precedent from Calico OSS for drop-reason classification.

### Key Insight from Competitors
**All open-source generators work from allowed flows, not from drops.** cpg is unusual in
consuming DROP verdicts directly from Hubble. This means cpg uniquely owns the infra-vs-policy
classification problem — there is no industry-standard approach to copy.

The general pattern for noisy-signal generators:
1. **Source selection gate**: filter at source (only consume events that are unambiguously
   policy-fixable). Inspektor Gadget does this by watching connections, not drops.
2. **Post-capture labeling**: label events by root cause category before aggregating.
   Terraform does this: `exit 0` (no change), `exit 2` (changes = actionable), `exit 1` (error).
3. **Operator-override escape hatch**: `--ignore-X` flags for events where the tool's classification
   is wrong for their environment. Pattern: `--ignore-protocol` (already shipped as PA5).

cpg v1.3 should implement all three: (1) taxonomy-based gate in aggregator, (2) labels on health
JSON entries, (3) `--ignore-drop-reason` flag.

---

## 3. cluster-health.json Schema

### Design Principles

- **Structured for both human reading and programmatic consumption.** Not a log file.
- **Granularity: reason × node × workload** (PROJECT.md spec). All three dimensions present.
- **Remediation hints are doc links, not prose.** Deep links to Cilium docs pages.
- **Schema version pinned.** Same discipline as `evidence/schema.go`.
- **No OpenMetrics/Prometheus in v1.3.** The file IS the export; Prometheus deferred.

### Concrete Schema Sketch

```json
{
  "schema_version": 1,
  "generated_at": "2026-04-26T14:32:00Z",
  "session_id": "2026-04-26T14:30:00Z-a3f1",
  "cpg_version": "1.3.0",
  "summary": {
    "total_infra_drops": 412,
    "total_transient_drops": 87,
    "total_noise_drops": 23,
    "total_policy_drops": 1204,
    "distinct_infra_reasons": 3,
    "distinct_infra_nodes": 2,
    "distinct_infra_workloads": 5
  },
  "infra_drops": [
    {
      "reason": "CT_MAP_INSERTION_FAILED",
      "bucket": "infra",
      "count": 341,
      "first_seen": "2026-04-26T14:30:05Z",
      "last_seen":  "2026-04-26T14:31:58Z",
      "severity": "critical",
      "nodes": [
        {"node": "node-a.example.com", "count": 310},
        {"node": "node-b.example.com", "count": 31}
      ],
      "workloads": [
        {"namespace": "mmtro", "workload": "adserver", "count": 205},
        {"namespace": "mmtro", "workload": "tracker", "count": 136}
      ],
      "remediation": {
        "summary": "Conntrack BPF map full. Raise bpf-ct-global-tcp-max or lower conntrack-gc-interval.",
        "docs_url": "https://docs.cilium.io/en/stable/operations/troubleshooting/#handling-drop-ct-map-insertion-failed",
        "actions": [
          "kubectl -n kube-system edit configmap cilium-config → increase bpf-ct-global-tcp-max",
          "helm upgrade cilium cilium/cilium --set conntrackGCInterval=30s"
        ]
      }
    }
  ],
  "transient_drops": [
    {
      "reason": "DROP_EP_NOT_READY",
      "bucket": "transient",
      "count": 87,
      "first_seen": "2026-04-26T14:30:01Z",
      "last_seen":  "2026-04-26T14:30:08Z",
      "severity": "low",
      "nodes": [...],
      "workloads": [...],
      "remediation": {
        "summary": "Endpoint BPF program not yet loaded. Normal during pod startup. Investigate only if sustained > 60s.",
        "docs_url": "https://docs.cilium.io/en/stable/operations/troubleshooting/"
      }
    }
  ]
}
```

### Go Struct Sketch (pkg/health)

```go
type ClusterHealth struct {
    SchemaVersion int               `json:"schema_version"`
    GeneratedAt   time.Time         `json:"generated_at"`
    SessionID     string            `json:"session_id"`
    CPGVersion    string            `json:"cpg_version"`
    Summary       HealthSummary     `json:"summary"`
    InfraDrops    []DropReasonEntry `json:"infra_drops,omitempty"`
    TransientDrops []DropReasonEntry `json:"transient_drops,omitempty"`
    // NOISE not emitted (internal bookkeeping, no operator value)
}

type HealthSummary struct {
    TotalInfraDrops       int64 `json:"total_infra_drops"`
    TotalTransientDrops   int64 `json:"total_transient_drops"`
    TotalNoiseDrops       int64 `json:"total_noise_drops"`
    TotalPolicyDrops      int64 `json:"total_policy_drops"`
    DistinctInfraReasons  int   `json:"distinct_infra_reasons"`
    DistinctInfraNodes    int   `json:"distinct_infra_nodes"`
    DistinctInfraWorkloads int  `json:"distinct_infra_workloads"`
}

type DropReasonEntry struct {
    Reason      string            `json:"reason"`       // enum name e.g. "CT_MAP_INSERTION_FAILED"
    Bucket      string            `json:"bucket"`       // "infra" | "transient"
    Count       int64             `json:"count"`
    FirstSeen   time.Time         `json:"first_seen"`
    LastSeen    time.Time         `json:"last_seen"`
    Severity    string            `json:"severity"`     // "critical" | "high" | "medium" | "low"
    Nodes       []NodeCount       `json:"nodes"`
    Workloads   []WorkloadCount   `json:"workloads"`
    Remediation RemediationHint   `json:"remediation"`
}

type NodeCount struct {
    Node  string `json:"node"`
    Count int64  `json:"count"`
}

type WorkloadCount struct {
    Namespace string `json:"namespace"`
    Workload  string `json:"workload"`
    Count     int64  `json:"count"`
}

type RemediationHint struct {
    Summary  string   `json:"summary"`
    DocsURL  string   `json:"docs_url"`
    Actions  []string `json:"actions,omitempty"`
}
```

### Granularity Decision

**Emit all three dimensions (reason × node × workload)** in v1.3. Rationale:
- Node dimension: `CT_MAP_INSERTION_FAILED` is node-local (BPF map per-node). If one node is
  dropping 90% of CT failures, that node needs cilium-config tuning, not the whole cluster.
- Workload dimension: `SERVICE_BACKEND_NOT_FOUND` may be isolated to one workload's traffic
  pattern. Per-workload count helps the operator correlate with a specific service.
- Reason dimension: obvious — different reasons have different remediation paths.

Top-N truncation: emit max 10 nodes and 10 workloads per reason entry. Add `truncated: true`
field if more exist. This prevents enormous JSON for cluster-wide `DROP_EP_NOT_READY` storms.

---

## 4. Session Summary Rendering

### Conventions from Similar CLI Tools

**Terraform:** Severity ordering in plan output: errors first, warnings second, changes third.
Color: red for errors, yellow for warnings, green for no-change. Structured blocks with headers.

**kubectl:** No color by default; ANSI only on TTY (already cpg convention). Uses indented
sub-items for related warnings. Groups by type/resource.

**tflint:** Exit 1 for errors, exit 0 for warnings (warnings do not fail the process). Separate
warning block at end of output.

### Recommended Session Summary Block (for `pipeline.go SessionStats.Log()`)

```
--- Session Summary ---
Duration:       45s
Flows seen:     1,204
Policies written: 12

INFRA DROPS (3 distinct reasons — cluster health issues, NO policy generated):
  CT_MAP_INSERTION_FAILED [CRITICAL]  341 drops  (node-a: 310, node-b: 31)
    → Conntrack map full. Docs: https://docs.cilium.io/...troubleshooting/#ct-map-insertion-failed
  FIB_LOOKUP_FAILED [HIGH]            28 drops
    → Kernel routing gap. Check cilium connectivity test.
  SERVICE_BACKEND_NOT_FOUND [HIGH]    43 drops   (mmtro/adserver: 31, payment/api: 12)
    → Stale LB backend map. Re-create service backends.

TRANSIENT DROPS (2 reasons — normal during pod startup):
  DROP_EP_NOT_READY    87 drops
  DROP_HOST_NOT_READY   3 drops

Cluster health file: ./cluster-health.json
```

**Ordering rules:**
1. INFRA before TRANSIENT (operator action needed for infra; not for transient)
2. Within bucket: by severity (critical → high → medium → low), then by count descending
3. NOISE never shown in summary (internal bookkeeping)
4. TRANSIENT shown as a brief count-only block (no remediation hints — they don't need action)
5. Hide TRANSIENT block entirely if total_transient < 5 AND no INFRA drops (very clean sessions)
6. Top-3 nodes/workloads inline in the summary line; full detail in cluster-health.json

**Color (when ANSI enabled — already tied to TTY detection in cpg):**
- CRITICAL: red bold
- HIGH: red
- MEDIUM: yellow
- LOW: dim/gray
- TRANSIENT section header: yellow
- INFRA section header: red bold

---

## 5. Exit Code Conventions

### Industry Survey

| Tool | Exit 0 | Exit 1 | Exit 2 | Notes |
|------|--------|--------|--------|-------|
| terraform | success, no changes | error | success + changes detected | `--detailed-exitcode` opt-in |
| tflint | no issues | error (parse/internal) | violations found | warnings do NOT cause non-zero |
| kubectl | success | error | — | no warning/error split |
| golangci-lint | no issues | issues found | usage error | |
| trivy | no vulns | vulns found (scan failed) | usage error | |

### Recommendation for cpg v1.3

**Default behavior (no `--fail-on-infra-drops`):**
- Exit 0 always (current behavior preserved). INFRA drops are surfaced in summary + JSON but do
  not affect exit code. Rationale: cpg is a generation tool; infra health is advisory output.
  Existing CI pipelines that use `cpg replay` in checks must not break.

**`--fail-on-infra-drops` opt-in:**
- Exit 0: no infra drops observed
- **Exit 1: infra drops observed** — the only non-zero code cpg uses for infra detection
- Exit 2 is NOT used (avoid terraform collision confusion)
- Internal errors (connection failure, write error) remain exit 1 (current behavior, via `cobra`)

**Rationale for NOT using exit 2:**
- Terraform's `exit 2 = changes detected` is well-known in the K8s/platform space. Using exit 2
  for "infra drops found" would create ambiguity in scripts that wrap both tools.
- The `--fail-on-infra-drops` flag is explicit opt-in; its semantics are documented at the flag
  level. No need for a secondary exit code.

**`--fail-on-infra-drops` is Cobra-compatible:** Return a non-nil sentinel error from `RunE`.
Use a dedicated error type `ErrInfraDropsDetected` so callers can detect it programmatically.

---

## Table Stakes (v1.3 Must-Have)

Features that operators will expect to be present. Missing any = milestone incomplete.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Drop-reason taxonomy embedded in code | Without it, cpg generates bogus policies for CT_MAP_INSERTION_FAILED etc. This is the core bug fix. | LOW-MEDIUM (data + lookup) | Const table in `pkg/health` (new package) or `pkg/hubble`. Pure data; no external calls. |
| Aggregator skips non-policy drops (INFRA + TRANSIENT + NOISE) | CNP generation for infra drops is actively harmful. | LOW (add gate in aggregator before bucketing) | Parity with `--ignore-protocol` (PA5): drop before `keyFromFlow`. Count and record in new HealthAccumulator. |
| cluster-health.json written alongside policy output | SRE needs a structured artifact to act on. JSON is machine-readable for alerting pipelines. | MEDIUM (new writer + schema) | New `pkg/health` package with `HealthWriter`. New `ClusterHealth` JSON schema (schema_version: 1). |
| Session summary block with INFRA drops listed | Terminal-first UX. Operator sees the cluster problem immediately without parsing JSON. | LOW (extend SessionStats.Log) | Add `InfraDrops map[string]int64` and `TransientDrops map[string]int64` to `SessionStats`. |
| `--ignore-drop-reason` flag | Same escape hatch pattern as `--ignore-protocol` (PA5). Critical for production: some operators deliberately tolerate certain infra drops. | LOW (add to aggregator, mirror PA5 exactly) | Comma-separated repeatable flag. Validated against known enum names. |
| `--fail-on-infra-drops` exit code | CI/cron use case: alert when cluster health degrades. Zero config change for existing pipelines. | LOW (add sentinel error return) | Opt-in only. Exit 1 on infra drops. |

---

## Differentiators (Above Minimum)

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Per-node and per-workload counters in health JSON | CT_MAP_INSERTION_FAILED is per-node; SERVICE_BACKEND_NOT_FOUND is per-workload. Both dimensions enable targeted remediation vs. "something is wrong somewhere." | MEDIUM (extend accumulator) | Top-10 truncation to keep JSON bounded. |
| Remediation hints with direct Cilium docs deep links | Operators do not know what CT_MAP_INSERTION_FAILED means or how to fix it. A direct link to the Cilium troubleshooting page converts the health file from a diagnostic to an action item. | LOW (static table) | URLs hardcoded per-reason in taxonomy. Version-pinning risk: link to `/en/stable/` not a specific version. |
| Severity levels per reason (critical/high/medium/low) | `CT_MAP_INSERTION_FAILED` at critical is more urgent than `DROP_EP_NOT_READY` at low. Operators can triage on severity without reading remediation text. | LOW (static table) | Hardcoded per-reason in taxonomy table alongside bucket. |
| `replay` + `generate` parity on health JSON | SRE wants to run health analysis on historical captures, not just live. | LOW (health writer plugs into same pipeline stage as evidence_writer) | ReplayCommand already supports all pipeline flags parity. |
| AUTH_REQUIRED classified with `needs_review` hint | mTLS drops are ambiguous — could be a policy spec change OR a SPIRE infrastructure failure. Flagging for human review prevents silent misclassification. | LOW (special-case in taxonomy) | Add `notes` field to DropReasonEntry for reasons with ambiguous classification. |

---

## Anti-Features (Explicitly Out of Scope for v1.3)

| Anti-Feature | Why Requested | Why Excluded | What Instead |
|--------------|---------------|--------------|-------------|
| OpenMetrics / Prometheus metrics export | "We want to alert on CT_MAP_INSERTION_FAILED in Grafana." | Out of v1.3 scope per PROJECT.md. Fields not yet validated by real usage. Prometheus endpoint requires long-running mode changes. | cluster-health.json is parseable by Prometheus pushgateway scripts. Defer to v1.4 after field names stabilize. |
| Semantic policy intersection ("would existing CNP already allow this?") | "Don't show POLICY_DENIED if there's already a CNP that should allow it." | Requires cluster API access + policy evaluation logic. Separate feature with its own complexity. Out of scope per PROJECT.md. | cpg already deduplicates against cluster policies (`--cluster-dedup`). This is a separate concern. |
| Automatic remediation (apply config changes) | "Just fix the CT map size for me." | cpg is a read/observe/generate tool. Applying cluster-level config changes is a fundamentally different trust level. Risk of unintended side effects. | Remediation hints are advisory. Operator applies changes manually or via their GitOps pipeline. |
| Splitting POLICY drops by which CNP rule was missing | "Show me which policy would have allowed this." | Requires policy evaluation against full cluster state + label resolution. This is the semantic intersection feature deferred above. | Session summary shows namespace/workload. `cpg explain` provides flow-level detail. |
| Per-pod (not per-workload) granularity in health JSON | "Show me which pod had the CT failure." | Pod names are ephemeral. Workload granularity (Deployment/DaemonSet) is actionable; pod names are noise. | Workload name from labels (existing `labels.WorkloadName` function). Pod name in FlowSample evidence. |
| Historical health trending (compare sessions) | "Show me if CT failures are getting worse over time." | Requires persistent state store across sessions. out-of-scope complexity. | Multiple cluster-health.json files can be diff'd manually. Session ID links to timestamp. |
| Web UI or dashboard for health data | — | CLI tool only per PROJECT.md. | — |

---

## Feature Dependencies (Integration Map)

```
[v1.2 Pipeline] (shipped)
        |
        +-- [TAXONOMY: pkg/health — drop reason → bucket + severity + hint]
        |       └── static table, no runtime deps
        |       └── used by all other v1.3 features
        |
        +-- [AGGREGATOR GATE: skip INFRA/TRANSIENT/NOISE before keyFromFlow]
        |       └── requires TAXONOMY
        |       └── mirrors PA5 (--ignore-protocol) pattern
        |       └── feeds HealthAccumulator (new)
        |
        +-- [HealthAccumulator: count by reason × node × workload]
        |       └── requires AGGREGATOR GATE
        |       └── analogous to ignoredByProtocol map in Aggregator
        |       └── feeds HealthWriter + SessionStats
        |
        +-- [HealthWriter: write cluster-health.json]
        |       └── requires HealthAccumulator
        |       └── new pkg/health package (or extend pkg/hubble)
        |       └── fan-out from pipeline like evidence_writer
        |
        +-- [SessionStats extension: InfraDrops + TransientDrops counts]
        |       └── requires HealthAccumulator
        |       └── extend existing SessionStats.Log() for summary block
        |
        +-- [--ignore-drop-reason flag]
        |       └── requires TAXONOMY (validation of reason names)
        |       └── parallel to --ignore-protocol in aggregator
        |
        +-- [--fail-on-infra-drops exit code]
                └── requires HealthAccumulator (non-zero infra count)
                └── sentinel error from RunPipeline / RunPipelineWithSource
```

### Critical Dependency Note

The aggregator gate (skipping non-policy drops) is the load-bearing change. Every other v1.3
feature flows from it. The gate must be placed BEFORE `keyFromFlow` so that INFRA/TRANSIENT
flows are:
1. Never bucketed (no CNP generated — the bug fix)
2. Counted by the HealthAccumulator (for health JSON and session summary)
3. Excluded from `flowsSeen` (preserves existing semantics of that counter for VIS-01)

Ordering: implement TAXONOMY first (pure data), then GATE (pure logic), then ACCUMULATOR
(counter), then WRITER (I/O), then FLAGS, then SUMMARY, then EXIT CODE.

---

## Sources

- [Cilium flow.proto DropReason enum](https://github.com/cilium/cilium/blob/main/api/v1/flow/flow.proto) — canonical enum (HIGH)
- [cilium/pkg/monitor/api/drop.go](https://github.com/cilium/hubble/blob/main/vendor/github.com/cilium/cilium/pkg/monitor/api/drop.go) — string names and DropMin threshold (HIGH)
- [Cilium Troubleshooting Guide](https://docs.cilium.io/en/stable/operations/troubleshooting/) — CT_MAP_INSERTION_FAILED remediation (HIGH)
- [Cilium Mutual Authentication docs](https://docs.cilium.io/en/stable/network/servicemesh/mutual-authentication/mutual-authentication/) — AUTH_REQUIRED classification context (MEDIUM)
- [Cilium Egress Gateway troubleshooting](https://docs.cilium.io/en/stable/network/egress-gateway/egress-gateway-troubleshooting/) — NO_EGRESS_GATEWAY, DROP_NO_EGRESS_IP context (MEDIUM)
- [Inspektor Gadget advise networkpolicy](https://inspektor-gadget.io/docs/main/gadgets/advise_networkpolicy/) — confirmed does not consume drop events (MEDIUM)
- [Otterize network-mapper](https://github.com/otterize/network-mapper) — confirmed flow-based, not drop-based (MEDIUM)
- [Terraform detailed-exitcode convention](https://discuss.hashicorp.com/t/terraform-detailed-exitcode-causes-plan-to-fail-when-exit-code-2/76890) — exit 0/1/2 precedent (HIGH)
- [Cilium CT map insertion failed GitHub issues](https://github.com/cilium/cilium/issues/35010) — field-validated infra classification (HIGH)
- [SERVICE_BACKEND_NOT_FOUND issues](https://github.com/cilium/cilium/issues/27061) — infra + stale endpoint slice context (MEDIUM)
- [FIB_LOOKUP_FAILED issues](https://github.com/cilium/cilium/issues/15200) — routing gap confirmed (MEDIUM)
- [UNENCRYPTED_TRAFFIC advisory](https://github.com/cilium/cilium/security/advisories/GHSA-7496-fgv9-xw82) — WireGuard strict mode context (HIGH)
- `.planning/PROJECT.md` — v1.3 scope + out-of-scope locks

---
*Feature research for: cpg v1.3 — Cluster Health Surfacing*
*Researched: 2026-04-26*
