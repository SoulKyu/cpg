# Phase 26: Namespace Creation Awareness - Pattern Map

**Mapped:** 2026-10-04
**Files analyzed:** 9 (5 new, 4 modified — including `pkg/hubble/pipeline.go`, which RESEARCH.md's structure list omits but whose `RunPipelineWithSource` body at :446 hosts the new print site)
**Analogs found:** 9 / 9 (every new file has a tracked, read-this-session analog)

All analog paths below verified git-TRACKED via `git ls-files` (no gitignored mirrors).

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `pkg/k8s/ns_watch.go` (NEW) | service (k8s client wrapper) | event-driven (watch) + fail-safe | `pkg/k8s/version.go` (probe shape) + `pkg/k8s/preflight.go` (warn-once contract) | exact |
| `pkg/k8s/ns_watch_test.go` (NEW) | test | unit | `pkg/k8s/preflight_test.go` | exact |
| `pkg/hubble/ns_awareness.go` (NEW) | utility (pure helpers + print) | transform + request-response (print) | `pkg/hubble/summary.go` | exact |
| `pkg/hubble/ns_awareness_test.go` (NEW) | test | unit | `pkg/hubble/summary_test.go` (+ `pipeline_l7_golden_test.go` for byte-identity) | exact |
| `testdata/flows/ns_staggered.jsonl` (NEW) | fixture | batch | `testdata/flows/small.jsonl` | role-match |
| `pkg/hubble/aggregator.go` (MODIFY) | service (stream aggregator) | streaming/event-driven | self — `l7Collapse` invariant (:140-147), `infraDrops` accessor (:240), `policyTargetEndpoint` (:270), Run loop (:417) | exact |
| `pkg/hubble/pipeline.go` (MODIFY) | orchestration (pipeline owner) | streaming | self — summary seam (:417-457), VIS-01/AUD-01 warn blocks (:378-399) | exact |
| `cmd/cpg/generate.go` (MODIFY) | CLI wiring | request-response | self — `maybeRunL7Preflight`/`maybeRunVersionPreflight` (:39-88) | exact |
| `cmd/cpg/mcp_session_test.go` (MODIFY — extend existing test) | test | integration | self — `TestMCPSessionLifecycleWiringAndStdoutPurity` (:158-241) | exact |

---

## Pattern Assignments

### `pkg/k8s/ns_watch.go` (NEW — service, event-driven watch)

**Analog:** `pkg/k8s/version.go` (probe construction + advisory warn copy conventions) and `pkg/k8s/preflight.go` (fail-safe, no-error-return contract).

**Imports pattern** — `pkg/k8s/version.go:1-20` shows the exact import set ns_watch.go needs (client-go core v1, metav1, kubernetes.Interface, zap):
```go
import (
	"context"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)
```
Add `"k8s.io/apimachinery/pkg/watch"` for `watch.Interface`/`watch.Added`.

**Fail-safe contract** — `pkg/k8s/preflight.go:40-52` (`RunL7Preflight` doc comment is the contract to copy verbatim in spirit):
```go
// Failures are reported as zap.Warn calls with remediation hints and DO NOT
// return errors — pre-flight is advisory. RBAC denials (apierrors.IsForbidden)
// are also warned and continued: cluster admins running cpg with reduced
// permissions (CI service accounts) must not be locked out.
//
// Contract: invoke ONCE per cpg invocation. Each warning fires at most once
// per call.
```

**Error-switch pattern** — `pkg/k8s/preflight.go:83-95` (`getCiliumConfig`): copy this `switch` shape for List/Watch error handling — special-case `apierrors.IsForbidden`, treat everything else as generic, warn once, proceed:
```go
	switch {
	case err == nil:
		// proceed
	case apierrors.IsForbidden(err):
		logger.Warn(warnConfigMapForbidden, zap.Error(err))
		return ciliumConfigData{Forbidden: true}
	default:
		logger.Warn(warnConfigMapNotFound, zap.Error(err))
		return ciliumConfigData{NotFound: true}
	}
```

**Explicit warning-copy convention** — `pkg/k8s/version.go:39-56`: warning strings are package-level consts with a comment `Warning copy is intentionally explicit so operators grep'ing logs during incident response find a literal, actionable string. Do not paraphrase.` Define `warnNsWatchDisabled`-style consts the same way, mentioning the required RBAC verb (`namespaces/list`, `namespaces/watch`).

**Bounded-probe rationale comment** — `pkg/k8s/version.go:31-37` (`versionDetectTimeout`): copy this style of "hard ceiling, never merely a suggestion" comment if the watcher gets any timeout; more importantly copy the `CompatInfo` result-struct pattern (`version.go:58-90`): a struct whose zero/`Source == "undetermined"` value means "feature unavailable," **never an error**. `ns_watch.go`'s collector should follow this: return a result object, never propagate errors.

---

### `pkg/k8s/ns_watch_test.go` (NEW — test, unit)

**Analog:** `pkg/k8s/preflight_test.go`

**Test-infra helpers to copy** — `pkg/k8s/preflight_test.go:20-62`:
```go
// newObservedLogger returns a zap logger that captures all entries at WarnLevel+.
func newObservedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.WarnLevel)
	return zap.New(core), logs
}
// countWarnings returns the number of warn-level entries in the observed log.
func countWarnings(logs *observer.ObservedLogs) int {
	return len(logs.FilterLevelExact(zapcore.WarnLevel).All())
}
```
(Note: `newObservedLogger`/`countWarnings` already exist in this package's test file — reuse them, do not redeclare; or extract to a shared test helper if the plan prefers.)

**Fake clientset table-test pattern** — `pkg/k8s/preflight_test.go:88-112`:
```go
func TestRunL7Preflight(t *testing.T) {
	tests := []struct {
		name         string
		setup        func() kubernetes.Interface
		wantWarns    int
		wantContains []string
	}{
		{
			name: "1_l7_proxy_true_envoy_ds_present_silent_pass",
			setup: func() kubernetes.Interface {
				return fake.NewSimpleClientset(
					configMap(map[string]string{"enable-l7-proxy": "true"}),
					envoyDS(),
				)
			},
			wantWarns: 0,
		},
		...
```
For NSAW-01 watch tests, extend with the reactor pattern: `fake.NewSimpleClientset()` + `clienttesting "k8s.io/client-go/testing"` + `PrependWatchReactor("namespaces", ...)` + `watch.NewFake()` (imports already used: `fake` at :19, `clienttesting` at :21, `runtime`/`schema` for reactors at :17-18).

**RBAC-denial test pattern** — `pkg/k8s/preflight_test.go:52-62` (`forbiddenReactor`) + table cases asserting `wantWarns: 1` and `wantContains` — this is exactly the "403 ⇒ one warning, feature disabled, no error" test NSAW-01 needs. Factory funcs like `configMap()` (:20-27) become `namespace(name string) *corev1.Namespace`.

---

### `pkg/hubble/ns_awareness.go` (NEW — utility, transform + print)

**Analog:** `pkg/hubble/summary.go`

**Imports pattern** — `pkg/hubble/summary.go:1-12`: small stdlib surface (`fmt`, `io`, `sort`, `strings`) + `flowpb` where needed. ns_awareness.go adds `google.golang.org/protobuf/types/known/timestamppb`.

**THE critical pattern — no-op-when-empty guard** — `pkg/hubble/summary.go:39-43`:
```go
func PrintClusterHealthSummary(out io.Writer, snapshots []HealthDropSnapshot, stats *SessionStats, healthPath string, state SummaryPathState) {
	if len(snapshots) == 0 {
		return
	}
```
`PrintNewNamespacesSummary` MUST open with the identical guard (`if len(namespaces) == 0 { return }`) — this is the byte-identity mechanism for success criterion 1. No `Fprintf` may exist outside the non-empty branch.

**Doc-comment style for print functions** — `pkg/hubble/summary.go:34-38`: state the no-op contract in the doc comment ("No-op when snapshots is nil or empty").

**Sorted-deterministic-output convention** — `pkg/hubble/summary.go:45-53` sorts before printing; the aggregator's accessors sort at call sites (`aggregator.go:389-396` `ObservedWorkloads` uses `sort.Strings(out)`). New-namespace list must be `sort.Strings`'d before printing for deterministic golden output.

---

### `pkg/hubble/ns_awareness_test.go` (NEW — test, unit)

**Analog:** `pkg/hubble/summary_test.go` (print-function tests) + `pkg/hubble/pipeline_l7_golden_test.go` (byte-identity provenance).

**Zero-output test** — `pkg/hubble/summary_test.go:100-114` (two tests to mirror verbatim in shape):
```go
// TestPrintClusterHealthSummaryZeroDrops verifies no output when no drops.
func TestPrintClusterHealthSummaryZeroDrops(t *testing.T) {
	...
	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(0, 0), "/some/path.json", SummaryPathWritten)
	assert.Equal(t, "", buf.String(), "no output when zero drops")
}
// TestPrintClusterHealthSummaryNilSnapshots verifies no output when snapshots is nil.
func TestPrintClusterHealthSummaryNilSnapshots(t *testing.T) {
	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, nil, makeStats(0, 0), "/some/path.json", SummaryPathWritten)
	assert.Equal(t, "", buf.String(), "no output when snapshots is nil")
}
```
`assert.Equal(t, "", buf.String())` on a `bytes.Buffer` is the repo's canonical zero-bytes assertion — use it for the NSAW-02 "all-zero timestamps ⇒ empty report" and "no new namespaces ⇒ nothing printed" tests.

**Byte-identity golden provenance** — `pkg/hubble/pipeline_l7_golden_test.go:1-56`: for the SC "summary byte-identical to v1.6" test, the dual-buffer comparison (run with feature inert vs without) is simpler than a golden file, but the provenance-comment discipline ("golden provenance — a reviewer must be able to re-derive the goldens… NEVER edit a golden to make this test pass") is the repo's documented precedent for byte-identity evidence; copy the comment discipline into the new test.

---

### `testdata/flows/ns_staggered.jsonl` (NEW — fixture)

**Analog:** `testdata/flows/small.jsonl` (line 1, quoted in 26-RESEARCH.md):
```json
{"flow":{"time":"2026-04-24T14:00:00Z","verdict":"DROPPED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{...,"namespace":"production"},...}}
```
Copy the exact JSON shape (`flow.time` RFC3339, `verdict`, `traffic_direction`, endpoint `namespace` fields). Fixture must contain ≥2 namespaces, one first observed on the very first line (capture-start namespace) and one appearing only on a later line — staggered `time` values are the whole point (Pitfall 3/4).

---

### `pkg/hubble/aggregator.go` (MODIFY — service, streaming)

**Analog:** self — four existing structures to copy for the first-seen map.

**1. Single-goroutine, no-locking invariant comment** — `aggregator.go:140-147` (`l7Collapse`): the new `firstSeenByNamespace` map is mutated ONLY in the Run loop and read only after `g.Wait()`; copy this comment's wording:
```go
	// l7Collapse is the whole-run observed-path store. Allocated by
	// SetCollapseL7Paths when collapse is enabled; mutated and read ONLY
	// inside the flush path (single aggregator goroutine; L7GEN-01
	// concurrency invariant — no locking by design).
```

**2. Post-g.Wait() accessor contract** — `aggregator.go:137-139` (`infraDrops` doc):
```go
	// M-3: single-goroutine ownership during Run() (the aggregator goroutine).
	// All read accessors (InfraDrops, InfraDropTotal) MUST be called only after
	// errgroup g.Wait() returns — same contract as healthWriter.Snapshot().
	// No mutex needed; concurrent access from outside Run() is a caller bug.
```

**3. Copy-returning accessor** — `aggregator.go:238-246` (`InfraDrops`): the new `FirstSeenByNamespace()` accessor must return a copy, like `IgnoredByProtocol` (:202-206):
```go
func (a *Aggregator) InfraDrops() map[flowpb.DropReason]uint64 {
	out := make(map[flowpb.DropReason]uint64, len(a.infraDrops))
	for k, v := range a.infraDrops {
		out[k] = v
	}
	return out
}
```
(Copy semantic: "Map iteration order is not stable; sort at the call site for deterministic output" — the caller sorts the namespace list.)

**4. Where to record: the Run loop's per-flow site** — `aggregator.go:417-470`: the flow channel consumption point already does unconditional per-flow bookkeeping before any filtering. Record first-seen HERE, next to the L7/AUDIT counters (:427-442), because — exactly like those counters — first-seen must be recorded "regardless of whether the flow makes it into a bucket" (filtering via `continue` at :449/:463 comes after; a `continue` before recording would drop source-namespace visibility):
```go
		case f, ok := <-in:
			if !ok {
				a.flush(buckets, out)
				return nil
			}
			// Count L7 HTTP and DNS records on every observed flow, regardless
			// of whether the flow makes it into a bucket and regardless of
			// l7Enabled.
			if f.GetL7().GetHttp() != nil {
				a.l7HTTPCount++
			}
```
**5. Namespace attribution** — `aggregator.go:270-287` (`policyTargetEndpoint` + `buildDropEvent`): namespace strings come from `ep.Namespace` with the `if ep.Namespace != ""` empty-guard. For NSAW-02 (both-endpoints decision, A5), read `f.GetSource().GetNamespace()` and `f.GetDestination().GetNamespace()` with the same empty-guard; skip empty names.

**Nil-safety precedent for `Flow.Time`** — pattern to apply: `flowpb.Flow.Time` is `*timestamppb.Timestamp`; use `.IsValid()` + nil check, mirroring how the codebase nil-guards optional flow fields (e.g. `f.GetL7().GetHttp()` at :428). Skip nil/invalid/zero times entirely (Pitfall 3).

---

### `pkg/hubble/pipeline.go` (MODIFY — orchestration, streaming)

**Analog:** self — the summary seam.

**Print site + Stdout resolution** — `pkg/hubble/pipeline.go:419-427` and the call at :446 (final block of `RunPipelineWithSource`):
```go
	// HEALTH-03: print cluster-health summary block to stdout.
	stdout := cfg.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	...
	PrintClusterHealthSummary(stdout, snapshots, stats, healthPath, pathState)

	stats.Log(cfg.Logger)
```
The new call must use the SAME resolved `stdout` local, immediately after `PrintClusterHealthSummary` (research recommendation: print after the health block). Reuse the already-resolved local — do not re-resolve `cfg.Stdout`.

**Warn-once advisory pattern (watcher-side, if any warn logic lives in pipeline)** — `pipeline.go:371-399` (VIS-01 then AUD-01): bare post-`g.Wait()` check + single `cfg.Logger.Warn` with structured fields (`zap.Strings`, `zap.Uint64`), no dedup map — the comment explicitly warns NOT to build one ("Mirrors VIS-01's shape exactly — a bare post-g.Wait() check, NOT a dedup map"). NSAW warnings emitted from `pkg/k8s` already satisfy warn-once; do not add a second warn layer here.

**Aggregator-stats surface pattern** — `pipeline.go:344-352`: end-of-run values are pulled from the aggregator via accessors AFTER `g.Wait()` and assigned to `stats` fields:
```go
	stats.InfraDropTotal = agg.InfraDropTotal()
	stats.InfraDropsByReason = agg.InfraDrops()
```
The new-namespace list follows the same shape: `agg.FirstSeenByNamespace()` (or a package-level helper fed by it) → pure helper computes the report list → `PrintNewNamespacesSummary(stdout, list, source)`. The watcher collector (NSAW-01) similarly threads its result through `PipelineConfig` (new field, mirroring how `ClusterPolicies`/`L7Enabled` are optional config fields at `pipeline.go:65-101`) — MCP mode leaves it nil ⇒ feature inert.

---

### `cmd/cpg/generate.go` (MODIFY — CLI wiring)

**Analog:** self — `maybeRunL7Preflight` / `maybeRunVersionPreflight`.

**The advisory-probe launcher pattern** — `cmd/cpg/generate.go:39-88`:
```go
// maybeRunL7Preflight runs pkg/k8s.RunL7Preflight when L7 generation is
// requested and pre-flight is not explicitly disabled. Pre-flight is advisory:
// any failure to construct a client is logged as a warning and the pipeline
// proceeds. Pre-flight is invoked AT MOST ONCE per cpg invocation.
//
// Caller contract: invoke from cpg generate ONLY. cpg replay is offline by
// definition and must never call this function regardless of --l7.
func maybeRunL7Preflight(ctx context.Context, kubeConfig *rest.Config, l7Enabled, noPreflight bool, logger *zap.Logger) {
	if !l7Enabled || noPreflight {
		return
	}
	if kubeConfig == nil {
		var err error
		kubeConfig, err = k8s.LoadKubeConfig()
		if err != nil {
			logger.Warn("--l7 preflight skipped: kubeconfig not available", zap.Error(err))
			return
		}
	}
	client, err := l7ClientFactory(kubeConfig)
	if err != nil {
		logger.Warn("--l7 preflight skipped: failed to construct kubernetes client", zap.Error(err))
		return
	}
	k8s.RunL7Preflight(ctx, client, logger)
}
```
Copy for `maybeStartNamespaceWatcher`:
- same "Caller contract: invoke from cpg generate ONLY" doc-comment discipline (replay never starts a watcher);
- same nil-kubeconfig early return with a skip warning (covers the `--server` bypass, Pitfall 6);
- reuse the `l7ClientFactory` package-level factory var (`generate.go:23-26`, "Extracted as a package-level variable so tests can substitute a fake client") — same seam lets NSAW tests inject a fake clientset;
- derive the watcher ctx from the same signal ctx passed to `RunPipeline` (bounded-probe rationale: `versionPreflightTimeout` comment at :27-34 — "warn-and-proceed, never blocking must survive a hung apiserver"); for NSAW the lifetime IS the run ctx, but the comment discipline (advisory must never hang startup/shutdown, Pitfall 5) applies.

---

### `cmd/cpg/mcp_session_test.go` (MODIFY — extend existing test)

**Analog:** self — `TestMCPSessionLifecycleWiringAndStdoutPurity` (:158-241).

**The stdout-purity mechanism to keep intact** — `mcp_session_test.go:159-167` + final assertion :237-240:
```go
	r, w, err := os.Pipe()
	require.NoError(t, err)
	realStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = realStdout })
	// Deliberately NOT swapping os.Stderr: mcpModeStdout() must route the
	// pipeline's human-readable summary there...
	...
	require.NoError(t, w.Close())
	leaked, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Empty(t, leaked, "Pitfall I: mcpModeStdout() must keep the session summary off the real os.Stdout")
```
Extension rule: this test already proves the whole-run `assert.Empty(t, leaked)` invariant end-to-end; because the new print block rides `cfg.Stdout` (== `mcpModeStdout()` == os.Stderr in MCP mode), the existing assertion structurally covers NSAW too. The extend step is: verify the test still passes unchanged after the new block lands and add a comment noting the new block is now under this umbrella (SC-4). Do not weaken or rewire it.

**mcpModeStdout contract** (why no MCP branch is needed) — `cmd/cpg/mcp.go:123-142`:
```go
// mcpModeStdout is the MCP-mode human-output seam value (D-02/D-05): never
// nil, never os.Stdout, always os.Stderr. It encodes the contract that
// PipelineConfig.Stdout (pkg/hubble/pipeline.go) must resolve to in MCP mode.
...
func mcpModeStdout() io.Writer {
	return os.Stderr
}
```
NSAW-01 is CLI-generate-only (research Open Question 2 recommendation) ⇒ `pkg/session` untouched ⇒ zero MCP surface change; this comment is the authority to cite in the plan.

---

## Shared Patterns

### Fail-safe advisory warn-once (never an error return)
**Sources:** `pkg/k8s/preflight.go:40-52` (contract), `pkg/k8s/preflight.go:83-95` (error switch), `cmd/cpg/generate.go:39-88` (call-site launcher), `pkg/hubble/pipeline.go:371-399` (VIS-01/AUD-01 shape)
**Apply to:** `pkg/k8s/ns_watch.go` and its `cmd/cpg/generate.go` wiring.
```go
// Failures are reported as zap.Warn calls ... and DO NOT
// return errors — pre-flight is advisory. RBAC denials (apierrors.IsForbidden)
// are also warned and continued ...
```
Rule: every failure path (List err, Watch err, RBAC, channel close) ⇒ exactly one warn, feature off for the run, zero effect on generation/exit code.

### No-op-when-empty summary guard (byte identity)
**Source:** `pkg/hubble/summary.go:39-43`; test proof shape `pkg/hubble/summary_test.go:100-114`
**Apply to:** `PrintNewNamespacesSummary` in `pkg/hubble/ns_awareness.go`.
Rule: `if len(namespaces) == 0 { return }` is the FIRST statement; sorted list; deterministic rendering.

### `cfg.Stdout` seam / MCP purity
**Sources:** `pkg/hubble/pipeline.go:420-424` (resolve once), `cmd/cpg/mcp.go:123-142` (`mcpModeStdout`), `cmd/cpg/mcp_session_test.go:158-241` (end-to-end proof)
**Apply to:** the new print call in `RunPipelineWithSource`.
Rule: print only through the already-resolved `stdout` local; no MCP-specific branch anywhere; never write to evidence JSON.

### Single-goroutine aggregator invariant + post-`g.Wait()` accessors
**Sources:** `pkg/hubble/aggregator.go:140-147` (invariant), `:137-139` (accessor contract), `:240-246` (copy accessor), `:417-442` (recording site before any `continue` filter)
**Apply to:** `firstSeenByNamespace` map + `FirstSeenByNamespace()` accessor in `pkg/hubble/aggregator.go`.

### Fake-clientset advisory-probe testing
**Sources:** `pkg/k8s/preflight_test.go:20-112` (observed logger, forbiddenReactor, fake.NewSimpleClientset table tests), `cmd/cpg/generate.go:23-26` (`l7ClientFactory` injection seam)
**Apply to:** `pkg/k8s/ns_watch_test.go` (via `PrependWatchReactor` + `watch.NewFake()`) and any cmd-level watcher-start test.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `testdata/flows/ns_staggered.jsonl` (the staggered-timestamp aspect) | fixture | batch | `small.jsonl` provides the shape, but no existing fixture encodes staggered per-namespace first-seen times — build new, copying `small.jsonl`'s field shape verbatim |

Everything else has an exact or self-analog. The client-go `Watch` API call itself (`CoreV1().Namespaces().Watch`) has no in-repo precedent — RESEARCH.md §Code Examples and the module-cache citations (`kubernetes/typed/core/v1/namespace.go:48`, `apimachinery pkg/watch/watch.go:54-74`, `testing/fake.go:115`) are the authoritative shape references.

## Metadata

**Analog search scope:** `pkg/k8s/`, `pkg/hubble/`, `cmd/cpg/`, `testdata/flows/`
**Files read this session:** 12 (`pipeline.go` ×2 ranges, `summary.go`, `summary_test.go`, `aggregator.go` ×3 ranges, `preflight.go`, `preflight_test.go`, `version.go`, `generate.go`, `mcp.go`, `mcp_session_test.go`, `pipeline_l7_golden_test.go`)
**Pattern extraction date:** 2026-10-04
