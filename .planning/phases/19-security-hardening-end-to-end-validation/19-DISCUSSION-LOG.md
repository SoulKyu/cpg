# Phase 19: Security Hardening & End-to-End Validation - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-21
**Phase:** 19-security-hardening-end-to-end-validation
**Mode:** Fully autonomous (user directive: "full autonomie, ne reviens vers moi QUE quand la phase 19 est terminée entièrement") — all gray areas auto-selected, recommended option chosen for every question.
**Areas discussed:** SEC-01 audit mechanism, SRV-04 e2e harness shape, SRV-01 schema assertion depth, SEC-03 README structure

---

## SEC-01 — structural readonly audit mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| Function-level static reachability (SSA callgraph rooted at `runMCPServer`, x/tools test-only dep) | Sound "reachable from the composition root" proof; survives the fact that `cmd/cpg` is one `package main` mixing MCP and CLI write paths | ✓ |
| Package/import-level scan + file conventions | Simpler, but cannot distinguish MCP-reachable from CLI-only code inside `package main` — would need a mushy allowlist that lets a future MCP tool call `runGenerate` unnoticed | |
| Runtime interception/sandboxing | Proves a run, not reachability; not structural | |

**Auto-selected:** callgraph reachability + explicit function-symbol allowlist for tmpdir-rooted writers. Algorithm (RTA vs CHA fallback) left to planner with over-approximation as the required soundness direction.

---

## SRV-04 — e2e harness shape

| Option | Description | Selected |
|--------|-------------|----------|
| Real subprocess (`go build -race` the binary, drive real stdin/stdout) + in-process fake Hubble observer gRPC server + `start_session{server, tls:false}` bypass | "Full stdio" as the roadmap demands; no cluster/kubeconfig; 17-CONTEXT D-07 was designed for exactly this | ✓ |
| In-memory transport extension | Already exists (Phase 18); does not exercise real stdio, framing, or the global stdout swap | |
| Real cluster (kind/minikube) in CI | Heavy, flaky, out of proportion for a stdio-contract test | |

**Auto-selected:** subprocess + fake relay. Ungraceful variant = abrupt stdin close after tmpdir discovery via `get_status`; assert bounded exit + tmpdir removal + relay stream ctx cancel. Port-forward-close step explicitly mapped to existing SESS-05 unit coverage (e2e bypasses port-forward by design) — documented in CONTEXT D-09 to preempt a phantom verification gap.

---

## SRV-01 — schema assertion depth

| Option | Description | Selected |
|--------|-------------|----------|
| Structural invariants (exact 8-name set, descriptions, input/output schema presence, annotations, dropclass enum) | Pins the contract without coupling to SDK serialization details | ✓ |
| Golden-file JSON schema snapshots | Byte-brittle against go-sdk changes; no added safety over invariants | |

**Auto-selected:** structural invariants, asserted inside the e2e handshake (SRV-01 satisfied on real stdio; in-memory all-8-tools test stays as fast feedback).

---

## SEC-03 — README structure

| Option | Description | Selected |
|--------|-------------|----------|
| One top-level `## MCP Server (cpg mcp)` section, example-first, six mandatory blocks (what/tools table/harness env JSON/secrets posture/exec-credential caveat/session model) | Matches README voice; requirement lists exactly these contents | ✓ |
| Separate docs/ page linked from README | Indirection for a tool with a single README; nothing else lives in docs/ | |

**Auto-selected:** single README section written from scratch (README currently has zero MCP mentions).

---

## Claude's Discretion

- E2e client plumbing (go-sdk CommandTransport vs hand-rolled pipes — researcher confirms SDK surface)
- Test file layout, `testing.Short()` guard, bounded-deadline constants
- Fake relay fixture flow shapes
- README prose/formatting within the mandatory block list

## Deferred Ideas

None new — REDACT-01/LIVE-01/FLOW-01 already tracked as v2; LINT/RELSEC debt tracked outside phase requirements.
