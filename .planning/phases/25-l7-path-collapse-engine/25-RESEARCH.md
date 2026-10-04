# Research: Phase 25 — L7 Path Collapse Engine (planning input)

## Summary
Phase 25 is buildable as a deterministic, segment-wise generalizer inside `pkg/policy` (new file, e.g. `l7_collapse.go`), plugged into the existing per-(port, method) HTTP rule emission, gated by two new flags plumbed exactly like `include_audit`. The two genuinely hard planning problems are (1) **where the collapse pass sees "all observed paths"**: the Aggregator clears its buckets on every flush (default 5s), so a collapse inside `BuildPolicy` only sees one flush window — a live run where each window sees 3 new user IDs would never reach a threshold of 10 distinct paths even though the run saw 10k; and (2) **merge-on-write reconciliation**: `MergePolicy` union-merges HTTP rule lists across flushes, so a template emitted late coexists with literal rules already written to disk unless collapse output is reconciled (or emitted only at finalize). Prior art (SkyAPM/R3, grafana/clusterurl, Beyla) validates segment-wise variance-only generalization and conservative min-sample gates; no production tool infers Cilium regexes, so cpg writes its own small classifier — which fits the anti-hallucination stance.

## Findings

### A. Algorithm: segment-wise generalization is the validated prior art; cpg needs only a tiny deterministic slice of it

