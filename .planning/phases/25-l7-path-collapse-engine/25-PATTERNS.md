# Phase 25 — Pattern Mapping: L7 Path Collapse Engine

Derived from `25-RESEARCH.md` (Findings C/D) against working tree at commit eff36ca.
Every excerpt below is verbatim from the named file at the shown line numbers.

## File inventory and roles

| File | Role | Data-flow position |
|---|---|---|
| `pkg/policy/l7_collapse.go` (NEW) | Collapse engine: segment-wise generalizer | called from `BuildPolicy`/`peerRules` path, gated by options |
| `pkg/policy/l7.go` | Existing L7 primitives (`extractHTTPRules`, `anchorPath`) | upstream of collapse: literal path extraction per flow |
| `pkg/policy/builder.go` | Policy assembly (`peerRules.httpRules`, `recordL7`, `portRulesFor`) | collapse insertion point before rule emission |
| `cmd/cpg/commonflags.go` | CLI flag registration/parsing shared by generate + replay | source of the two new flags |
| `cmd/cpg/mcp_tools.go` | MCP `start_session` arg surface + validation | MCP mirror of the same two inputs |
| `pkg/session/session.go` | `StartArgs` data layer (no validation) | carries MCP args toward pipeline config |
| `pkg/session/pipeline_config.go` | `StartArgs` → `hubble.PipelineConfig` mapping | forwarding point |
| `pkg/hubble/aggregator.go` | Per-window accumulation + `BuildPolicy` invocation | setter-flags + options-struct hand-off |
| `pkg/hubble/pipeline.go` | Pipeline composition (`PipelineConfig`, `RunPipelineWithSource`) | config → Aggregator setter wiring |

The single reference analog for the whole plumbing chain is **`include_audit`**
(AUD-01): it crosses every one of these layers in exactly the order the collapse
flags must. Wherever possible, replicate its shape field-for-field.

---

## Pattern 1 — Options-struct threading into `BuildPolicy` (the `include_audit` / `L7Enabled` analog)

`AttributionOptions` is the small options struct that flows from the Aggregator
into `BuildPolicy` and gates the L7 branch. Collapse options extend this struct
(or a sibling passed alongside it) and gate the new code path the same way —
default false must leave emission byte-identical (criterion 2).

`pkg/policy/attribution.go`:
```go
121: type AttributionOptions struct {
122: 	MaxSamples int
123: 	// L7Enabled gates the HTTP L7 codegen branch in BuildPolicy. When true,
124: 	// flows carrying Flow.L7.Http records produce toPorts.rules.http entries
125: 	// on the matching L4 PortRule. When false (default), L7 records on flows
126: 	// are ignored entirely — output is byte-identical to v1.1 for any input
127: 	// (HTTP-01 / HTTP-04 gating, byte-stability invariant).
128: 	L7Enabled bool
129: }
```

The Aggregator carries the flag as an unexported field set by a setter "safe to
call before Run()", then forwards it in `flush` via the options struct:

`pkg/hubble/aggregator.go` (fields + setter):
```go
78: 	// l7Enabled gates the HTTP L7 codegen branch in BuildPolicy. Forwarded
79: 	// from PipelineConfig.L7Enabled via SetL7Enabled before Run().
80: 	l7Enabled bool
```
```go
316: // SetL7Enabled toggles the HTTP L7 codegen branch in BuildPolicy. Safe to
317: // call before Run().
318: func (a *Aggregator) SetL7Enabled(enabled bool) {
319: 	a.l7Enabled = enabled
320: }
```
```go
334: // SetIncludeAudit toggles whether the classification gate treats
335: // Verdict_AUDIT the same as Verdict_DROPPED. Safe to call before Run().
336: func (a *Aggregator) SetIncludeAudit(enabled bool) {
337: 	a.includeAudit = enabled
338: }
```

