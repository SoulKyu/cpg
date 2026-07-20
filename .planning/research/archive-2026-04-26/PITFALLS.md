# Pitfalls Research

**Domain:** Adding drop-reason classification + cluster-health surfacing to existing cpg flow-processing pipeline (v1.3)
**Researched:** 2026-04-26
**Confidence:** HIGH (verified against Cilium v1.19.1 protobuf source, existing cpg aggregator pattern, codebase audit)

> **Scope note.** This file is v1.3-specific. v1.2 pitfalls (L7 visibility, HTTP anchoring, DNS companion rules) are archived at `.planning/research/archive-2026-04-25/PITFALLS.md`. The prior research remains canonical for those topics.

---

## Critical Pitfalls

### Pitfall 1: UNKNOWN_REASON defaults to policy bucket — regenerates the original v1.2 bug class

**What goes wrong:**
The classifier receives a flow whose `DropReasonDesc` is `DROP_REASON_UNKNOWN` (numeric 0) or a numeric value not present in the taxonomy map. If the fallback is `CategoryPolicy`, cpg generates a CNP for it — exactly the regression we are fixing. The trigger was `CT_MAP_INSERTION_FAILED` (155) producing a bogus `cpg-mmtro-adserver` CNP; the same bug fires for any reason not in the map.

**Why it happens:**
Developers write `switch reason { case policy: ... default: policy }` as a natural fallback, not realising that "unknown" means "we don't know, do NOT generate a policy". The safe interpretation of unknown is: skip policy generation AND record to cluster-health.

**How to avoid:**
- Default bucket for all unrecognised numeric values MUST be `CategoryUncategorized` (or a distinct `CategoryUnknown`), never `CategoryPolicy`.
- `DROP_REASON_UNKNOWN` (0) must be explicitly mapped to `CategoryUncategorized`, not inferred from a zero-value Go constant that could silently fall through.
- The classifier must emit a `WARN` log on every first-occurrence of an unrecognised value: `"unrecognised Cilium drop reason — treating as infra/uncategorized; update classifier taxonomy: <numeric_value>"`.
- Include a unit test: `classifyReason(DROP_REASON_UNKNOWN) == CategoryUncategorized`, `classifyReason(9999) == CategoryUncategorized` (future Cilium value).

**Warning signs:**
- CNPs appearing for `CT_MAP_INSERTION_FAILED`, `IP_FRAGMENTATION_NOT_SUPPORTED`, or any infra reason.
- `cluster-health.json` shows zero entries while `policies/` grows with unexpected files.
- No WARN log lines mentioning "unrecognised drop reason".

**Phase to address:**
Phase 1 (classifier core) — must be correct before any pipeline wiring. The test for unknown-reason fallback must be part of the Phase 1 acceptance gate, not a later review.

---

### Pitfall 2: Cilium adds new DropReason enum values silently across minor versions

**What goes wrong:**
Cilium has added ~15 new `DropReason` values between 1.14 and 1.19 (e.g. `DROP_HOST_NOT_READY` = 202, `DROP_EP_NOT_READY` = 203, `DROP_NO_EGRESS_IP` = 204 appeared in recent minor versions). When cpg is built against Cilium 1.19.1 and run against a cluster on 1.20+, protobuf will deserialise unrecognised enum values as `DROP_REASON_UNKNOWN` (0) on wire — this is protobuf's forward-compat rule for enums. The classifier then sees 0, which maps to `CategoryUncategorized` (safe), BUT the actual reason string may be in `DropReasonDesc`'s string representation or a companion text field.

**Why it happens:**
Cilium uses protobuf enum extensions. New values added upstream are valid wire integers but not present in the compiled Go stubs until the dependency is bumped. The numeric zero fallback is silent — there is no protobuf decode error, no panic, just a loss of classification fidelity.

**How to avoid:**
- Never classify by string name alone (brittle across versions). Classify by the `DropReasonDesc` int32 numeric value — protobuf guarantees numeric stability even when names are unavailable.
- Maintain a `classifierVersion` constant in the taxonomy file, bump it when new reasons are added. Log it in session summary.
- At startup (or on first unrecognised value), log: `"classifier taxonomy built against Cilium <version> — flows with newer drop reasons will be reported as uncategorized"`.
- Add a CI job that diffs the taxonomy map against the latest Cilium release's proto file and fails if new values are found. This can be a simple `go generate` script.

