# Technology Stack — v1.3 Cluster Health Surfacing

**Project:** cpg (Cilium Policy Generator)
**Researched:** 2026-04-26
**Scope:** NEW additions only for v1.3. Existing stack (Go 1.25.1, cobra, zap, client-go, cilium/cilium gRPC proto) is unchanged and not re-litigated here.

---

## No New Dependencies Required

All v1.3 features are implementable with the existing `go.mod`. Zero new `require` entries.

| Feature | Implementation | Why No New Dep |
|---------|---------------|----------------|
| Drop-reason taxonomy | Code-embedded `map[flowpb.DropReason]Category` | Keyed on proto enum — stdlib map |
| cluster-health.json write | `encoding/json` + atomic write (same pattern as `pkg/evidence/writer.go`) | Already used in evidence package |
| Remediation hint URLs | String constants embedded in taxonomy map or a parallel `map[Category]string` | No templating needed — static strings |
| Session summary block | `fmt.Fprintf` to `os.Stderr` or existing zap | stdlib only |
| `--ignore-drop-reason` flag | `cobra.StringSlice`, same validation pattern as `--ignore-protocol` | `spf13/cobra v1.10.2` already present |
| `--fail-on-infra-drops` exit code | Custom sentinel error type returned from `RunE`, caught in `main()` | `os.Exit` already called on `rootCmd.Execute()` error |

---

## Q1: DropReason Enum — Canonical Iteration at Compile Time

**Answer: Use `flowpb.DropReason_name` (runtime map), not compile-time iteration. This is the correct pattern for protobuf enums in Go.**

**Verified against:** `/home/gule/go/pkg/mod/github.com/cilium/cilium@v1.19.1/api/v1/flow/flow.pb.go` lines 597–675.

The generated code exposes two exported package-level vars:

```go
// github.com/cilium/cilium/api/v1/flow — flow.pb.go
var (
    DropReason_name  = map[int32]string{ 0: "DROP_REASON_UNKNOWN", 130: "INVALID_SOURCE_MAC", ... }
    DropReason_value = map[string]int32{ "DROP_REASON_UNKNOWN": 0, "POLICY_DENIED": 133, ... }
)
```

Import path already used in the codebase: `flowpb "github.com/cilium/cilium/api/v1/flow"` (see `pkg/hubble/aggregator.go:10`, `evidence_writer.go:6`).

**Taxonomy map pattern** — the new `pkg/health` package should define its classifier as:

```go
import flowpb "github.com/cilium/cilium/api/v1/flow"

type Category string
const (
    CategoryPolicy    Category = "policy"
    CategoryInfra     Category = "infra"
    CategoryTransient Category = "transient"
    CategoryUnknown   Category = "unknown"
)

var dropReasonCategory = map[flowpb.DropReason]Category{
    flowpb.DropReason_POLICY_DENIED:   CategoryPolicy,
    flowpb.DropReason_POLICY_DENY:     CategoryPolicy,
    flowpb.DropReason_AUTH_REQUIRED:   CategoryPolicy,
    flowpb.DropReason_CT_MAP_INSERTION_FAILED: CategoryInfra,
    // ... full list
}

func Classify(r flowpb.DropReason) Category {
    if c, ok := dropReasonCategory[r]; ok {
        return c
    }
    return CategoryUnknown
}
```

`flowpb.DropReason_name` can be used in `ValidIgnoreDropReasons()` (same pattern as `ValidIgnoreProtocols()` in `pkg/hubble/aggregator.go:38`) to build the allowlist for `--ignore-drop-reason` flag validation.

**Complete enum values** (74 entries in cilium@v1.19.1, proto range 0 and 130–205):

Policy-class candidates: `POLICY_DENIED (133)`, `POLICY_DENY (181)`, `AUTH_REQUIRED (189)`, `NO_CONFIGURATION_AVAILABLE_TO_PERFORM_POLICY_DECISION (165)`.

Infra-class candidates: `CT_MAP_INSERTION_FAILED (155)`, `CT_NO_MAP_FOUND (190)`, `SNAT_NO_MAP_FOUND (191)`, `FIB_LOOKUP_FAILED (169)`, `SERVICE_BACKEND_NOT_FOUND (158)`, `NO_EGRESS_GATEWAY (194)`, `DROP_HOST_NOT_READY (202)`, `DROP_EP_NOT_READY (203)`, `REACHED_EDT_RATE_LIMITING_DROP_HORIZON (162)`, `DROP_RATE_LIMITED (198)`.