`pkg/hubble/aggregator.go` — the flush that clears buckets each window (note:
this is why per-window-only collapse fails live-mode criterion 1, per research
finding 6; the new options here are the exact threading point for
`CollapseL7Paths` + `MinFlowsPerL7Rule`):
```go
543: func (a *Aggregator) flush(buckets map[AggKey][]*flowpb.Flow, out chan<- policy.PolicyEvent) {
544: 	for key, flows := range buckets {
545: 		cnp, attrib := policy.BuildPolicy(key.Namespace, key.Workload, flows, a.tracker, policy.AttributionOptions{
546: 			MaxSamples: a.maxSamples,
547: 			L7Enabled:  a.l7Enabled,
548: 		})
549: 		out <- policy.PolicyEvent{
550: 			Namespace:   key.Namespace,
551: 			Workload:    key.Workload,
552: 			Policy:      cnp,
553: 			Attribution: attrib,
554: 		}
555: 	}
556: 	for k := range buckets {
557: 		delete(buckets, k)
558: 	}
```

`pkg/hubble/pipeline.go` — composition-root wiring, setters called right after
construction (new setters follow this block):
```go
193: 	tracker := NewUnhandledTracker(cfg.Logger)
194: 	agg := NewAggregator(cfg.FlushInterval, cfg.Logger, tracker)
195: 	agg.SetL7Enabled(cfg.L7Enabled)
196: 	agg.SetIncludeAudit(cfg.IncludeAudit)
197: 	agg.SetIgnoreProtocols(cfg.IgnoreProtocols)
198: 	agg.SetIgnoreDropReasons(cfg.IgnoreDropReasons)
```

`cmd/cpg/generate.go` and `cmd/cpg/replay.go` — the CLI → `PipelineConfig`
hand-off; both files set the same two lines identically:
```go
295: 		// (generate.go)
296: 		L7Enabled:    f.l7,
297: 		IncludeAudit: f.includeAudit,
```
```go
123: 		// (replay.go)
124: 		L7Enabled:    f.l7,
125: 		IncludeAudit: f.includeAudit,
```

`pkg/session/pipeline_config.go` — the MCP → `PipelineConfig` hand-off (same
field names, sourced from `StartArgs` instead of `commonFlags`):
```go
100: 		L7Enabled:    args.L7,
101: 		IncludeAudit: args.IncludeAudit,
```

**Executor recipe:** add `CollapseL7Paths bool` + `MinFlowsPerL7Rule int` at
every level above, in the same order: `commonFlags` field → `PipelineConfig`
fields → `Aggregator` fields + setters → `AttributionOptions` (or sibling) →
gate inside `BuildPolicy`/`peerRules`.

---

## Pattern 2 — CLI flag registration (`addCommonFlags`/`parseCommonFlags`)

`cmd/cpg/commonflags.go` — struct field, registration with default at
registration time, and parse. Both `generate` and `replay` inherit flags
automatically because both call `addCommonFlags`:
```go
40: type commonFlags struct {
...
52: 	l7 bool
53:
54: 	includeAudit bool
...
```
```go
84: 	f.Bool("l7", false, "enable L7 (HTTP/DNS) policy generation; Phase 7 plumbs the flag, codegen lights up in v1.2 Phase 8/9")
85:
86: 	f.Bool("include-audit", false, "ingest Verdict_AUDIT flows alongside DROPPED (opt-in; default preserves pre-v1.6 DROPPED-only behavior)")
```
```go
98: func parseCommonFlags(cmd *cobra.Command) commonFlags {
...
112: 	out.l7, _ = f.GetBool("l7")
113: 	out.includeAudit, _ = f.GetBool("include-audit")
```

There is also a shared `PreRunE` validator hook, if the new flags need
cross-flag validation (e.g. reject `--min-flows-per-l7-rule` ≤ 0):
```go
193: func validateCommonFlags(cmd *cobra.Command, _ []string) error {
194: 	f := parseCommonFlags(cmd)
195: 	if _, err := validateIgnoreProtocols(f.ignoreProtocols); err != nil {
196: 		return err
197: 	}
...
202: 	return nil
203: }
```

**Conventions:** default values are baked into flag registration
(`f.Int("min-flows-per-l7-rule", <default>, …)`); help text must pin the gate
metric to "distinct observed paths" per L7GEN-04, not "flows" (research finding
10 flags the name/semantics mismatch).

---

## Pattern 3 — MCP session arg + package-main validation

