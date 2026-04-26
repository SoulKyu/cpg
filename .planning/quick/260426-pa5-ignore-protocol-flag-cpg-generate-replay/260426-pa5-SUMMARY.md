---
phase: 260426-pa5-ignore-protocol-flag-cpg-generate-replay
plan: 01
type: quick
status: complete
completed: "2026-04-26T18:25:00Z"
requirements:
  - PA5-IGN-PROTO
key-files:
  created:
    - testdata/flows/mixed_tcp_icmp.jsonl
  modified:
    - pkg/hubble/aggregator.go
    - pkg/hubble/aggregator_test.go
    - pkg/hubble/pipeline.go
    - cmd/cpg/commonflags.go
    - cmd/cpg/generate.go
    - cmd/cpg/generate_test.go
    - cmd/cpg/replay.go
    - cmd/cpg/replay_test.go
metrics:
  tests_total_before: 319
  tests_total_after: 328
  tests_added: 9
commits:
  - 532142a -- test(pa5): add failing tests for --ignore-protocol filter and flag parsing
  - bb9aa03 -- feat(pa5): --ignore-protocol drops flows by L4 proto in aggregator + counter
  - 8f33122 -- test(pa5): e2e replay test + fixture for --ignore-protocol
---

# Quick Task pa5 (260426): `--ignore-protocol` Flag Summary

Repeatable, comma-separated, case-insensitive `--ignore-protocol` flag on `cpg generate` and `cpg replay` that filters flows by L4 protocol BEFORE bucketing, with a per-protocol drop counter logged in the session summary.

## Final API Surface

- **Flag spelling:** `--ignore-protocol` (StringSlice; repeatable + comma-separated).
- **Valid values:** `tcp`, `udp`, `icmpv4`, `icmpv6`, `sctp`. Single source of truth in `pkg/hubble/aggregator.go` (`validIgnoreProtocols` map), exposed sorted via `hubble.ValidIgnoreProtocols()`.
- **Normalization:** raw input lowercased in `cmd/cpg.validateIgnoreProtocols` (preserves order, no dedup — aggregator set dedups).
- **Unknown value:** rejected at command setup with `unknown protocol "<v>": valid values are icmpv4, icmpv6, sctp, tcp, udp` (sorted allowlist).
- **Empty/nil:** no-op, both at the CLI helper and at `Aggregator.SetIgnoreProtocols`.

## Drop Location Rationale

Inside `Aggregator.Run`, after the L7 HTTP/DNS counter bumps (which must remain accurate for the VIS-01 gate) and BEFORE `keyFromFlow`. This means ignored flows:

- do NOT increment `flowsSeen`,
- do NOT touch `seenWorkloads` (no spurious VIS-01 workload entries),
- do NOT reach the unhandled tracker (they are *intentionally excluded*, not "unhandled"),
- DO increment the `ignoredByProtocol` counter for transparency.

Rationale: the operator's intent — "make these flows invisible" — must be invisible at every observable surface except the dedicated counter.

## New Aggregator + SessionStats Fields

- `Aggregator.ignoreProtocols map[string]struct{}` — lowercase set, populated via `SetIgnoreProtocols`.
- `Aggregator.ignoredByProtocol map[string]uint64` — counters; copy returned by `IgnoredByProtocol()`.
- `PipelineConfig.IgnoreProtocols []string` — already-validated lowercase slice forwarded by `cmd/cpg`.
- `SessionStats.IgnoredByProtocol map[string]uint64` — populated in the `g.Wait()` finalize block, logged via `zap.Any("ignored_by_protocol", ...)`.
- Internal helper `flowL4ProtoName(*flowpb.Flow) string` — returns one of `tcp/udp/icmpv4/icmpv6/sctp` or `""` (nil-L4 / unknown protocols fall through to the existing tracker paths).

## Tests Added (9)

`pkg/hubble/aggregator_test.go` (4):