Transient-class candidates: `STALE_OR_UNROUTABLE_IP (151)`, `UNKNOWN_CONNECTION_TRACKING_STATE (163)`.

Everything else defaults to `unknown` — safe for the planner to refine.

**Confidence: HIGH** — directly verified against module cache.

---

## Q2: New Deps for JSON Marshaling, Atomic Writes, Remediation Links

**Answer: None needed.**

- `encoding/json` — stdlib, already used in `pkg/evidence/writer.go` for `json.MarshalIndent` + `json.Unmarshal`. Same approach for `cluster-health.json`.
- Atomic write pattern — `os.CreateTemp` + `os.Rename` already implemented at `pkg/evidence/writer.go:62–80`. Copy verbatim into new `pkg/health/writer.go`.
- Remediation hint URLs — static string constants co-located with the taxonomy map. No `text/template`, no external package. Example: `map[Category]string{CategoryInfra: "https://docs.cilium.io/en/stable/operations/troubleshooting/"}`.

**Confidence: HIGH** — verified against existing codebase patterns.

---

## Q3: Cobra Exit Code Pattern for `--fail-on-infra-drops`

**Answer: Return a custom sentinel error from `RunE`; `main()` already calls `os.Exit(1)` on any `rootCmd.Execute()` error. For a distinct exit code (e.g., 2), intercept before `os.Exit`.**

Current flow in `cmd/cpg/main.go:58–60`:
```go
if err := rootCmd.Execute(); err != nil {
    os.Exit(1)
}
```

`cobra.Command.RunE` returns an `error`. If it returns non-nil, `Execute()` returns that error, and `main()` exits with code 1.

For `--fail-on-infra-drops`, two viable patterns:

**Pattern A — Single exit code (simplest):**
Return a sentinel error `ErrInfraDropsDetected` from `runGenerate`/`runReplay`. `main()` catches it and exits 1. No `main.go` changes needed.

**Pattern B — Distinct exit code (recommended for v1.3):**
Define a typed error:
```go
type ExitCodeError struct {
    Code int
    Msg  string
}
func (e *ExitCodeError) Error() string { return e.Msg }
```
In `main.go`, change the Execute block:
```go
if err := rootCmd.Execute(); err != nil {
    var ec *ExitCodeError
    if errors.As(err, &ec) {
        os.Exit(ec.Code)
    }
    os.Exit(1)
}
```

**Recommendation: Pattern B.** The v1.3 requirement is a CI/cron hook — operators need a distinct exit code to differentiate "infra drops detected" (actionable) from "cpg error" (bug). Exit 2 is the conventional "condition" code in CLI tooling. The `ExitCodeError` type adds ~10 lines to `main.go` and is clean. `runGenerate` and `runReplay` return `&ExitCodeError{Code: 2, Msg: "infra drops detected"}` when `--fail-on-infra-drops` is set and `stats.InfraDropCount > 0`.

`cobra` itself does not expose an exit-code mechanism beyond returning an error from `RunE` — there is no built-in `cmd.SetExitCode()`. Verified: cobra v1.10.2 has no such API.

**Confidence: HIGH** — verified against `main.go` source and cobra v1.10.2 API.

---

## Q4: Hubble Proto DropReasonDesc Stability Across Cilium 1.14 / 1.15 / 1.16

**Answer: `DropReasonDesc` field (field 25 on `Flow`) is stable. Values are additive — new enum values are appended, no renames or removals in this range. One critical nuance: `POLICY_DENIED` (133) and `POLICY_DENY` (181) coexist; BOTH must be classified as `CategoryPolicy`.**

Key findings from proto inspection:

- Field `drop_reason_desc = 25` (type `DropReason`) introduced in Cilium 1.13 to supersede deprecated `uint32 drop_reason = 3`.
- `POLICY_DENY (181)` was added in Cilium 1.14 as a second policy-denial code distinct from `POLICY_DENIED (133)`. Both are emitted depending on which BPF program path triggers the drop. The taxonomy must classify both as `CategoryPolicy`.
- `AUTH_REQUIRED (189)`, `CT_NO_MAP_FOUND (190)`, `SNAT_NO_MAP_FOUND (191)` appeared in the 1.14–1.15 range.
- `DROP_HOST_NOT_READY (202)`, `DROP_EP_NOT_READY (203)`, `DROP_NO_EGRESS_IP (204)`, `DROP_PUNT_PROXY (205)` are 1.15–1.16 additions.
- No enum value has been renamed or removed (proto numeric stability guarantee).