`cmd/cpg/mcp_tools.go` — the LLM-facing struct; every optional field carries
`omitempty` and a `jsonschema:` description (these render into the tool schema):
```go
21: type startSessionArgs struct {
22: 	Namespace         []string `json:"namespace,omitempty" jsonschema:"namespace filter, repeatable"`
23: 	AllNamespaces     bool     `json:"all_namespaces,omitempty" jsonschema:"observe all namespaces"`
24: 	L7                bool     `json:"l7,omitempty" jsonschema:"enable L7 (HTTP/DNS) policy generation"`
25: 	IncludeAudit      bool     `json:"include_audit,omitempty" jsonschema:"ingest Verdict_AUDIT flows alongside DROPPED (opt-in; default preserves pre-v1.6 DROPPED-only behavior)"`
...
34: }
```

Validation happens in `registerSessionTools`'s handler, in package main,
BEFORE `mgr.Start` — per Pitfall J the CLI validators are unexported
package-main functions `pkg/session` cannot see, so `StartArgs` receives only
already-validated values:
```go
111: 		ignoreProtocols, err := validateIgnoreProtocols(args.IgnoreProtocols) // D-06, verbatim reuse
112: 		if err != nil {
113: 			return nil, session.StartResult{}, err
114: 		}
...
123: 		flushInterval, err := parseOptionalDuration(args.FlushInterval, "flush_interval")
124: 		if err != nil {
125: 			return nil, session.StartResult{}, err
126: 		}
128: 		result, err := mgr.Start(ctx, session.StartArgs{
129: 			Namespaces:        args.Namespace,
...
132: 			IncludeAudit:      args.IncludeAudit,
```

**Zero-value semantics for the new int arg:** an omitted
`min_flows_per_l7_rule` unmarshals to Go `0`, which must mean "use the
conservative default when collapse is on" — exactly the problem
`defaultDuration` solves for durations. `pkg/session/pipeline_config.go`:
```go
14: func defaultDuration(d, fallback time.Duration) time.Duration {
15: 	if d <= 0 {
16: 		return fallback
17: 	}
18: 	return d
19: }
```
Apply the same ≤0-⇒-default treatment to `MinFlowsPerL7Rule` (at the
`buildPipelineConfig`/Aggregator level, or via a small int helper) and reject
negative values in the MCP handler if stricter UX is wanted.

`pkg/session/session.go` — `StartArgs` is the pure data layer; new fields get
the same style of constraint comment as `IncludeAudit`:
```go
144: type StartArgs struct {
145: 	Namespaces    []string
146: 	AllNamespaces bool
147: 	L7            bool
148: 	// IncludeAudit ingests Verdict_AUDIT flows alongside Verdict_DROPPED
149: 	// (AUD-01). Default false preserves pre-v1.6 DROPPED-only behavior.
150: 	IncludeAudit bool
...
166: }
```

---

## Pattern 4 — Collapse insertion points inside `pkg/policy`

The collapse pass operates on the per-bucket HTTP rule lists that
`recordL7`/`addHTTPRules` accumulate, before `portRulesFor` assembles
`PortRule.Rules.HTTP`.

`pkg/policy/builder.go` — the bucket state (dedup by literal `(method, path)`
via `httpSeen`; note literal attribution is recorded separately in `recordL7`,
which is what keeps `cpg explain --http-path` working unchanged per finding 8):
```go
205: 	// httpRules collects per-(port, proto) HTTP L7 entries when
206: 	// AttributionOptions.L7Enabled is true. Key is "<port>/<proto>" matching
207: 	// the seen-key form so a port's L7 rules align to its L4 PortProtocol.
208: 	// When empty (the v1.1 codepath), ingress/egress emission is byte-stable
209: 	// with v1.1 — Rules are NOT attached to any PortRule.
210: 	httpRules map[string][]api.PortRuleHTTP
211: 	// httpSeen dedups within a bucket so two flows reporting GET /a on the
212: 	// same (port, proto) collapse into a single PortRuleHTTP entry.
213: 	httpSeen map[string]map[string]struct{}
```