- `TestAggregator_IgnoreProtocols_DropsICMPv4`
- `TestAggregator_IgnoreProtocols_TCPPassthrough`
- `TestAggregator_IgnoreProtocols_MultipleProtocols`
- `TestAggregator_IgnoreProtocols_EmptyIsNoOp`

`cmd/cpg/generate_test.go` (4):

- `TestParseCommonFlags_IgnoreProtocol_CaseInsensitiveAndCommaSep`
- `TestValidateIgnoreProtocols_Normalization`
- `TestValidateIgnoreProtocols_UnknownReturnsError`
- `TestValidateIgnoreProtocols_EmptyIsNoOp`

`cmd/cpg/replay_test.go` (1):

- `TestReplay_IgnoreProtocol_ICMP` — e2e against `testdata/flows/mixed_tcp_icmp.jsonl`, asserts no `icmps:`/`ICMPv4`/`ICMPv6` in output, at least one TCP rule survives, session summary log carries `ignored_by_protocol{icmpv4>=1}`.

## Fixture

`testdata/flows/mixed_tcp_icmp.jsonl` (4 lines, all `verdict: DROPPED`):

- 2× INGRESS TCP (ports 80, 443) `app=client` → `app=server` in `production`.
- 2× EGRESS ICMPv4 type 8 from `app=client` toward `reserved:world` (10.0.0.1, 10.0.0.2).

## Verification

```
$ go test ./... -count=1
Go test: 328 passed in 9 packages   # 319 baseline + 9 new

$ go vet ./...
(clean)

$ go test ./cmd/cpg/ -run 'L7FlagByteStable|L7HTTP_DisabledByteStable|L7DNSDisabled' -count=1
Go test: 3 passed in 1 packages     # Phase 7/8/9 byte-stability invariants intact
```

End-to-end manual smoke (after Task 3):

```
$ go run ./cmd/cpg replay testdata/flows/mixed_tcp_icmp.jsonl \
    --ignore-protocol ICMPv4,icmpv6 -o /tmp/cpg-pa5-smoke ...
"ignored_by_protocol": {"icmpv4":2}, "policies_written": 1
$ grep -l 'icmps:' /tmp/cpg-pa5-smoke -r || echo "OK: no icmps blocks"
OK: no icmps blocks
```

Negative path:

```
$ go run ./cmd/cpg replay ... --ignore-protocol foo
Error: unknown protocol "foo": valid values are icmpv4, icmpv6, sctp, tcp, udp
```

## Deviations from Plan

None. Plan executed exactly as written. The plan's hint that the error allowlist would read `tcp, udp, icmpv4, icmpv6, sctp` was inconsistent with its "(sorted)" qualifier — the implementation emits the alphabetically sorted list (`icmpv4, icmpv6, sctp, tcp, udp`) which is what `ValidIgnoreProtocols()` already returns, and the test was set to match the actually-sorted form.

## Deferred Items (Out of Scope)

- `--only-protocol` (positive filter, inverse of `--ignore-protocol`) — not requested, not implemented.
- Per-protocol drop metrics on a Prometheus endpoint — `cpg` has no metrics surface yet.
- Wildcard / pattern matching beyond the L4 allowlist — over-scope for the use case (operators want a known finite set).

## Self-Check: PASSED

- testdata/flows/mixed_tcp_icmp.jsonl: FOUND
- pkg/hubble/aggregator.go: FOUND (modified)
- pkg/hubble/aggregator_test.go: FOUND (modified)
- pkg/hubble/pipeline.go: FOUND (modified)
- cmd/cpg/commonflags.go: FOUND (modified)
- cmd/cpg/generate.go: FOUND (modified)
- cmd/cpg/generate_test.go: FOUND (modified)
- cmd/cpg/replay.go: FOUND (modified)
- cmd/cpg/replay_test.go: FOUND (modified)
- Commit 532142a: FOUND
- Commit bb9aa03: FOUND
- Commit 8f33122: FOUND