`DropReason_UNKNOWN (0)` is emitted when the agent doesn't populate the field (older nodes, or non-drop verdicts). The classifier must return `CategoryUnknown` for 0. The aggregator must still count it in `cluster-health.json` but must not suppress it from policy generation (it might be a genuine policy denial with missing metadata).

At runtime, `f.GetDropReasonDesc()` returns `flowpb.DropReason_DROP_REASON_UNKNOWN` (0) for unset fields — safe default, no nil dereference. Already called at `evidence_writer.go:131` as `f.GetDropReasonDesc().String()`.

**Confidence: MEDIUM** — proto file verified in module cache (v1.19.1); version range evolution inferred from enum value numbering and proto comments. No cross-referenced changelog, but additive proto guarantee is a Cilium project commitment.

---

## Integration Points

| What changes | File(s) | Change type |
|---|---|---|
| New `pkg/health` package | `pkg/health/classifier.go`, `pkg/health/writer.go` | New files |
| Aggregator drop-reason filter | `pkg/hubble/aggregator.go` | Modify — add `ignoreDropReasons` + `SetIgnoreDropReasons()`, suppress non-policy flows before bucketing |
| `validIgnoreDropReasons` set | `pkg/hubble/aggregator.go` | Modify — add alongside `validIgnoreProtocols`; keyed on `flowpb.DropReason_value` map |
| `ValidIgnoreDropReasons()` func | `pkg/hubble/aggregator.go` | New exported func — mirrors `ValidIgnoreProtocols()` |
| `--ignore-drop-reason` flag | `cmd/cpg/commonflags.go` | Modify — add flag + `validateIgnoreDropReasons()` |
| `--fail-on-infra-drops` flag | `cmd/cpg/commonflags.go` | Modify — add bool flag |
| `ExitCodeError` type | `cmd/cpg/main.go` | Modify — ~10 lines, intercept before `os.Exit(1)` |
| `PipelineConfig` extensions | `pkg/hubble/pipeline.go` | Modify — add `IgnoreDropReasons []string`, `FailOnInfraDrops bool`, `HealthOutputPath string` |
| `SessionStats` extensions | `pkg/hubble/pipeline.go` | Modify — add `InfraDropCount uint64`, `IgnoredByDropReason map[string]uint64` |
| `cluster-health.json` write | `pkg/health/writer.go` + wired in `pipeline.go` Finalize section | New logic post-pipeline |
| Session summary infra block | `pkg/hubble/pipeline.go` `SessionStats.Log()` | Modify |

---

## Anti-Additions (Explicitly Out of Scope for v1.3)

| Library / Feature | Why Not |
|---|---|
| `prometheus/client_golang` | Metrics export deferred — gather field feedback first (PROJECT.md) |
| `open-telemetry/opentelemetry-go` | Same deferral as Prometheus |
| Any semantic policy solver | Shelved (PROJECT.md) |
| `text/template` for remediation hints | Overkill — static URL constants suffice |
| `cpg apply` command | Deferred to v1.4+ (PROJECT.md) |
| Policy consolidation/merging | Deferred to v1.4+ |
| L7-FUT-* flags | Deferred to v1.4+ |
| `database/sql` or embedded DB | No persistence layer needed — JSON file output is sufficient |

---

## Sources

- Verified: `/home/gule/go/pkg/mod/github.com/cilium/cilium@v1.19.1/api/v1/flow/flow.pb.go` — `DropReason` enum constants, `DropReason_name` + `DropReason_value` exported vars
- Verified: `/home/gule/go/pkg/mod/github.com/cilium/cilium@v1.19.1/api/v1/flow/flow.proto` — complete enum definition (lines 430–end)
- Verified: `/home/gule/Workspace/team-infrastructure/cpg/pkg/hubble/aggregator.go` — `ValidIgnoreProtocols()` + `validIgnoreProtocols` pattern
- Verified: `/home/gule/Workspace/team-infrastructure/cpg/pkg/evidence/writer.go` — atomic write pattern (`os.CreateTemp` + `os.Rename`)
- Verified: `/home/gule/Workspace/team-infrastructure/cpg/cmd/cpg/main.go` — current `os.Exit(1)` on `Execute()` error
- Verified: `/home/gule/Workspace/team-infrastructure/cpg/go.mod` — all existing dependency versions (cilium/cilium v1.19.1, cobra v1.10.2)
