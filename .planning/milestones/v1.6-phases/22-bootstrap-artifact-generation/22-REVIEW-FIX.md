---
phase: 22-bootstrap-artifact-generation
fixed_at: 2026-07-22T00:00:00Z
review_path: .planning/phases/22-bootstrap-artifact-generation/22-REVIEW.md
iteration: 1
findings_in_scope: 2
fixed: 3
skipped: 0
status: all_fixed
---

# Phase 22: Code Review Fix Report

Scope: Critical + Warning (default). Fixes applied inline by the orchestrator (both mechanical).

| ID | Severity | Fix | Files |
|----|----------|-----|-------|
| WR-01 | warning | Runbook no longer documents the removed `-o`/`--output` flag — replaced with shell redirection (`cpg bootstrap -n <ns> > file.yaml`) | `docs/bootstrap-runbook.md` |
| WR-02 | warning | Added `validateBootstrapNamespace` (apimachinery `IsDNS1123Label`, no new dep) shared by CLI (`runBootstrap`) and MCP (`handleGetBootstrapPolicy`) — invalid namespaces (`Foo Bar`, `../etc`, `UPPER`, `trailing-`) rejected before detection/emission on both surfaces; covered by `TestBootstrapInvalidNamespace` | `cmd/cpg/bootstrap.go`, `cmd/cpg/mcp_bootstrap.go`, `cmd/cpg/bootstrap_test.go` |
| IN-01 | info | Fixed opportunistically with WR-01 (same drift): stale `-n/-o` constructor comment now says `-n` | `cmd/cpg/bootstrap.go` |

Skipped (info, out of default scope): IN-02 (shared `t := true` backing var in builder — harmless, future footgun only), IN-03 (`VersionSource` can be empty vs schema enum promise — cosmetic schema wording).

Verification: `rtk proxy go build ./...`, `go vet ./cmd/cpg/`, `rtk proxy go test ./cmd/cpg/... -run "TestBootstrap|TestGetBootstrapPolicy|TestRunbook|TestReadmeCompat" -count=1 -race` — all green.
