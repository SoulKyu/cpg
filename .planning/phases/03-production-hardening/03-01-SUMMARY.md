---
phase: 03-production-hardening
plan: 01
subsystem: policy
tags: [cilium, cidr, world-identity, dedup, cnp]

requires:
  - phase: 01-core-policy-engine
    provides: "BuildPolicy, MergePolicy, Writer, label selectors"
provides:
  - "CIDR-based egress/ingress rules for world identity (external) traffic"
  - "PoliciesEquivalent semantic comparison for policy dedup"
  - "File-based dedup in Writer to skip redundant disk writes"
affects: [03-02-cluster-dedup]

tech-stack:
  added: []
  patterns: [cidr-rule-generation, yaml-byte-comparison-dedup, label-prefix-normalization]

key-files:
  created:
    - pkg/policy/dedup.go
    - pkg/policy/dedup_test.go
  modified:
    - pkg/policy/builder.go
    - pkg/policy/builder_test.go
    - pkg/policy/merge.go
    - pkg/policy/testdata/ingress_flow.go
    - pkg/output/writer.go
    - pkg/output/writer_test.go

key-decisions:
  - "YAML byte comparison for file dedup instead of in-memory PoliciesEquivalent (avoids any: prefix roundtrip mismatch)"
  - "matchEndpoints normalizes any: prefix for merge correctness after YAML roundtrip"
  - "World identity detection by Identity==2 OR reserved:world label (defense in depth)"
  - "CIDR rules ordered before endpoint selector rules in generated policies"

patterns-established:
  - "World identity detection: isWorldIdentity checks Identity field and label"
  - "CIDR rule generation: separate grouping map from endpoint selector grouping"
  - "Label normalization: strip any: prefix for comparison across YAML roundtrip boundary"

requirements-completed: [PGEN-03, DEDP-01]

duration: 6min
completed: 2026-03-08
---

# Phase 03 Plan 01: CIDR World Identity Rules and File Dedup Summary

**CIDR-based policy generation for external (world identity) traffic with file-level deduplication to skip redundant writes**

## Performance

- **Duration:** 6 min
- **Started:** 2026-03-08T20:06:24Z
- **Completed:** 2026-03-08T20:13:00Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments
- World identity flows (Identity=2, reserved:world) now produce FromCIDR/ToCIDR rules with /32 IPs instead of broken endpoint selectors
- PoliciesEquivalent function provides semantic comparison for reuse by cluster dedup (Plan 02)
- Writer skips disk write when merged policy YAML matches existing file byte-for-byte
- Fixed pre-existing merge bug where any: prefix from YAML roundtrip caused duplicate rules

## Task Commits

Each task was committed atomically:

1. **Task 1: CIDR rules for world identity + semantic dedup comparison**
   - `cf3c346` (test: failing tests for CIDR and dedup)
   - `53172cc` (feat: CIDR rules and PoliciesEquivalent implementation)
2. **Task 2: File-based deduplication in writer**
   - `936dd4c` (test: failing tests for writer dedup)
   - `056dbf1` (feat: writer dedup + merge label normalization fix)

## Files Created/Modified
- `pkg/policy/builder.go` - isWorldIdentity, getSourceIP/getDestinationIP, CIDR rule generation in buildIngressRules/buildEgressRules
- `pkg/policy/dedup.go` - PoliciesEquivalent with normalized comparison
- `pkg/policy/dedup_test.go` - Equivalence tests (same spec, different rules, order, nil)
- `pkg/policy/builder_test.go` - World egress/ingress CIDR, mixed flows, nil IP tests
- `pkg/policy/merge.go` - matchLabelsNormalized strips any: prefix
- `pkg/policy/testdata/ingress_flow.go` - WorldEgressTCPFlow, WorldIngressTCPFlow, WorldFlowNilIP helpers
- `pkg/output/writer.go` - YAML byte comparison dedup before write
- `pkg/output/writer_test.go` - SkipEquivalentPolicy, WritesDifferentPolicy tests

## Decisions Made
- Used YAML byte comparison for file dedup (not in-memory PoliciesEquivalent) because Cilium adds any: prefix during YAML roundtrip, making in-memory comparison unreliable for serialized policies
- Fixed matchEndpoints in merge.go to normalize any: prefix -- this was a pre-existing bug that caused duplicate rules on re-merge after YAML roundtrip
- World identity detected by both Identity==2 and reserved:world label for robustness
- CIDR rules placed before endpoint selector rules in output for consistent ordering

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fixed merge label prefix normalization**
- **Found during:** Task 2 (File-based dedup)
- **Issue:** After YAML roundtrip, Cilium adds `any:` prefix to MatchLabels keys. MergePolicy's matchEndpoints compared raw keys, causing duplicate rules on re-merge of the same policy.
- **Fix:** Added matchLabelsNormalized/normalizeLabels to strip `any:` prefix before comparison
- **Files modified:** pkg/policy/merge.go
- **Verification:** All existing merge tests pass, new writer dedup tests pass
- **Committed in:** 056dbf1

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Fix was necessary for correct dedup behavior. Without it, every re-merge after YAML roundtrip would duplicate rules. No scope creep.

## Issues Encountered
None beyond the deviation above.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- PoliciesEquivalent exported and ready for cluster-level dedup in Plan 02
- CIDR rules integrate cleanly with existing merge logic (merge now handles any: prefix correctly)
- All 29 tests pass across policy and output packages with race detector

---
*Phase: 03-production-hardening*
*Completed: 2026-03-08*