1. **Claim:** The standard approach for collapsing high-cardinality URL sets is a per-segment (or trie-per-level) grouping where segments with observed variance become placeholders, and ID-shaped segments are classified by cheap regexes (numeric, hex, UUID, IP). R3/SkyWalking (Drain3-derived URIDrain), grafana/clusterurl (used by OpenTelemetry eBPF instrumentation/Beyla), VictoriaLogs' `collapse_nums`, and Elchi's API discovery all converge on this shape. None of them emit regexes for policy — they emit `{var}`/`*` placeholders for analytics grouping, so cpg's "generalize to RE2 character classes" output is novel and must be its own code.
   **Sources:** [SkyAPM/R3 README](https://github.com/SkyAPM/r3), [grafana/clusterurl](https://github.com/grafana/clusterurl), [VictoriaLogs issue #981](https://github.com/VictoriaMetrics/VictoriaLogs/issues/981), [Elchi path normalization](https://www.elchi.io/docs/api-discovery/path-normalization), [dpep/iriq](https://github.com/dpep/iriq). **Support:** direct evidence (READMEs/issue text read in full). **Confidence:** high.

2. **Claim:** R3's documented heuristics map almost 1:1 onto L7GEN-02's "variance-only generalization" and L7GEN-04's gate:
   - Only parametrize a segment when **sample count ≥ threshold** (`combine_min_url_count`; server default `DRAIN_ANALYSIS_MIN_URL_COUNT=20`) — the same idea as `--min-flows-per-l7-rule`.
   - Do **not** parametrize word-like segments (`/api/sale`, `/api/product_sale` stay literal) — supports "literal segments stay literal" and suggests word-like variance should NOT be generalized without extra evidence.
   - Do **not** parametrize API version segments (`/test/v1`, `/test/v2`, `/test/v3` stay three literals, not `/test/v<N>`) — `v1`/`v2` variance is structural, not an ID. cpg should hardcode "short version-like segment (`v` + digits) stays literal" or accept minor over-collapse risk consciously.
   - Numeric/hex/IP detection via anchored per-segment regexes; R3's masking table is a usable reference set: NUM `[+-]?\d+`, HEX `0x[a-fA-F0-9]+`, IPv4 `\d{1,3}(\.\d{1,3}){3}`, hex runs `[0-9a-f]{6,}`.
   **Sources:** [SkyAPM/R3 README](https://github.com/SkyAPM/r3), R3 `models/uri_drain/masking.py` (read from cloned repo). **Support:** direct evidence. **Confidence:** high.

3. **Claim:** A safe deterministic classifier for cpg (researcher-synthesized design from the above, not quoted from any source): group observed paths by (method, segment count); for each segment position, collect the set of observed values; only when |set| > 1 AND every value matches the **same** most-specific class do you emit that class's character class:
   - UUID `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}` (check before plain hex)
   - hex `0x…` → `0x[0-9a-fA-F]+`; bare hex run → `[0-9a-fA-F]+`
   - numeric `[0-9]+`
   - everything else (including mixed-class observations, single-observation positions, version-like `v<N>`) stays literal via `regexp.QuoteMeta`.
   Mixed classes at one position (some numeric, some words) must fall back to literal — generalizing to `[^/]+` there would be exactly the "silent generalization" the REQUIREMENTS.md Out-of-Scope section forbids. A trailing-slash variant (`/a/b` vs `/a/b/`) is a different segment count → different group → stays literal unless explicitly normalized first (planner decision: probably don't normalize; two templates is honest).
   **Support:** researcher inference from cited prior art + REQUIREMENTS.md constraints. **Confidence:** high (design), noting it is inference, not source quotation.

### B. Cilium/Envoy constraints for the emitted regexes

4. **Claim:** Cilium's `PortRuleHTTP.Path` is documented as "extended POSIX regex … (egrep/unix syntax, not the perl syntax)" and "Paths must begin with a `/`". Official examples write `path: "/public"` and `path: "/path1$"` — no leading `^`, implying Cilium anchors the start implicitly; cpg's existing explicit `^…$` (from `anchorPath`, `pkg/policy/l7.go`) remains valid and L7GEN-02 mandates keeping it. Practical constraint for the classifier: emit POSIX-ERE-safe syntax — `[0-9]`, `[0-9a-f]`, `{n}` bounded repetition are all fine; avoid `\d`/`\w` (Perl-ish), backreferences, lookarounds (RE2 has none anyway).
   **Sources:** [Cilium L7 docs](https://docs.cilium.io/en/latest/security/policy/layer7/), [Cilium CRD yaml](https://github.com/cilium/cilium/blob/main/pkg/k8s/apis/cilium.io/client/crds/v2/ciliumnetworkpolicies.yaml). **Support:** direct evidence. **Confidence:** high.

5. **Claim:** **Envoy enforces a RE2 max program size of 100 by default, and a policy whose path regex exceeds it is NACKed — leaving the endpoint's policy update unapplied** (cilium/cilium#41314, Cilium 1.17.x, still being worked 2026-06; workaround is a helm/envoy-config bump of `re2.max_program_size.error_level`). Program size grows roughly with regex complexity/length. Two planning consequences: (a) long literal paths can already be near the limit today — collapse makes rules *smaller* per rule, which is a benefit worth stating in docs; (b) the collapse engine should keep generated patterns compact (character classes + `{n}` are cheap) and could guard with a conservative pattern-length/complexity cap, falling back to literals when a would-be template regex is too large. Note cpg cannot compute Envoy's exact program size from Go's `regexp` package (the Cilium maintainer says exactly this in the issue).
   **Sources:** [cilium/cilium#41314](https://github.com/cilium/cilium/issues/41314). **Support:** direct evidence (issue + maintainer comments). **Confidence:** high.
   Also relevant (same docs page): multiple HTTP rules on the same (port, proto) are OR-ed — "A layer 7 request is permitted if at least one of the rules matches" — so a template plus leftover literals is safe (permissive), only redundant; and "If a layer 4 rule is specified … and a similar layer 4 rule with layer 7 rules is also specified, then the layer 7 portions of the latter rule will have no effect."

### C. Where the collapse pass sits — the flush/merge interaction is the core planning decision

6. **Claim (direct code evidence):** `Aggregator.flush` (`pkg/hubble/aggregator.go`) calls `policy.BuildPolicy` over the flows accumulated **since the last ticker flush** and then `delete`s every bucket. Default flush interval is 5s. Therefore a collapse pass placed naively inside `BuildPolicy`/`portRulesFor` sees only one window's paths. A live capture where each 5s window observes 3 new IDs for `/api/v1/users/<n>` would never satisfy a `min-flows-per-l7-rule 10` gate per-window, even though the run observed 10k distinct paths — breaking success criterion 1 in live mode (replay of a single big file has the same per-window semantics).
   **Source:** direct read of `pkg/hubble/aggregator.go` (`Run`, `flush`). **Support:** direct evidence. **Confidence:** high.

7. **Claim (direct code evidence):** `MergePolicy` (`pkg/policy/merge.go`) union-merges the HTTP sub-lists of same-(port, proto) PortRules across flushes (merge-on-write in `pkg/output/writer.go`). Consequence: even if per-window collapse emits `^/api/v1/users/[0-9]+$` at flush k, literal rules written at flushes < k stay in the YAML file (dedup keys differ), and a template whose regex later widens (new segment class observed) merges as a *second* template rule. Planner options, in increasing invasiveness:
   - **(a) Finalize-time collapse (recommended to plan first):** cpg already has a run-finished contract (`OnFinal`/`SessionStats`, `Aggregator.Flush` on ctx cancel). Keep per-bucket lightweight cross-flush state — a `map[(port, method)]set[literalPath]` per (namespace, workload) instead of nothing — and run the collapse pass at flush/finalize over the **whole-run path set**, replacing (not merging with) prior per-window emissions for the same bucket. Requires the output/merge layer to know "this bucket's L7 rules are authoritative/replaced each flush".
   - **(b) Accumulate-and-rebuild:** rebuild each workload policy every flush from a retained whole-run L7 observation registry (paths are tiny; flows are still dropped). Same reconciliation problem as (a) for files on disk.
   - **(c) Per-window-only collapse:** simplest code, but demonstrably fails success criterion 1 in live mode (see finding 6) — reject or accept only for replay-from-single-file with one flush.
   **Support:** findings 6–7 are direct code evidence; option analysis is researcher inference. **Confidence:** high on the mechanics; the recommended option is a planner decision.

8. **Claim (direct code evidence):** The natural insertion points inside `pkg/policy` are (i) `peerRules.httpRules` (map `(port/proto) → []api.PortRuleHTTP`, already deduped via `httpSeen`) — collapse operates per (peer bucket, port, proto, method) right before `portRulesFor` assembles `PortRule.Rules.HTTP`; (ii) attribution is recorded separately in `recordL7` per literal (method, path) via `ruleKeyForL7`, so collapsing at emission leaves evidence/attribution keyed by literal paths while the policy shows templates. Keeping literal attribution is arguably a feature (`cpg explain` continues to show which observed paths back a template), but the planner must decide whether attribution entries should be rolled up under the template key or left literal; `cpg explain`'s `--http-path` filter is exact-match only (KNOWN_LIMITATIONS #11), so literal-keyed evidence keeps that tool working unchanged. `dedup.PoliciesEquivalent`/`sortL7Rules` sort HTTP entries by (Method, Path) — collapsed templates sort deterministically the same way, no changes needed.
   **Source:** direct read of `pkg/policy/builder.go`, `pkg/policy/dedup.go`, `pkg/explain/render.go`. **Support:** direct evidence (mechanics) + inference (recommendation). **Confidence:** high.

### D. Flag/gate design and plumbing

9. **Claim (direct code evidence):** The `include_audit` mirror surface for L7GEN-03 is exactly:
   - CLI: `commonFlags` field + `addCommonFlags`/`parseCommonFlags` entries in `cmd/cpg/commonflags.go` (flags `--l7-collapse-paths`, `--min-flows-per-l7-rule`; shared by generate + replay automatically since both call `addCommonFlags`). Gate default should be set at flag registration (`f.Int("min-flows-per-l7-rule", <default>, …)`), with collapse gated off by default.
   - MCP: `startSessionArgs` struct in `cmd/cpg/mcp_tools.go` — add `CollapseL7Paths bool` (`json:"collapse_l7_paths,omitempty"`) and `MinFlowsPerL7Rule int` (`json:"min_flows_per_l7_rule,omitempty"`); validation happens in `registerSessionTools`'s handler (package main) before `Manager.Start`, per Pitfall J (validators are unexported package-main functions).
   - Session: `StartArgs` fields in `pkg/session/session.go`; mapped in `buildPipelineConfig` (`pkg/session/pipeline_config.go`) → new `hubble.PipelineConfig` fields.
   - Pipeline: `Aggregator` setter pattern (`SetL7Enabled`/`SetIncludeAudit` style) → threaded into `policy.AttributionOptions` (or a sibling options struct) → `BuildPolicy`.
   Note the MCP zero-value pitfall is different here than for durations: `min_flows_per_l7_rule: 0` omitted means "use the conservative default" (only meaningful when collapse is on), mirroring how `defaultDuration` handles omitted durations.
   **Source:** direct reads of the five files listed. **Support:** direct evidence. **Confidence:** high.

10. **Claim:** Gate semantics: L7GEN-04 counts **distinct observed paths** supporting a template ("fewer than N distinct observed paths emit their literal rules instead"), while the flag name says "min-flows-per-l7-rule". The flag name is therefore misleading (researcher observation); the plan should pin the doc/help text to "distinct paths" per the requirement and success criterion 3, and count *distinct literal paths that the template subsumes*, not flow count. Conservative default: prior art evidence is thin — R3's analysis floor is 20 URLs, its parametrize threshold (`combine_min_url_count`) is configurable with small examples (3); a default in the **5–10** range is defensible for a security tool (lower = collapse less often = safer), with 20 as the "maximally conservative" prior-art anchor. This is a product decision for the planner; REQUIREMENTS.md only says "conservative default when collapse is enabled".
    **Sources:** [SkyAPM/R3](https://github.com/SkyAPM/r3) (defaults), REQUIREMENTS.md L7GEN-04. **Support:** direct evidence for R3 defaults; inference for the recommended range. **Confidence:** medium (the specific number is a judgment call).

11. **Claim:** Byte-identical-without-flag (criterion 2) is naturally satisfiable: collapse is opt-in (`--l7-collapse-paths` default false), the gate only activates under collapse, and all changes flow through the `L7Enabled`-style options gate in `BuildPolicy`. The existing golden-fixture conventions (colocated `_test.go`, testify, `testdata/flows/*.jsonl`, `pkg/policy/testdata/`) support a v1.6-output regression test comparing emitted YAML byte-for-byte with collapse disabled. The one thing to watch: adding new fields to `AttributionOptions`/`PipelineConfig` must not perturb default-path emission (verify with the existing dedup/merge fixtures).
    **Support:** direct code evidence (flag defaults, options gating) + inference. **Confidence:** high.

### E. Pitfalls to plan tests around

12. **Claim:** Known hazards the unit tests (criterion 4: "unit-tested per segment class") must cover: URL-encoding (`/users/Andr%C3%A9` — QuoteMeta escapes `%` so encoded forms stay literal; never decode-then-generalize, per KNOWN_LIMITATIONS #8 Cilium matches the wire form); empty path → `^/$`; query strings already stripped by `pathFromURL`; trailing-slash variance (different segment count → no merge); mixed segment classes at one position (stay literal); `v1`/`v2` positions; uppercase hex; segments containing regex metacharacters after generalization of *adjacent* segments (literals still QuoteMeta'd); very long paths near Envoy's RE2 program-size limit (finding 5); identical paths across methods (collapse is per (method, template)); a path set where every segment varies (should yield zero generalization, not `^/.+$`).
    **Support:** researcher inference grounded in KNOWN_LIMITATIONS #8, the `anchorPath`/`pathFromURL` code, and finding 5. **Confidence:** high.

## Contradictions
- **Flag name vs. gate semantics:** `--min-flows-per-l7-rule` (flows) vs. L7GEN-04/criterion 3 (distinct observed paths). Both project documents exist; the requirement text is authoritative. Not resolved here — flagged for the plan.
- **Envoy RE2 flavor:** Cilium docs say "extended POSIX regex"; the actual enforcement is Google RE2 inside Envoy (issue #41314). Practically identical for cpg's generated syntax (classes + bounded repetition), but "POSIX" implies avoiding `\d`-style Perl classes. Not a blocker; record it.
- **Docs examples show unanchored-start paths (`/public`) while cpg emits `^…$`** — Cilium treats both as valid (start anchoring is implicit); cpg's explicit anchoring predates this phase and L7GEN-02 mandates keeping it. No action beyond awareness.

## Missing evidence
- No authoritative source recommends a specific default N for a security-policy confidence gate; R3's 20 is analytics-oriented. The default value remains an open product decision.
- Envoy's exact RE2 program-size cost function is not computable from Go (`regexp` can't estimate it — confirmed by Cilium maintainer in #41314); a "too big" guard would have to be a heuristic length cap, and its threshold is unvalidated against a real cluster.
- No evidence on how `cilium policy validate` / `cilium-dbg` behave with multi-rule character-class templates at scale beyond issue #41314's NACK report; live-cluster validation of collapsed rules (do they NACK? do they match encoded paths the same way literals did?) is outside this research's reach and should be a manual verification step.
- Whether Hubble's `Http.Url` field can carry percent-encoded vs decoded paths inconsistently across Cilium versions (KNOWN_LIMITATIONS #8 says "no automated cross-Cilium-version test") — unresolved upstream.

## Validation Architecture
End-to-end validation should prove the five success criteria with these fixtures and observables:

1. **Fixture inputs** (new `testdata/flows/l7-collapse-*.jsonl`, jsonpb lines, `Verdict_DROPPED` + `Flow.L7.Http` records, following existing fixture conventions):
   - `users-jsonl`: ~10k distinct `GET /api/v1/users/<n>` (n ∈ [0-9]+) on one (ns, workload, port 80/TCP) plus a few literal-stable paths (`GET /api/v1/health`) — proves criterion 1's 10k→1 collapse.
   - `mixed-classes.jsonl`: numeric IDs, hex IDs, UUIDs, and word segments at different positions, plus a position with **mixed** classes and a `v1`/`v2` version position — feeds the per-segment-class unit tests (criterion 4).
   - `below-threshold.jsonl`: a template position with only 2–3 distinct observed paths — proves the gate falls back to literal rules (criterion 3).
   - `unicode-encoded.jsonl`: percent-encoded and UTF-8 paths — prove they stay literal (KNOWN_LIMITATIONS #8 invariant).
2. **Observable outputs:**
   - `cpg replay <fixture> --l7 --l7-collapse-paths` → inspect generated CNP YAML: exactly one `http:` entry per (method, template) with `path: ^/api/v1/users/[0-9]+$` (anchored, POSIX-ERE-safe), literal rules for below-threshold templates.
   - **Byte-identity regression:** `cpg replay <existing fixtures> --l7` (no collapse flag) with collapse code compiled in → output YAML byte-identical to pre-phase golden files (criterion 2). Automated as a testify golden test, not a manual diff.
   - **Gate:** same users fixture replayed with `--min-flows-per-l7-rule 10` and a windowed variant (fixture split across two flush windows) → literal fallback when per-run distinct-path count < N; collapsed when ≥ N. Use replay (deterministic) rather than live streaming for CI.
   - **Segment classes:** table-driven unit tests in `pkg/policy` colocated `_test.go` (testify): per class (numeric/hex/UUID/mixed/version/encoded/single-observation), assert the emitted regex fragment or the decision to stay literal.
   - **MCP parity (criterion 5):** an in-memory MCP transport session test (pattern already exists from Phase 19 e2e tests) calling `start_session` with `collapse_l7_paths: true` + `min_flows_per_l7_rule` against a fake `flowsource.FlowSource` fixture, then `get_policy` → assert identical collapsed rules as the CLI replay of the same flows. A CLI-vs-MCP golden equivalence test on one shared fixture is the cleanest proof.
   - **Envoy size guard (if planned):** unit test that a deliberately huge path set either produces patterns under the length heuristic or falls back to literals; real-RE2-acceptance remains a documented manual check (`cilium policy validate`) since Go cannot compute Envoy's program size.
3. **Explained side-effects to assert:** evidence files still carry literal-path attribution (or template-keyed attribution, per planner decision — assert whichever is chosen), and `cpg explain` output over a collapsed run renders without error.

## Sources
- Kept:
  - Cilium L7 policy docs (https://docs.cilium.io/en/latest/security/policy/layer7/) — authoritative semantics of `PortRuleHTTP.Path` (POSIX-ERE flavor, anchoring examples, OR-combining of same-port rules).
  - cilium/cilium#41314 (https://github.com/cilium/cilium/issues/41314) — Envoy RE2 max program size 100 / NACK behavior; decision-relevant hazard for generated regexes.
  - SkyAPM/R3 (https://github.com/SkyAPM/r3) + its `models/uri_drain/masking.py` — closest prior art for variance-only segment generalization, conservative min-sample gates, version-segment and word-segment exceptions.
  - grafana/clusterurl (https://github.com/grafana/clusterurl) — Go prior art for trie/segment URL collapsing (Beyla lineage); confirms approach, not directly reusable (placeholder output, ML gibberish model).
  - cpg source: `pkg/policy/l7.go`, `builder.go`, `merge.go`, `dedup.go`, `pkg/hubble/aggregator.go`, `pkg/session/{session.go,pipeline_config.go,manager.go}`, `cmd/cpg/{commonflags.go,mcp_tools.go}`, `pkg/explain/render.go`, `docs/KNOWN_LIMITATIONS.md` — direct evidence for all integration points.
  - REQUIREMENTS.md, ROADMAP.md, ARCHITECTURE.md, STACK.md, STATE.md — requirement IDs, phase goal, architecture layers.
- Rejected/deprioritized:
  - dpep/iriq, ZoriHQ/trie-url-classifier, Optimi clusterpath, US Patent 11586487, mintlify PlayCapture wiki — same problem domain but analytics-oriented, less documented, or non-authoritative; R3 + clusterurl already cover the prior-art claim.
  - Cilium gateway-api RE2 validation PR/issue (#47005/#47000) — Envoy-internal route validation, adjacent context only.

## Next steps
- Decide (planner/discuss): gate default N and its exact metric wording ("distinct paths"); cross-flush strategy (recommend option (a) finalize-time collapse, finding 7); attribution rollup policy (literal vs template keys) — this decides `cpg explain` behavior.
- Optional 30-minute spike: estimate RE2 program size empirically by compiling candidate template shapes with Go `regexp` and measuring `.MaxSize()`-style stats, to size a conservative pattern-length guard (acknowledging Go cannot reproduce Envoy's exact metric).

---
*Research run: 2026-10-04. Codebase evidence read at working tree of `/home/ubuntu/Workspace/soulkyu/cpg` (stack mapped at commit eff36ca).*