**Warning signs:**
- Cluster-health counter shows high volume of `category=uncategorized`.
- Users report that `cpg` stopped generating policies after a Cilium upgrade (all reasons landing in uncategorized).
- `WARN unrecognised Cilium drop reason` appearing frequently in logs.

**Phase to address:**
Phase 1 (classifier) — add `classifierVersion` and the WARN log. Phase 4 (session summary) — surface uncategorized count so operators notice. CI job is a post-ship follow-up.

---

### Pitfall 3: Cluster-health alerts buried in log stream — invisible to operators scanning for CNPs

**What goes wrong:**
cpg's primary output is YAML files. Operators typically run `cpg generate ... 2>/dev/null` or redirect stderr to a file, then inspect `policies/`. An infra alert emitted as a `zap.Warn` line at T+30s scrolls off the terminal. The operator sees the policies (the "real" output) and never acts on the cluster-health information.

**Why it happens:**
cpg was designed as a code generator, not a monitoring tool. Adding health alerts to the same log stream as policy-generation noise creates information-density competition. The alert wins only if it's visually distinct and structurally different from normal log output.

**How to avoid:**
- Write a separate `cluster-health.json` file alongside policies that is machine-readable and persists after the session ends.
- Print a structured session-summary block to stdout (separate from zap/stderr logs) at shutdown, similar to how `kubectl` prints summaries. Use `fmt.Fprintf(os.Stdout, ...)` not `logger.Warn(...)` for the final infra-drops banner.
- The banner must be impossible to miss: `\n=== CLUSTER HEALTH ISSUES DETECTED ===\n`, with counts and the path to `cluster-health.json`.
- Do NOT suppress this banner in `--dry-run` mode — dry-run is explicitly a preview, not a silence mode.

**Warning signs:**
- In user testing: no one mentions infra drops even though they occurred.
- `cluster-health.json` has entries but no ticket was opened.
- Users ask "cpg generated a weird policy for CT_MAP_INSERTION_FAILED" (means they never saw the warning).

**Phase to address:**
Phase 3 (cluster-health.json writer) + Phase 4 (session summary banner). The banner is not optional polish — it is the primary UX mechanism that prevents the alert from being ignored.

---

### Pitfall 4: Exit code change breaks existing automations without opt-in gate

**What goes wrong:**
Every existing cpg CI pipeline, cron job, and post-hook that tests `if cpg generate ...; then apply; fi` has been written assuming `exit 0` = success, `exit 1` = connection/config error. If infra drops now cause `exit 2` (or any non-zero) by default, every automation breaks silently on the first session that observes infra drops — which is every prod session with any cluster stress.

**Why it happens:**
Feature authors think "exit non-zero on serious issues is natural" without auditing the downstream contract. In a code generator, exit 0 has always meant "generation succeeded, files are valid". Infra drops do not invalidate the generated policies.

**How to avoid:**
- `--fail-on-infra-drops` is already scoped as opt-in. ENFORCE: default is always exit 0 when policies generated successfully, regardless of infra drop count.
- Never add a new exit code without a flag gate. The flag must be documented with the exact exit code it produces (`exit 2` for infra drops, distinct from `exit 1` for errors).
- In the session summary banner, print: `"Use --fail-on-infra-drops to exit with code 2 when infra drops are detected"` — this surfaces the opt-in to users who need it.
- Document in CHANGELOG and README migration guide: "v1.3 introduces exit code 2, opt-in only via `--fail-on-infra-drops`. Default exit behavior unchanged."

**Warning signs:**
- Any PR adding `os.Exit` with a new code without an associated flag-gate check.
- Test suite not covering `--fail-on-infra-drops` off-by-default behaviour.
- CI test asserting `exit 0` failing after v1.3 merge.

**Phase to address:**
Phase 5 (--fail-on-infra-drops flag). The off-by-default invariant must be a unit test: run pipeline with infra drops, no flag set → assert `RunPipeline` returns `nil`.

---

### Pitfall 5: cluster-health.json committed to git as a policy artifact

