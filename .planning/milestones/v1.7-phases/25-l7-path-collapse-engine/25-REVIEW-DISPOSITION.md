---
phase: 25
date: 2026-10-04
source: 25-REVIEW.md
counts:
  blocker: 0
  warning: 1
  info: 3
---

# Phase 25 Review Dispositions

| Finding | Severity | Disposition | Notes |
|---------|----------|-------------|-------|
| R-01 | WARNING | open | Cross-run narrowing semantics (replace-merge vs prior-run/hand-edited HTTP rules). Doc-or-gate decision belongs to the operator: document in KNOWN_LIMITATIONS (#13 candidate) or gate replace to cpg-managed buckets. Not a must_haves violation (Decision 1 pinned cross-flush only); flagged for follow-up before the v1.7 release docs pass. |
| R-02 | INFO | acknowledged | Inherent within-class widening, never a reduction; by-design. |
| R-03 | INFO | open | Encoded-path explain divergence untested; one-line test addition candidate for gap closure / next docs pass. |
| R-04 | INFO | acknowledged | Bounded, self-healing, documented behavior note. |
