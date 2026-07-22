---
phase: 20
slug: include-audit-verdict-ingestion
status: verified
threats_open: 0
asvs_level: 1
created: 2026-07-22
---

# Phase 20 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Hubble Relay gRPC stream → cpg (buildFilters) | Server-side verdict filter; widening is opt-in | Flow telemetry (verdicts, L3/L4/L7 metadata) |
| Replay file (untrusted JSONL) → cpg (file.go gate) | Per-line verdict gate; existing protojson boundary, no new parse surface | Serialized flows from disk |
| Hubble flow channel → aggregator classification gate | AUDIT flows newly evaluated at site 5; DROPPED handling unchanged | Classified flow events |
| CLI arg (cobra) → PipelineConfig | User-controlled bool; cobra self-validates true/false | `--include-audit` flag |
| MCP client (LLM) → start_session args | JSON arg coerced by go-sdk JSON-schema; reaches PipelineConfig | `include_audit` bool |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-20-01 | Tampering / Elevation (default-off violation) | classification gate, filter sites 1-4, flag plumbing | mitigate | `includeAudit` defaults false everywhere (`f.Bool("include-audit", false, ...)`, omitempty MCP arg, `PipelineConfig` zero value); flag-off byte-identical proven executable: `TestAggregator_AuditNotClassifiedWhenDisabled`, `TestBuildFilters_*` value-pinning with `false` arg, `TestPipeline_AuditDisabled_AuditFlowsIgnored` (E2E), plus verifier binary-run diff of generated CNP YAML flag on/off | closed |
| T-20-03 | Tampering (scope creep beyond {DROPPED, AUDIT}) | buildFilters, replay gate, aggregator gate | mitigate | Single `verdicts` slice built once and reused; hardcoded `{DROPPED, AUDIT}` — no generalized verdict allowlist; repo-wide re-grep confirms no site admits REFUSED/REDIRECTED; code review verified boolean equivalence at all 3 verdict sites | closed |
| T-20-04 | Tampering (malformed MCP input) | startSessionArgs.IncludeAudit | mitigate | Bool coerced by go-sdk JSON-schema (same mechanism as L7/TLS/ClusterDedup); no input surface beyond true/false | closed |
| T-20-05 | Elevation (SEC-01 readonly regression) | MCP composition root | mitigate | Zero new K8s write verbs, zero new filesystem-write call sites; `cmd/cpg/mcp_audit_test.go` byte-identical across the whole phase (`git diff --stat 9481ba6..HEAD -- cmd/cpg/mcp_audit_test.go` empty) and green | closed |
| T-20-SC | Tampering (supply chain) | go.mod / go.sum | accept | Zero new packages across all 4 plans + WR-01 fix; `go.mod`/`go.sum` diff empty vs phase base | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| R-20-01 | T-20-SC | No dependency changes this phase — supply-chain surface unchanged; Package Legitimacy Audit not triggered | gsd orchestrator (plan-time disposition, all 4 plans) | 2026-07-22 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-07-22 | 5 | 5 | 0 | secure-phase short-circuit (plan-time register; evidence from gsd-verifier 19/19 + code review 21 files) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-07-22