`pkg/policy/builder.go` — the L7 gate + per-literal attribution:
```go
478: func recordL7(pr *peerRules, f *flowpb.Flow, fp *flowProto, direction string, peer Peer, opts AttributionOptions) l7Kind {
479: 	if !opts.L7Enabled {
480: 		return l7None
481: 	}
...
488: 	if f.GetL7().GetHttp() != nil {
489: 		rules := extractHTTPRules(f)
490: 		if len(rules) == 0 {
491: 			return l7None
492: 		}
493: 		portKey := strconv.FormatUint(uint64(fp.port), 10) + "/" + string(fp.proto)
494: 		pr.addHTTPRules(portKey, rules)
495: 		for _, r := range rules {
496: 			l7 := &L7Discriminator{Protocol: "http", HTTPMethod: r.Method, HTTPPath: r.Path}
497: 			pr.recordAttribution(ruleKeyForL7(direction, peer, fp, l7), f, opts.MaxSamples)
498: 		}
499: 		return l7HTTP
500: 	}
```

`pkg/policy/builder.go` — final assembly; collapse should run against
`httpByPort[key]` just before this builds the `L7Rules{HTTP: …}` (or be invoked
over the bucket map in `buildIngressRules`/`buildEgressRules`):
```go
667: func portRulesFor(ports []api.PortProtocol, httpByPort map[string][]api.PortRuleHTTP) api.PortRules {
...
683: 	out := make(api.PortRules, 0, len(ports))
684: 	for _, p := range ports {
685: 		key := p.Port + "/" + string(p.Protocol)
686: 		pr := api.PortRule{Ports: []api.PortProtocol{p}}
687: 		if rules := httpByPort[key]; len(rules) > 0 {
688: 			pr.Rules = &api.L7Rules{HTTP: rules}
689: 		}
690: 		out = append(out, pr)
691: 	}
692: 	return out
693: }
```

`pkg/policy/l7.go` — the literal primitives the collapse engine builds on
(`pathFromURL` strips query/fragment; `QuoteMeta` + `^…$` anchoring is the
format templates must preserve):
```go
93: func anchorPath(rawURL string) string {
94: 	path := pathFromURL(rawURL)
95: 	if path == "" {
96: 		path = "/"
97: 	}
98: 	return "^" + regexp.QuoteMeta(path) + "$"
99: }
```

`pkg/policy/l7.go` — per-flow rule shape and the HTTP-05 anti-feature contract
(Headers/Host stay zero — collapse templates inherit this):
```go
31: func extractHTTPRules(f *flowpb.Flow) []api.PortRuleHTTP {
...
51: 	return []api.PortRuleHTTP{{
52: 		Method: method,
53: 		Path:   anchorPath(http.GetUrl()),
54: 		// HTTP-05: Headers, Host, HeaderMatches deliberately left zero.
55: 	}}
56: }
```

---

## Pattern 5 — Merge-on-write reconciliation constraint (context for the flush problem)

`pkg/policy/merge.go` — `MergePolicy` union-merges HTTP sub-lists across
flushes, which is why late-emitted templates coexist with earlier literals on
disk (finding 7) unless the chosen cross-flush strategy replaces them:
```go
18: func MergePolicy(existing, incoming *ciliumv2.CiliumNetworkPolicy) *ciliumv2.CiliumNetworkPolicy {
19: 	result := existing.DeepCopy()
...
35: 	// Merge ingress rules
36: 	for _, inRule := range incoming.Spec.Ingress {
37: 		merged := false
38: 		for i, exRule := range result.Spec.Ingress {
39: 			if ingressRulesMatch(exRule, inRule) {
40: 				result.Spec.Ingress[i].ToPorts = mergePortRules(exRule.ToPorts, inRule.ToPorts)
```

`pkg/output/writer.go` — the atomic writer pattern (temp file + chmod +
rename) used for every policy write; the finalize-time-replacement strategy
must route through this same writer:
```go
81: 	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
...
98: 	if err := os.Rename(tmpPath, path); err != nil {
99: 		_ = os.Remove(tmpPath)
100: 		return fmt.Errorf("atomic rename: %w", err)
101: 	}
```

---

## Pattern 6 — Test conventions to replicate

**Table-driven unit tests in `pkg/policy`** (colocated `_test.go`, plain
`testing` + helper constructors; the collapse classifier's per-segment-class
tests follow this exact shape). `pkg/policy/l7_test.go`:
```go
14: func httpFlow(method, url string, headers []*flowpb.HTTPHeader) *flowpb.Flow {
15: 	return &flowpb.Flow{
16: 		L7: &flowpb.Layer7{
17: 			Record: &flowpb.Layer7_Http{
18: 				Http: &flowpb.HTTP{
19: 					Method:  method,
20: 					Url:     url,
21: 					Headers: headers,
22: 				},
23: 			},
24: 		},
25: 	}
26: }
28: func TestExtractHTTPRules(t *testing.T) {
29: 	tests := []struct {
30: 		name string
31: 		flow *flowpb.Flow
32: 		want []api.PortRuleHTTP
33: 	}{
```