**What goes wrong:**
Users run `cpg generate -o ./policies/` and then `git add ./policies/`. If `cluster-health.json` lives in `./policies/`, it gets committed. The file is session state (volatile, changes every run), not a policy artifact (stable, reviewed, versioned). Git history fills up with health snapshots; diffs become noisy; `git blame` on policies becomes unreliable.

**Why it happens:**
Putting health output next to policy output is the path of least resistance. Developers conflate "output" with "output directory".

**How to avoid:**
- `cluster-health.json` must NOT live in `--output-dir`. Options ranked by preference:
  1. **Evidence dir** (`$XDG_CACHE_HOME/cpg/evidence/...`): consistent with where session state already lives (evidence JSON). Accessible via `cpg explain`. Not committed. XDG-compliant. RECOMMENDED.
  2. **Separate `--health-output` flag**: gives operators explicit control, but adds surface area.
  3. **Current working directory**: bad — pollutes project root, easy to commit.
- Add `cluster-health.json` to `.gitignore` template in README, and emit a WARN if the file would be written into a directory that appears to be under git version control (heuristic: `git rev-parse --is-inside-work-tree` returns true AND path is not in `.gitignore`).
- Document clearly: "cluster-health.json is session state, not a policy artifact. It lives in the evidence directory, not alongside policies."

**Warning signs:**
- `cluster-health.json` appearing in `git status` output.
- User PR adding `cluster-health.json` to the policies directory.
- Repeated entries in `git log --name-only` for `policies/cluster-health.json`.

**Phase to address:**
Phase 3 (cluster-health.json writer) — location decision must be made before the first line of writer code, not as a post-ship fix.

---

### Pitfall 6: Counter increment / flow skip race leading to undercounting in summary

**What goes wrong:**
The aggregator runs in a single goroutine (no concurrent flow processing), so there is no data-race risk on counters in the current architecture. However, the proposed classifier introduces a new code path: if the classifier check (which may skip the flow) occurs *before* the `a.flowsSeen++` increment, flows classified as infra/transient are never counted in `flowsSeen`. The session summary then shows `flows_seen=42` when 100 flows arrived, with 58 silently vanished.

**Why it happens:**
The `--ignore-protocol` pattern increments `ignoredByProtocol[name]++` and then `continue`s before `flowsSeen++`. This is correct because those flows are intentionally excluded. But infra/transient drops are a different category: they ARE seen, they ARE counted, they just don't generate CNPs. The distinction matters for operators diagnosing cluster health.

**How to avoid:**
- Define clear counting semantics in a comment before `Run()`:
  ```
  // flowsSeen: every flow that reaches keyFromFlow(), regardless of category.
  // ignoredByProtocol: flows skipped BEFORE flowsSeen (intentionally excluded).
  // classifiedInfra / classifiedTransient: flows counted IN flowsSeen but routed
  //   to health accounting instead of policy generation.
  ```
- Increment `flowsSeen` BEFORE the classifier branch, not after policy routing.
- Separate counters: `infraDrops uint64`, `transientDrops uint64` — surfaced in SessionStats alongside `flowsSeen`.
- Unit test: send 5 policy flows + 3 infra flows → assert `flowsSeen=8`, `infraDrops=3`, policies generated=5.

**Warning signs:**
- `flows_seen` in session summary equals `policies_generated` exactly (suspiciously clean).
- Infra drop counters are always 0 even when `cluster-health.json` has entries.
- Test covering the counter invariant is missing.

**Phase to address:**
Phase 2 (aggregator wiring) — counter semantics must be specified in the Phase 2 acceptance criteria.

---

### Pitfall 7: --ignore-drop-reason silently no-ops when reason is already infra-suppressed

**What goes wrong:**
An operator passes `--ignore-drop-reason CT_MAP_INSERTION_FAILED`. The classifier already routes this to `CategoryInfra` (suppressed from CNP generation by default). The `--ignore-drop-reason` filter then has no observable effect. The operator gets no feedback. They conclude the flag is broken, file a bug, or worse, assume the behaviour changed.

**Why it happens:**
The filter and the classifier operate at different levels. The filter is "I want this reason completely invisible (no health count either)". The classifier is "route this reason to the correct output channel". When the user intent is "stop generating CNPs for this", the classifier already handles it — the flag is redundant and confusing.

