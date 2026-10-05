---
schema_version: 1
open_count: 2
waived_count: 0
fixed_count: 0
total_count: 2
last_updated: 2026-10-05T08:49:04.540Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 26 | unrun-verify | pkg/k8s/ns_watch.go |  | Full-suite -race gate excludes whole-program SSA/RTA tests (TestMCPAuditReadonlyReachability, TestAuditWindowNotReachableFromMCP) due to environmental OOM on no-swap machine; both green non-race | open |  | 2026-10-05T08:49:04.402Z |  |
| 2 | 26 | deviation | pkg/session/manager_test.go | 656 | TestManager_Start_ShutdownRacesSetup load-flaky under full-suite parallel -race; fails identically at baseline 8fc2b68 (pre-existing, environmental) | open |  | 2026-10-05T08:49:04.540Z |  |

````json
[
  {
    "id": 1,
    "kind": "unrun-verify",
    "phase": "26",
    "file": "pkg/k8s/ns_watch.go",
    "line": null,
    "description": "Full-suite -race gate excludes whole-program SSA/RTA tests (TestMCPAuditReadonlyReachability, TestAuditWindowNotReachableFromMCP) due to environmental OOM on no-swap machine; both green non-race",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-10-05T08:49:04.402Z",
    "resolved_at": null,
    "milestone": "v1.7"
  },
  {
    "id": 2,
    "kind": "deviation",
    "phase": "26",
    "file": "pkg/session/manager_test.go",
    "line": 656,
    "description": "TestManager_Start_ShutdownRacesSetup load-flaky under full-suite parallel -race; fails identically at baseline 8fc2b68 (pre-existing, environmental)",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-10-05T08:49:04.540Z",
    "resolved_at": null,
    "milestone": "v1.7"
  }
]
````