**Fixture-driven pipeline golden tests** (`testdata/flows/*.jsonl` jsonpb
lines, testify `require`/`assert`, tiny flush interval, `RunPipelineWithSource`
with `flowsource.NewFileSource`). `pkg/hubble/pipeline_l7_test.go`:
```go
23: const l7HTTPFixture = "../../testdata/flows/l7_http.jsonl"
...
38: func runReplayPipeline(t *testing.T, fixture string, l7Enabled bool, evidenceEnabled bool) (outDir, evDir string, logs *observer.ObservedLogs) {
...
45: 	src, err := flowsource.NewFileSource(fixture, logger)
46: 	require.NoError(t, err)
...
52: 	cfg := PipelineConfig{
53: 		FlushInterval: 50 * time.Millisecond,
54: 		OutputDir:     outDir,
55: 		Logger:        logger,
56: 		L7Enabled:     l7Enabled,
57: 	}
...
69: 	require.NoError(t, RunPipelineWithSource(ctx, cfg, src))
```
Assertions then read the emitted YAML and use `assert.Contains` on the `http:`
block (byte-identity for criterion 2 should be an exact string/file comparison
of the same output against the pre-phase golden).

**Flag registration test convention.** `cmd/cpg/commonflags_test.go`:
```go
299: // TestIncludeAuditFlagParses confirms the --include-audit flag is registered
300: // on both generate and replay subcommands, parses to true when set, and
301: // defaults to false when absent (AUD-01 opt-in, pre-v1.6 behavior unchanged).
302: func TestIncludeAuditFlagParses(t *testing.T) {
303: 	t.Run("generate", func(t *testing.T) {
304: 		cmd := newGenerateCmd()
305: 		require.NoError(t, cmd.Flags().Set("include-audit", "true"))
306: 		f := parseCommonFlags(cmd)
307: 		assert.True(t, f.includeAudit)
308: 	})
```

**MCP arg validation test convention.** `cmd/cpg/mcp_session_test.go` hosts
in-memory transport session tests (`TestMCPSessionToolsListed`,
`TestMCPSessionLifecycleWiringAndStdoutPurity`, `TestParseOptionalDuration` at
line 248) — new `collapse_l7_paths`/`min_flows_per_l7_rule` validation and a
CLI-vs-MCP parity test slot here.

**Shared flow builders** live in `pkg/policy/testdata/` (`ingress_flow.go` —
`testdata.IngressTCPFlow(...)` used across `builder_test.go`, `merge_*_test.go`);
new L7-collapse fixtures reuse these or add a sibling builder there.

---

## Executor checklist (distilled)

1. Thread `CollapseL7Paths bool` + `MinFlowsPerL7Rule int` through: `commonFlags` → `commonFlags`→`PipelineConfig` (generate.go AND replay.go) → `PipelineConfig` fields → `Aggregator` fields + `SetCollapseL7Paths`/`SetMinFlowsPerL7Rule` setters → `AttributionOptions`/sibling → gate in the collapse path only.
2. MCP mirror: `startSessionArgs` fields (`collapse_l7_paths,omitempty`, `min_flows_per_l7_rule,omitempty`) → handler validation before `mgr.Start` → `session.StartArgs` fields → `buildPipelineConfig` mapping with ≤0-⇒-default int handling (mirroring `defaultDuration`).
3. New collapse engine in `pkg/policy/l7_collapse.go`: pure function(s) over `[]api.PortRuleHTTP` (or literal paths + methods), POSIX-ERE-safe output, `QuoteMeta` for literals, `^…$` anchoring preserved, gate on distinct-path count.
4. Default-off invariant: all new fields zero ⇒ emission byte-identical; prove with the existing golden/`testdata/flows` fixtures plus a table of per-segment-class unit tests.
5. Decide-and-document (from research "Next steps"): cross-flush strategy (finding 7 option (a) recommended), attribution rollup (literal keys keep `cpg explain` untouched), and the gate default N — these are planner decisions, not executor choices.