**How to avoid:**
- At flag parse time, validate each `--ignore-drop-reason` value against the classifier taxonomy. If the reason is already in `CategoryInfra` or `CategoryTransient` (not in `CategoryPolicy`), emit a `WARN`:
  ```
  WARN  --ignore-drop-reason CT_MAP_INSERTION_FAILED has no effect: this reason is
        already classified as 'infra' and does not generate policies. Use this flag
        only to suppress reasons classified as 'policy' or 'uncategorized'.
  ```
- Document the semantics: `--ignore-drop-reason` suppresses from ALL output (including cluster-health.json), unlike the classifier which routes non-policy reasons to health accounting.
- Add a unit test for the warning path.

**Warning signs:**
- User opens issue: "my --ignore-drop-reason flag doesn't work".
- No WARN log when passing an infra-classified reason.
- Flag documentation only says "suppress from output" without explaining interaction with classifier.

**Phase to address:**
Phase 5 (--ignore-drop-reason flag) — validation warning must be in the flag parsing, not the aggregator.

---

### Pitfall 8: cpg replay on old jsonpb capture with different Cilium version floods uncategorized warnings

**What goes wrong:**
An operator runs `cpg replay old-capture-cilium-1.14.json`. The capture was taken against Cilium 1.14 where some drop reasons had different numeric codes or names. Protobuf deserialises known numerics correctly (numerics are stable in Cilium's proto history), but if the capture contains raw integer drop_reason values that are in gaps in the enum (e.g. 159 is unassigned in 1.19), the classifier emits a WARN for each such flow. If the capture has 10k flows, 10k WARNs appear.

**Why it happens:**
The `--ignore-protocol` pattern emits one-shot warnings via `warnedReserved` dedup. The drop-reason classifier may not have an equivalent dedup gate, leading to log spam in replay mode.

**How to avoid:**
- The "unrecognised drop reason" WARN must be deduped per unique numeric value, exactly as `warnedReserved` deduplicates per reserved-identity+direction. Use a `warnedUnknownReason map[int32]struct{}` in the Aggregator.
- In replay mode (`cpg replay`), add a session-end summary: `"N flows had unrecognised drop reasons (values: 159, 161): consider updating cpg to a version built against a newer Cilium release"`.
- Document in README: `cpg replay` against captures from a different Cilium version may produce higher uncategorized counts than a live run.

**Warning signs:**
- `cpg replay old.jsonpb` produces hundreds of WARN lines for the same drop reason numeric value.
- Session summary uncategorized count is unexpectedly high on replay vs. live runs.

**Phase to address:**
Phase 1 (classifier) — the `warnedUnknownReason` dedup must be designed alongside the classifier's WARN, not added later. Phase 3 (session summary) — the per-reason uncategorized count must be in the summary.

---

### Pitfall 9: Remediation hint URLs in cluster-health.json become stale or dead

**What goes wrong:**
`cluster-health.json` includes `remediation_hint` fields pointing to Cilium documentation. Cilium reorganises their docs with major releases. A URL valid for Cilium 1.19 (`https://docs.cilium.io/en/v1.19/...`) 404s on a cluster running 1.21.

**Why it happens:**
Hard-coded version-pinned URLs in code are a maintenance burden that accumulates silently. Operators click the link, get a 404, and lose trust in the tool.

**How to avoid:**
- All hint URLs must be **version-unqualified or redirect-stable**. Use `https://docs.cilium.io/en/stable/...` (stable redirects to latest) or anchor the URL in a cpg-controlled redirector (`https://github.com/SoulKyu/cpg/wiki/...`).
- Confirm: hints are static strings in the classifier taxonomy map in code — they are NOT user-influenceable. The security risk of user-controlled links in JSON output is zero here; the risk is only staleness.
- Add a CI job (or release checklist item) to check that all hint URLs return HTTP 200.

**Warning signs:**
- 404s when clicking hint links after a Cilium major release.
- Hint URLs containing version strings like `/en/v1.19/`.

**Phase to address:**
Phase 1 (classifier taxonomy) — URL format decision made when writing the taxonomy map. Use `stable` immediately, never pin to a version.

---

### Pitfall 10: Classifier called per-flow with O(n) iteration instead of O(1) map lookup

**What goes wrong:**
If the classifier is implemented as a `switch` statement or a slice scan (`for _, entry := range taxonomy`), it is O(n) where n = number of classified reasons (currently ~75 entries). With Hubble streaming at 1000+ flows/sec under cluster stress, the classifier becomes the hot path in the aggregator goroutine, adding measurable latency.

**Why it happens:**
A `switch` is the natural Go pattern for enum dispatch. It is O(n) in compiled form unless the compiler optimises it to a jump table (which Go does for consecutive integer ranges, but the Cilium DropReason enum starts at 130 with gaps, so no jump table). A `map[DropReason]Category` is O(1) at the cost of map initialisation at startup.

**How to avoid:**
- Use `var reasonCategory = map[flowpb.DropReason]Category{ ... }` initialised once at package init.
- Classifier function: `func ClassifyReason(r flowpb.DropReason) Category { if c, ok := reasonCategory[r]; ok { return c }; return CategoryUncategorized }`.
- This is a single map lookup per flow — effectively free.
- Do NOT use `switch` for the main classification path. `switch` is acceptable for the human-readable category-to-string mapping (3 cases).

**Warning signs:**
- Classifier implemented as `switch r { case CT_MAP_INSERTION_FAILED: ... default: ... }`.
- Performance regression detectable in `cpg generate` under load (aggregator goroutine CPU spike).
- Missing benchmark test for classifier.

**Phase to address:**
Phase 1 (classifier) — the map-based design must be specified before implementation. A `BenchmarkClassifyReason` with 1M iterations should be in the Phase 1 acceptance criteria.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Hardcode all 75 DropReason entries in one Go file | Simple, no code gen | Must be manually updated on each Cilium bump; silent misclassification if missed | Acceptable for v1.3; add `go generate` script in v1.4 |
| Use `CategoryUncategorized` for all new/unknown reasons | Safe default, no false positives | High uncategorized count on Cilium upgrades until taxonomy is updated | Always acceptable — it is the correct safe default |
| Write cluster-health.json to evidence dir only | Consistent with existing evidence pattern | Less discoverable than output-dir | Acceptable; README and session summary banner must point to the path |
| Single in-memory map for health counters | No persistence overhead | Lost on crash mid-session | Acceptable; evidence dir write at shutdown is sufficient for operator use |
| Static remediation hints (no dynamic lookup) | Zero latency, no network calls | Hints can go stale with Cilium version changes | Acceptable; use `stable` URLs and add CI link-check |

---

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| Cilium DropReason proto enum | Classify by string name from `DropReason_name` map | Classify by numeric `int32` value — names are generated Go constants, numerics are the stable protobuf contract |
| `flowpb.Flow.DropReasonDesc` field | Assume it is always populated | Field is `optional`; `DROP_REASON_UNKNOWN` (0) is the zero value and appears on non-drop flows too; always check `Verdict == DROPPED` first |
| `Flow.drop_reason` (field 3) vs `Flow.drop_reason_desc` (field 25) | Use the deprecated `uint32 drop_reason` field | Use `DropReasonDesc` (field 25, typed `DropReason` enum) — `drop_reason` is deprecated since Cilium 1.11 |
| cobra exit code with `RunE` | Return `fmt.Errorf(...)` from `RunE` to signal infra drops | cobra converts any non-nil error to `exit 1`; for `exit 2` (infra drops) use `os.Exit(2)` in `RunE` after `RunPipeline` returns, guarded by the `--fail-on-infra-drops` flag |
| `--ignore-drop-reason` with comma-separated values | Use `StringSlice` flag | Use `StringSlice` (same as `--ignore-protocol`) — `StringSlice` handles both `--flag a,b` and `--flag a --flag b` |

---

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| O(n) classifier in aggregator hot path | CPU spike in aggregator goroutine; latency visible in flow-to-policy pipeline | `map[DropReason]Category` initialised at package init | Under load >1000 flows/sec (prod cluster stress events) |
| Per-flow `cluster-health.json` flush | I/O spike; disk writes on every flow | Accumulate in memory; write once at session end (same as evidence JSON) | Any prod session |
| Holding all health counters in a single lock-protected struct | Lock contention if health writer is in a separate goroutine | Aggregator is single-goroutine; no lock needed unless health writer is promoted to a goroutine | Only if architecture changes to concurrent health writing |

---

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Remediation hint URLs are user-influenceable | Open redirect / phishing if written to JSON and opened by operator | Hints are static strings in code, never derived from flow data or user input — confirmed safe |
| `cluster-health.json` written to `--output-dir` | Accidental commit of session state exposing cluster topology (drop reason by node + workload) | Write to evidence dir (XDG_CACHE_HOME), not output dir; document clearly |
| Node names and workload names in `cluster-health.json` | File may be committed to a public repo | Same mitigation as above; README must warn this file contains cluster topology data |

---

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Health alerts only in log stream | Operators miss infra issues; no action taken | Dedicated `cluster-health.json` + shutdown banner on stdout (`fmt.Fprintf`, not logger) |
| `--fail-on-infra-drops` enabled by default | All existing CI pipelines break silently | Opt-in only; default exit 0; document the flag prominently |
| Health output mixed with policy output in same dir | Accidental git commit of session state | Evidence dir; `.gitignore` template in README |
| No hint when `--ignore-drop-reason` flag is redundant | User thinks flag is broken | WARN at flag parse time if reason is already non-policy |
| Uncategorized count in summary with no remediation path | Operator sees count, doesn't know what to do | Summary should print: "N uncategorized drops: run with `--debug` to see numeric reason codes, then report at github.com/SoulKyu/cpg/issues" |

---

## "Looks Done But Isn't" Checklist

- [ ] **Classifier fallback:** Verify `classifyReason(0) == CategoryUncategorized` (not CategoryPolicy) in unit test.
- [ ] **Classifier fallback:** Verify `classifyReason(9999) == CategoryUncategorized` (future Cilium value) in unit test.
- [ ] **Flow counting:** Verify `flowsSeen` includes infra/transient drops, not just policy-routed flows.
- [ ] **Exit code:** Verify `RunPipeline` returns `nil` (exit 0) when infra drops observed but `--fail-on-infra-drops` not set.
- [ ] **cluster-health.json location:** Verify file is NOT written inside `--output-dir`.
- [ ] **cluster-health.json dry-run:** Verify file is NOT written when `--dry-run` is set (consistent with evidence/policy writer dry-run semantics from v1.1).
- [ ] **Session summary banner:** Verify banner is printed to stdout (not logger) so it appears even when stderr is redirected.
- [ ] **--ignore-drop-reason warning:** Verify WARN is emitted when user passes an already-infra-classified reason.
- [ ] **Unknown reason dedup:** Verify that 10k replay flows with the same unrecognised reason produce exactly 1 WARN log line (not 10k).
- [ ] **Deprecated field:** Verify classifier reads `Flow.DropReasonDesc` (field 25), not the deprecated `Flow.drop_reason` (field 3).

---

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Wrong default bucket (policy instead of uncategorized) | HIGH — must revert; bogus CNPs may have been applied | Roll back, add `DELETE_IF_EMPTY` migration for affected CNPs, add regression test |
| cluster-health.json committed to git | LOW | Add to `.gitignore`, `git rm --cached cluster-health.json`, amend history if needed |
| Exit code change breaks CI | MEDIUM | Revert to exit 0 default, re-release; document `--fail-on-infra-drops` as migration path |
| Stale remediation URLs | LOW | Update taxonomy map URLs, re-release; no user data affected |
| Flood of WARN logs in replay | LOW | Add dedup gate, re-release; cosmetic issue only |

---

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| UNKNOWN_REASON → policy bucket regression | Phase 1 (classifier) | Unit test: `classifyReason(0) == CategoryUncategorized` |
| Cilium enum versioning — new values land as uncategorized | Phase 1 (classifier + WARN log) | Unit test: `classifyReason(9999)` + WARN emission test |
| Health alerts invisible in log stream | Phase 3 (writer) + Phase 4 (banner) | Manual review: banner must appear on stdout even with `2>/dev/null` |
| Exit code breaks automations | Phase 5 (flag) | Unit test: exit 0 default without flag; exit 2 with flag |
| cluster-health.json committed to git | Phase 3 (writer) | File path assertion: must be under evidence dir, not output dir |
| Counter skip leading to undercounting | Phase 2 (aggregator) | Unit test: 5 policy + 3 infra flows → `flowsSeen=8`, `infraDrops=3` |
| --ignore-drop-reason redundant flag confusion | Phase 5 (flag) | Unit test: passing infra-classified reason emits WARN |
| Replay flood of unknown-reason WARNs | Phase 1 (classifier dedup) | Test: 10k identical-reason flows → exactly 1 WARN |
| Stale remediation hint URLs | Phase 1 (taxonomy) | Use `stable` URL prefix; CI link-check job |
| O(n) classifier performance | Phase 1 (classifier) | Benchmark: `BenchmarkClassifyReason` must show O(1) |

---

## Exhaustive Cilium Drop Reason Classification Reference

Enumerated from `github.com/cilium/cilium@v1.19.1/api/v1/flow/flow.pb.go`. Provides the authoritative ground truth for the v1.3 taxonomy map. New values in future Cilium versions will appear as `CategoryUncategorized` until the map is updated.

**CategoryPolicy (generate CNPs):**
- `POLICY_DENIED` (133) — L3/L4 policy deny
- `POLICY_DENY` (181) — policy deny (newer alias)
- `AUTH_REQUIRED` (189) — mutual auth / mTLS required
- `NO_CONFIGURATION_AVAILABLE_TO_PERFORM_POLICY_DECISION` (165) — policy engine not ready

**CategoryInfra (record in cluster-health, no CNP):**
- `CT_MAP_INSERTION_FAILED` (155) — conntrack map full (the v1.3 trigger bug)
- `CT_TRUNCATED_OR_INVALID_HEADER` (135)
- `CT_MISSING_TCP_ACK_FLAG` (136)
- `CT_UNKNOWN_L4_PROTOCOL` (137)
- `CT_CANNOT_CREATE_ENTRY_FROM_PACKET` (138)
- `CT_NO_MAP_FOUND` (190)
- `SNAT_NO_MAP_FOUND` (191)
- `ERROR_WRITING_TO_PACKET` (141)
- `ERROR_WHILE_CORRECTING_L3_CHECKSUM` (153)
- `ERROR_WHILE_CORRECTING_L4_CHECKSUM` (154)
- `ERROR_RETRIEVING_TUNNEL_KEY` (147)
- `ERROR_RETRIEVING_TUNNEL_OPTIONS` (148)
- `MISSED_TAIL_CALL` (140)
- `FAILED_TO_INSERT_INTO_PROXYMAP` (161)
- `FIB_LOOKUP_FAILED` (169)
- `LOCAL_HOST_IS_UNREACHABLE` (164)
- `NO_TUNNEL_OR_ENCAPSULATION_ENDPOINT` (160)
- `SOCKET_LOOKUP_FAILED` (178)
- `SOCKET_ASSIGN_FAILED` (179)
- `DROP_HOST_NOT_READY` (202)
- `DROP_EP_NOT_READY` (203)
- `DROP_NO_EGRESS_IP` (204)

**CategoryTransient (datapath/packet issues, no CNP, low severity):**
- `INVALID_SOURCE_IP` (132) — spoofed/invalid source (ephemeral)
- `INVALID_SOURCE_MAC` (130)
- `INVALID_DESTINATION_MAC` (131)
- `INVALID_PACKET_DROPPED` (134)
- `STALE_OR_UNROUTABLE_IP` (151)
- `IP_FRAGMENTATION_NOT_SUPPORTED` (157)
- `UNKNOWN_L4_PROTOCOL` (142)
- `UNKNOWN_L3_TARGET_ADDRESS` (150)
- `UNSUPPORTED_L3_PROTOCOL` (139)
- `UNSUPPORTED_L2_PROTOCOL` (166)
- `UNKNOWN_ICMPV4_CODE` (143), `UNKNOWN_ICMPV4_TYPE` (144)
- `UNKNOWN_ICMPV6_CODE` (145), `UNKNOWN_ICMPV6_TYPE` (146)
- `UNKNOWN_CONNECTION_TRACKING_STATE` (163)
- `FORBIDDEN_ICMPV6_MESSAGE` (176)
- `TTL_EXCEEDED` (196)
- `REACHED_EDT_RATE_LIMITING_DROP_HORIZON` (162)
- `DROP_RATE_LIMITED` (198)
- `INVALID_IPV6_EXTENSION_HEADER` (156)
- `INVALID_TC_BUFFER` (184)
- `INVALID_GENEVE_OPTION` (149)
- `INVALID_VNI` (183)
- `ENCAPSULATION_TRAFFIC_IS_PROHIBITED` (170)
- `INVALID_CLUSTER_ID` (192)
- `UNENCRYPTED_TRAFFIC` (195)
- `PROXY_REDIRECTION_NOT_SUPPORTED_FOR_PROTOCOL` (180)
- `DROP_PUNT_PROXY` (205)

**Ambiguous / needs operator decision:**
- `SERVICE_BACKEND_NOT_FOUND` (158) — could be infra (pod not ready) or mis-configuration (wrong CNP selector); default `CategoryInfra`, document that operators may reclassify
- `NO_MATCHING_LOCAL_CONTAINER_FOUND` (152) — routing table issue, `CategoryInfra`
- `UNKNOWN_SENDER` (172) — unknown source identity, `CategoryTransient`
- `DENIED_BY_LB_SRC_RANGE_CHECK` (177) — LoadBalancer source range policy, `CategoryPolicy`
- `NO_EGRESS_GATEWAY` (194) — missing egress gateway config, `CategoryInfra`
- `IGMP_HANDLED` (199), `IGMP_SUBSCRIBED` (200), `MULTICAST_HANDLED` (201) — control plane housekeeping, `CategoryTransient`
- `NAT_NOT_NEEDED` (173), `NAT46` (187), `NAT64` (188) — NAT translation drops, `CategoryTransient`
- `NO_SID` (185), `MISSING_SRV6_STATE` (186) — SRv6 segment routing, `CategoryInfra`
- `VLAN_FILTERED` (182) — VLAN config, `CategoryInfra`
- `IS_A_CLUSTERIP` (174) — routing bypass, `CategoryTransient`
- `FIRST_LOGICAL_DATAGRAM_FRAGMENT_NOT_FOUND` (175) — fragmentation, `CategoryTransient`
- `UNSUPPORTED_PROTOCOL_FOR_NAT_MASQUERADE` (168), `NO_MAPPING_FOR_NAT_MASQUERADE` (167) — NAT, `CategoryTransient`
- `UNSUPPORTED_PROTOCOL_FOR_DSR_ENCAP` (193) — DSR, `CategoryInfra`

**CategoryUncategorized (safe fallback):**
- `DROP_REASON_UNKNOWN` (0) — protobuf zero value / unset
- Any numeric value not in the above map

Approximate real-prod fraction: based on the enum analysis, ~2 out of 75 values are pure policy (`POLICY_DENIED`, `POLICY_DENY`). `AUTH_REQUIRED` and `DENIED_BY_LB_SRC_RANGE_CHECK` are policy-adjacent. The remaining ~71 values (94%) are infrastructure or transient. Under steady-state prod traffic, policy drops dominate in default-deny environments, but during cluster stress events (OOM, scale-out, rolling restarts), infra drops can briefly outnumber policy drops.

---

## Sources

- Cilium v1.19.1 protobuf enum: `/home/gule/go/pkg/mod/github.com/cilium/cilium@v1.19.1/api/v1/flow/flow.pb.go` (direct inspection)
- cpg aggregator pattern: `/home/gule/Workspace/team-infrastructure/cpg/pkg/hubble/aggregator.go` — `--ignore-protocol` as the reference for filter + counter + dedup WARN
- cpg reasons: `/home/gule/Workspace/team-infrastructure/cpg/pkg/policy/reasons.go` — existing UnhandledReason enum pattern
- cpg pipeline: `/home/gule/Workspace/team-infrastructure/cpg/pkg/hubble/pipeline.go` — fan-out, SessionStats, exit code contract
- cpg main: `/home/gule/Workspace/team-infrastructure/cpg/cmd/cpg/main.go` — single `os.Exit(1)` call confirming exit-0-on-success contract
- cpg PROJECT.md Key Decisions table — v1.1 evidence dir (XDG), v1.1 FlowSource, v1.2 warn-and-proceed exit code precedent

---
*Pitfalls research for: cpg v1.3 — Cluster Health Surfacing*
*Researched: 2026-04-26*
