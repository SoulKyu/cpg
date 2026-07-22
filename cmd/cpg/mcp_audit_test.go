package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// cpgModulePrefix is the package-path prefix that identifies "cpg's own
// code" as opposed to a third-party dependency. Restricting Stage 3's
// direct-call-instruction scan to functions carrying this prefix is what
// keeps the audit small and precise — third-party functions in the Stage-2
// BFS's visited set are never individually re-scanned (D-01, Pitfall 1).
const cpgModulePrefix = "github.com/SoulKyu/cpg/"

// disallowedFSWrite is the watched set of write-capable os.* package-level
// functions (D-03, Property 2). Deliberately an over-approximation — D-04's
// soundness direction is to prefer over-flagging a benign call over silently
// missing a real one (e.g. os.OpenFile is flagged even though most calls in
// this repo pass read-only flags, because StaticCallee() alone cannot see
// the flag argument's value; a false positive here is a human triage, not a
// missed write).
var disallowedFSWrite = map[string]bool{
	"os.WriteFile":  true,
	"os.Create":     true,
	"os.CreateTemp": true,
	"os.OpenFile":   true,
	"os.Mkdir":      true,
	"os.MkdirAll":   true,
	"os.MkdirTemp":  true,
	"os.Rename":     true,
	"os.Remove":     true,
	"os.RemoveAll":  true,
	// WR-02: filesystem-mutating os.* calls that bypass a watched
	// constructor. os.Chmod is already used in the write path
	// (pkg/output/writer.go's allowlisted (*output.Writer).Write); the
	// others (Truncate/Symlink/Link/Chown/Lchown/Chtimes) create, destroy,
	// or mutate metadata on a caller-chosen path with no watched
	// constructor in the chain.
	"os.Chmod":    true,
	"os.Truncate": true,
	"os.Symlink":  true,
	"os.Link":     true,
	"os.Chown":    true,
	"os.Lchown":   true,
	"os.Chtimes":  true,
}

// k8sWriteVerbs is Property 1's (D-02) verb-name set, matched ONLY against
// interface-dispatch ("invoke" mode) calls' Method.Name() — deliberately
// with NO receiver-type filtering (an earlier, under-specified type-shape
// check was dropped during plan review; see 19-01-PLAN.md Task 1).
// client-go's typed clients and the dynamic client expose every K8s write
// verb exclusively as interface methods, so a bare-name match on an invoke
// call IS the K8s-write signal; IsInvoke() is the only qualifier needed, and
// it exists solely to exclude static calls (e.g. os.Create — Property 2's
// concern) and concrete-type methods (e.g. sync.Map.Delete), never to filter
// by receiver type. There is intentionally NO allowlist for this property:
// the repo baseline is zero reachable K8s write verbs, and any hit is new.
var k8sWriteVerbs = map[string]bool{
	"Create": true,
	"Update": true,
	"Patch":  true,
	"Delete": true,
	"Apply":  true,
	// WR-03: additional write verbs client-go's typed and dynamic clients
	// also expose as interface methods.
	"DeleteCollection": true,
	"UpdateStatus":     true,
	"ApplyStatus":      true,
}

// fsWriteAllowlist is the exact, hand-audited set of cpg-owned functions
// permitted to call a disallowedFSWrite function (D-03), keyed by SSA symbol
// ((*ssa.Function).String()). Per-function, not per-call-site: any watched
// os.* call from one of these 5 functions is accepted, but a brand new
// writer FUNCTION — even one reusing the identical atomic-write shape —
// still fails until a human reviews and adds it here (T-19-03: accepted
// risk, RESEARCH.md Open Question 2). Every entry's path is rooted in
// os.MkdirTemp (the session tmpdir itself) or session.DeriveSessionPaths
// (the tmpdir-layout single source of truth, pkg/session/paths.go), written
// via an atomic CreateTemp+Write+Rename sequence (pkg/output/writer.go's
// shape, mirrored byte-identically in pkg/evidence/writer.go and
// pkg/hubble/health_writer.go).
var fsWriteAllowlist = map[string]bool{
	// pkg/session/manager.go: os.MkdirTemp("", "cpg-session-*") creates the
	// session's own tmpdir; every os.RemoveAll(tmpDir) call on Start's
	// purge-existing-session and setup-failure paths targets only that same
	// freshly minted tmpdir — never a caller-chosen path.
	"(*github.com/SoulKyu/cpg/pkg/session.Manager).Start": true,

	// pkg/session/manager.go — the anonymous closure inside Shutdown's
	// bounded-remove goroutine: os.RemoveAll(tmpDir), the same tmpDir
	// captured from the session struct, rooted in the same os.MkdirTemp call
	// as Start above.
	"(*github.com/SoulKyu/cpg/pkg/session.Manager).Shutdown$1": true,

	// pkg/output/writer.go — MkdirAll, CreateTemp, Remove (rollback), Rename.
	// Path is filepath.Join(outputDir, ...) where outputDir derives from
	// session.DeriveSessionPaths(tmpDir).OutputDir — rooted in the session
	// tmpdir.
	"(*github.com/SoulKyu/cpg/pkg/output.Writer).Write": true,

	// pkg/evidence/writer.go — same atomic shape, rooted in
	// session.DeriveSessionPaths(tmpDir).EvidenceDir.
	"(*github.com/SoulKyu/cpg/pkg/evidence.Writer).Write": true,

	// pkg/hubble/health_writer.go — same atomic shape, writes
	// cluster-health.json under
	// session.DeriveSessionPaths(tmpDir).ClusterHealthPath's parent dir.
	"(*github.com/SoulKyu/cpg/pkg/hubble.healthWriter).finalize": true,

	// NOTE (Phase 22): `cpg bootstrap` deliberately has NO file-write path —
	// it emits its CNP to stdout only. Any fs-writing function reachable from
	// a cobra RunE target would be swept into this audit's reachable set by
	// RTA's documented reflect.Value.Call over-approximation (rta.go:181-206:
	// once any reflect.Value.Call site is reachable, every address-taken
	// function — including all RunE values — gains a synthetic edge), forcing
	// a false-positive allowlist entry here. Keeping the command stdout-only
	// keeps this allowlist at its five genuinely-reachable entries.
}

// bfsResult is the Stage-2 BFS outcome: which *ssa.Function values are
// reachable from root (over the RTA-computed whole-program callgraph's Out
// edges), plus, for every function other than root itself, the function via
// which it was first discovered — enough to reconstruct one concrete call
// path back to root for the D-04 failure diagnostic (see callPathFrom).
type bfsResult struct {
	visited map[*ssa.Function]bool
	parent  map[*ssa.Function]*ssa.Function
}

// bfsFromRoot performs a plain breadth-first search over cg's Out edges,
// starting at root. Per RESEARCH.md Pattern 1 / Pitfall 1, this walks the
// whole-program RTA graph only far enough to determine WHICH functions are
// reachable — it never inspects what those functions do (that is Stage 3's
// job, restricted afterward to cpg-owned functions only).
func bfsFromRoot(cg *callgraph.Graph, root *ssa.Function) bfsResult {
	res := bfsResult{
		visited: map[*ssa.Function]bool{root: true},
		parent:  map[*ssa.Function]*ssa.Function{},
	}
	rootNode := cg.Nodes[root]
	if rootNode == nil {
		return res
	}
	queue := []*callgraph.Node{rootNode}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, edge := range n.Out {
			callee := edge.Callee
			if callee == nil || callee.Func == nil || res.visited[callee.Func] {
				continue
			}
			res.visited[callee.Func] = true
			res.parent[callee.Func] = n.Func
			queue = append(queue, callee)
		}
	}
	return res
}

// bfsFromRootGenuine is bfsFromRoot's SEC-01-tripwire-safe sibling: it skips
// any edge with a nil Site, which per callgraph.Edge's own doc comment
// (callgraph.go:86-87 — "Site is nil for edges originating in synthetic or
// intrinsic functions, e.g. reflect.Value.Call or the root of the call
// graph") marks a synthetic edge rather than a real call site. Reachability
// computed this way cannot be inflated by any cobra RunE value merely
// existing somewhere in the program (23-RESEARCH.md "SEC-01 Tripwire
// Design"): assigning a function as a RunE field value takes its address,
// and RTA's reflect.Value.Call sweep (rta.go:181-207, visitAddrTakenFunc)
// adds a synthetic edge from that intrinsic to every address-taken function
// in the whole program the instant reflect.Value.Call is itself reachable —
// bootstrap.go:153-157's own comment documents this concretely affecting
// runBootstrap today. A tripwire built on raw bfsFromRoot visited-set
// membership would false-positive on the same grounds the moment
// runAuditWindow exists; this genuine-edge variant is immune to that.
//
// Empirically discovered second sweep (execution-time finding, not in
// 23-RESEARCH.md): RTA applies the identical whole-program address-taken
// sweep to ANY indirect call through a value of the bare, zero-parameter,
// zero-result `func()` type — not merely reflect.Value.Call — and, critically,
// attributes the resulting edges to the REAL call instruction (Site != nil),
// unlike the reflect-specific case the Edge.Site doc comment describes. A
// diagnostic dump of (*pkg/session.Manager).Shutdown's outgoing RTA edges
// showed a single indirect call instruction ("t14()", the `context.CancelFunc`
// value call) connected — with a genuine, non-nil Site — to several hundred
// unrelated whole-program closures of matching signature (runtime internals,
// gRPC internals, and, load-bearing here, every pkg/auditwindow.Manager
// exit-path closure), producing a spurious
// "runMCPServer -> (*session.Manager).Shutdown -> (*auditwindow.Manager).Close$1
// -> ... -> k8s.ExecCiliumDbg" chain with no basis in real program semantics
// (pkg/session imports nothing from pkg/auditwindow — grep confirms zero
// references). Every legitimate seam field this codebase actually dispatches
// through (readFn, setFn, listCEFn, watchCEFn, preconditionFn,
// resolveCurrentIDFn, bootstrapDetectVersion, l7ClientFactory, ...) carries a
// non-trivial signature (at least a context.Context parameter), so excluding
// only the maximally-generic bare-func() indirect-call shape preserves every
// genuine seam-mediated call path (verified: the positive-path proof below
// still finds runAuditWindow -> ... -> k8s.ExecCiliumDbg via the readFn seam,
// whose signature is func(context.Context, string, int64) (bool, error), not
// bare func()) while eliminating this second RTA sweep.
func bfsFromRootGenuine(cg *callgraph.Graph, root *ssa.Function) bfsResult {
	res := bfsResult{
		visited: map[*ssa.Function]bool{root: true},
		parent:  map[*ssa.Function]*ssa.Function{},
	}
	rootNode := cg.Nodes[root]
	if rootNode == nil {
		return res
	}
	queue := []*callgraph.Node{rootNode}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, edge := range n.Out {
			if edge.Site == nil {
				continue // synthetic (reflect.Value.Call sweep or graph root) — not a real call
			}
			if isBareFuncValueDispatch(edge.Site.Common()) {
				continue // RTA's second whole-program sweep (see doc comment above) — not a traceable real call
			}
			callee := edge.Callee
			if callee == nil || callee.Func == nil || res.visited[callee.Func] {
				continue
			}
			res.visited[callee.Func] = true
			res.parent[callee.Func] = n.Func
			queue = append(queue, callee)
		}
	}
	return res
}

// isBareFuncValueDispatch reports whether common is an indirect call
// (neither a static call nor an interface-method invoke) through a value of
// the bare, zero-parameter, zero-result `func()` type — the specific shape
// RTA resolves via a whole-program address-taken sweep rather than any
// traceable dataflow fact (see bfsFromRootGenuine's doc comment).
//
// WR-02 soundness note: pruning this edge is unavoidable (it is the only way
// to stay immune to RTA's spurious cross-package `func()` sweep — the sweep
// even connects to genuinely-cpg-owned closures like
// (*auditwindow.Manager).Close$1, so a callee-package filter cannot
// distinguish it). The accepted cost — dropping the pruned edge's own callee
// closure from reachability — is compensated in the negative half by
// withAnonFuncs, which re-scans the LEXICALLY-nested closures (once/defer/go
// bodies) of every genuinely-reachable cpg-owned function. A hypothetical exec
// constructor inside a cpg-owned once/defer/go closure reachable from
// runMCPServer is therefore still caught, without reintroducing the spurious
// sweep (nesting is lexical, not a callgraph edge).
func isBareFuncValueDispatch(common *ssa.CallCommon) bool {
	if common == nil || common.StaticCallee() != nil || common.IsInvoke() {
		return false
	}
	sig := common.Signature()
	return sig != nil && sig.Params().Len() == 0 && sig.Results().Len() == 0
}

// restrictToCpgOwned restricts a BFS-visited set to cpg's own package tree
// (same prefix-match TestMCPAuditReadonlyReachability's inline loop already
// performs at lines ~288-293) — used ONLY by TestAuditWindowNotReachableFromMCP
// below. TestMCPAuditReadonlyReachability's own inline loop is deliberately
// left untouched (not rewired to call this helper): that test's body must
// stay byte-identical in what it proves, and a few duplicated lines are cheap
// insurance for a zero-tolerance security test.
func restrictToCpgOwned(visited map[*ssa.Function]bool) map[*ssa.Function]bool {
	cpgOwned := make(map[*ssa.Function]bool)
	for f := range visited {
		if f != nil && f.Pkg != nil && f.Pkg.Pkg != nil && strings.HasPrefix(f.Pkg.Pkg.Path(), cpgModulePrefix) {
			cpgOwned[f] = true
		}
	}
	return cpgOwned
}

// callPathFrom reconstructs one concrete call path from root to target using
// the parent pointers bfsFromRoot recorded, rendered as
// "root -> f1 -> f2 -> target" using each function's SSA symbol
// ((*ssa.Function).String()). This is the D-04 re-runnability contract: a
// future author must be able to see immediately what leaked and how it's
// reached from the composition root.
func callPathFrom(res bfsResult, root, target *ssa.Function) string {
	var chain []*ssa.Function
	for cur := target; cur != nil && cur != root; cur = res.parent[cur] {
		chain = append(chain, cur)
	}
	chain = append(chain, root)
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	parts := make([]string, len(chain))
	for i, f := range chain {
		parts[i] = f.String()
	}
	return strings.Join(parts, " -> ")
}

// symbolSet renders a set of *ssa.Function values as their SSA symbol
// strings ((*ssa.Function).String()) so a specific function's presence in a
// reachable set (e.g. cpgOwned) can be asserted on by name — used by WR-01's
// floor check to pin a known-reachable deep writer, making a vacuous audit
// impossible to pass silently.
func symbolSet(fns map[*ssa.Function]bool) []string {
	out := make([]string, 0, len(fns))
	for f := range fns {
		out = append(out, f.String())
	}
	return out
}

// withAnonFuncs expands a set of functions to also include every anonymous
// function LEXICALLY nested within them (transitively, via (*ssa.Function).
// AnonFuncs). This is the WR-02 compensating scan: bfsFromRootGenuine must
// prune bare-func() indirect-dispatch edges to stay immune to RTA's
// whole-program address-taken sweep, but that prune would otherwise cut the
// BFS at a `sync.Once.Do(func(){...})`, `defer func(){...}()`, or
// `go func(){...}()` call and silently drop the closure's body from the
// scanned set — so a hypothetical exec constructor call inside a cpg-owned
// once/defer/go closure reachable from runMCPServer would escape the negative
// assertion (the precise false-negative class WR-02 flags).
//
// Nesting is a purely lexical relationship (a closure is genuinely executed by
// its enclosing function — Once.Do/defer/go all run it), NOT a callgraph edge,
// so expanding along it re-includes exactly those closures WITHOUT reintroducing
// the spurious cross-package sweep: e.g. (*auditwindow.Manager).Close$1 is a
// lexical child of (*auditwindow.Manager).Close (never genuinely reachable from
// MCP), so it stays excluded — only the spurious bare-func() edge ever connected
// it to runMCPServer.
func withAnonFuncs(fns map[*ssa.Function]bool) map[*ssa.Function]bool {
	out := make(map[*ssa.Function]bool, len(fns))
	var add func(f *ssa.Function)
	add = func(f *ssa.Function) {
		if f == nil || out[f] {
			return
		}
		out[f] = true
		for _, anon := range f.AnonFuncs {
			add(anon)
		}
	}
	for f := range fns {
		add(f)
	}
	return out
}

// TestMCPAuditReadonlyReachability is the SEC-01 structural audit: it proves,
// at go test time over the SSA form of the actually-compiled program, that
// no K8s write verb (D-02) and no filesystem write outside the 5 allowlisted
// session/atomic-writer functions (D-03) is reachable from the MCP
// composition root runMCPServer (cmd/cpg/mcp.go). It is re-runnable with
// zero test edits against a future registerXTools call or new tool handler
// (D-04): the callgraph is computed fresh from source on every run, and any
// leak's failure message names the offending function and a call path from
// runMCPServer.
//
// Wall-clock budget: ~45-76s under -race (19-RESEARCH.md Pitfall 5) — this
// is the cost of a whole-program SSA build + RTA over cmd/cpg's full
// dependency graph (Cilium, client-go, cilium/ebpf), not a hang. Do not add
// a -timeout below ~120s for this test.
//
// Design: 19-RESEARCH.md Pattern 1, empirically validated against this exact
// repository (produced exactly the 5 fsWriteAllowlist callers, 0 K8s-write
// hits). Naive whole-program "is X reachable from runMCPServer" reachability
// queries (via either CHA or RTA) are deliberately NOT used: they surface
// 70-102 spurious call paths through third-party interface-dispatch noise
// (RESEARCH.md Anti-Patterns, Pitfalls 1-2). This audit instead (1) computes
// a whole-program callgraph via RTA rooted at main+init, per RTA's own
// documented contract — NOT rooted at runMCPServer directly, which is
// off-label; (2) BFS-restricts that graph's runMCPServer subgraph to
// cpg-owned functions only; (3) directly scans only those functions' own SSA
// call instructions, never recursing into third-party callees.
func TestMCPAuditReadonlyReachability(t *testing.T) {
	// Stage 1: load cmd/cpg + its full dependency graph with type-annotated
	// syntax, then build SSA for the whole program. Tests: false — this
	// audit's own _test.go files (including this one) are deliberately
	// excluded from the analyzed program; only production code is in scope.
	cfg := &packages.Config{Mode: packages.LoadAllSyntax, Tests: false, Dir: "."}
	initial, err := packages.Load(cfg, ".")
	require.NoError(t, err, "packages.Load(cmd/cpg)")
	if packages.PrintErrors(initial) > 0 {
		t.Fatal("packages.Load reported package errors for cmd/cpg (see stderr above) — cannot build a sound SSA program")
	}

	mode := ssa.InstantiateGenerics // required for soundness (matches x/tools/cmd/callgraph)
	prog, pkgs := ssautil.AllPackages(initial, mode)
	prog.Build()

	var mainPkg *ssa.Package
	for _, p := range pkgs {
		if p != nil && p.Pkg != nil && p.Pkg.Name() == "main" {
			mainPkg = p
			break
		}
	}
	require.NotNil(t, mainPkg, "expected an SSA package named \"main\" for cmd/cpg")

	mainFn := mainPkg.Func("main")
	initFn := mainPkg.Func("init")
	require.NotNil(t, mainFn, "cmd/cpg must declare func main()")
	require.NotNil(t, initFn, "cmd/cpg must have a synthesized package initializer")

	root := mainPkg.Func("runMCPServer")
	require.NotNil(t, root, "cmd/cpg must declare func runMCPServer — the MCP composition root this audit's BFS roots at")

	// Stage 2: RTA rooted at main+init per its documented contract (rta.
	// Analyze's own doc: "The root functions must be one or more entrypoints
	// (main and init functions) of a complete SSA program") — NOT rooted at
	// runMCPServer directly (off-label; empirically confirmed unnecessary,
	// see RESEARCH.md Anti-Patterns). runMCPServer is used only as the BFS
	// start node over the resulting whole-program graph.
	rtaRes := rta.Analyze([]*ssa.Function{mainFn, initFn}, true)
	require.NotNil(t, rtaRes, "rta.Analyze returned nil (no roots supplied?)")
	require.NotNil(t, rtaRes.CallGraph, "rta.Analyze(roots, buildCallGraph=true) must populate CallGraph")

	// WR-01: prove root is genuinely a node in the RTA callgraph BEFORE
	// trusting the BFS below. bfsFromRoot unconditionally seeds its result
	// with {root: true} and returns exactly that seed when
	// cg.Nodes[root] == nil (see its early-return above), so a self-check of
	// bfsRes.visited[root] is always true regardless of whether root ever
	// reached the graph — this is the real guard against that vacuous case.
	require.NotNil(t, rtaRes.CallGraph.Nodes[root],
		"runMCPServer must be a node in the RTA callgraph — otherwise the BFS scans nothing and this audit passes vacuously")

	bfsRes := bfsFromRoot(rtaRes.CallGraph, root)

	// Restrict to cpg's own package tree — this is what keeps Stage 3 small
	// and precise; third-party functions in the visited set are never
	// individually scanned (Pitfall 1).
	cpgOwned := make(map[*ssa.Function]bool)
	for f := range bfsRes.visited {
		if f != nil && f.Pkg != nil && f.Pkg.Pkg != nil && strings.HasPrefix(f.Pkg.Pkg.Path(), cpgModulePrefix) {
			cpgOwned[f] = true
		}
	}
	// WR-01: a floor of exactly {runMCPServer} (len == 1) would mean the BFS
	// never descended past the root — the audit would be scanning nothing.
	// Pinning a known-reachable deep writer additionally proves the BFS
	// descended all the way into the session/output writer subsystem, not
	// merely into some unrelated cpg-owned function.
	require.Greater(t, len(cpgOwned), 1,
		"expected runMCPServer to transitively reach cpg-owned functions; a size of 1 means the audit is vacuous")
	require.Contains(t, symbolSet(cpgOwned), "(*github.com/SoulKyu/cpg/pkg/session.Manager).Start",
		"the session writer subsystem must be reachable from runMCPServer for this audit to be meaningful")

	// Stage 3: direct call-instruction scan of each cpg-owned reachable
	// function's OWN body only — never recurse into a callee's body, even a
	// cpg-owned one already covered by iterating cpgOwned itself.
	//
	// IN-01 (known completeness gap): this scan handles only
	// common.StaticCallee() != nil (Property 2) and common.IsInvoke()
	// (Property 1). A CallInstruction dispatched through a func value (e.g.
	// `w := os.WriteFile; w(path, data, 0o644)`) has a nil StaticCallee()
	// and is not an invoke, so it is scanned by neither property and would
	// go undetected. No cpg code currently dispatches a filesystem/K8s
	// write through a func value, so this is a soundness completeness gap
	// rather than a live miss; a full fix would resolve func-value callees
	// against the RTA graph's Out edges for the call site.
	for f := range cpgOwned {
		for _, b := range f.Blocks {
			for _, instr := range b.Instrs {
				call, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				common := call.Common()

				if callee := common.StaticCallee(); callee != nil {
					// Property 2 (D-03): static call to a watched fs-write function.
					if disallowedFSWrite[callee.String()] && !fsWriteAllowlist[f.String()] {
						t.Errorf("SEC-01: unallowlisted filesystem write — %s calls %s\n  call path from runMCPServer: %s",
							f.String(), callee.String(), callPathFrom(bfsRes, root, f))
					}
				} else if common.IsInvoke() && common.Method != nil {
					// Property 1 (D-02): interface-dispatch call whose method
					// name is a K8s write verb. Verb-name-only, no
					// receiver-type filtering, no allowlist — baseline is
					// zero, any hit is new.
					if k8sWriteVerbs[common.Method.Name()] {
						t.Errorf("SEC-01: reachable K8s write verb — %s calls %s via interface dispatch\n  call path from runMCPServer: %s",
							f.String(), common.Method.Name(), callPathFrom(bfsRes, root, f))
					}
				}
			}
		}
	}
}

// execConstructorSymbol is the exact StaticCallee().String() form of the
// privileged exec-executor constructor Property 3 watches for
// (AUD-04/T-23-08): remotecommand.NewSPDYExecutor, the SPDY dialer entry
// point pkg/k8s/exec.go's ExecPolicyAuditMode/ReadPolicyAuditMode ultimately
// call through to actually perform a `pods/exec` mutation.
const execConstructorSymbol = "k8s.io/client-go/tools/remotecommand.NewSPDYExecutor"

// TestAuditWindowNotReachableFromMCP is the SEC-01 structural evolution
// (AUD-04, Property 3): it proves, over the same whole-program SSA/RTA setup
// TestMCPAuditReadonlyReachability already uses, that no cpg-owned function
// GENUINELY (non-reflect-swept) reachable from runMCPServer contains a
// static call instruction to remotecommand.NewSPDYExecutor — the negative
// half — AND that the same call IS genuinely reachable from runAuditWindow
// — the non-vacuous positive half (require.True; a vacuous "not reachable
// from MCP" proof that also isn't reachable from anywhere is worthless).
//
// "Genuinely reachable" uses bfsFromRootGenuine (Edge.Site != nil only,
// filtering RTA's reflect.Value.Call sweep), specifically so a cobra RunE
// value's mere existence in the compiled binary can never, by itself,
// satisfy or violate this property — only a REAL call chain can
// (23-RESEARCH.md "SEC-01 Tripwire Design"). This deliberately duplicates
// TestMCPAuditReadonlyReachability's Stage 1-2 SSA/RTA setup rather than
// sharing state across tests, so this test remains independently re-runnable
// and its own failure diagnostics are self-contained.
//
// Wall-clock budget: ~45-76s under -race, same as TestMCPAuditReadonlyReachability
// (19-RESEARCH.md Pitfall 5) — a whole-program SSA build + RTA cost, not a
// hang. Do not add a -timeout below ~120s for this test.
func TestAuditWindowNotReachableFromMCP(t *testing.T) {
	// Stage 1: identical to TestMCPAuditReadonlyReachability's own Stage 1 —
	// load cmd/cpg + its full dependency graph with type-annotated syntax,
	// build SSA for the whole program, excluding this package's own _test.go
	// files.
	cfg := &packages.Config{Mode: packages.LoadAllSyntax, Tests: false, Dir: "."}
	initial, err := packages.Load(cfg, ".")
	require.NoError(t, err, "packages.Load(cmd/cpg)")
	if packages.PrintErrors(initial) > 0 {
		t.Fatal("packages.Load reported package errors for cmd/cpg (see stderr above) — cannot build a sound SSA program")
	}

	mode := ssa.InstantiateGenerics // required for soundness (matches x/tools/cmd/callgraph)
	prog, pkgs := ssautil.AllPackages(initial, mode)
	prog.Build()

	var mainPkg *ssa.Package
	for _, p := range pkgs {
		if p != nil && p.Pkg != nil && p.Pkg.Name() == "main" {
			mainPkg = p
			break
		}
	}
	require.NotNil(t, mainPkg, "expected an SSA package named \"main\" for cmd/cpg")

	mainFn := mainPkg.Func("main")
	initFn := mainPkg.Func("init")
	require.NotNil(t, mainFn, "cmd/cpg must declare func main()")
	require.NotNil(t, initFn, "cmd/cpg must have a synthesized package initializer")

	mcpRoot := mainPkg.Func("runMCPServer")
	require.NotNil(t, mcpRoot, "cmd/cpg must declare func runMCPServer")
	auditRoot := mainPkg.Func("runAuditWindow")
	require.NotNil(t, auditRoot, "cmd/cpg must declare func runAuditWindow")

	// Stage 2: RTA rooted at main+init, per its documented contract — not
	// rooted at either mcpRoot or auditRoot directly (same off-label concern
	// TestMCPAuditReadonlyReachability's own Stage 2 comment documents).
	rtaRes := rta.Analyze([]*ssa.Function{mainFn, initFn}, true)
	require.NotNil(t, rtaRes, "rta.Analyze returned nil (no roots supplied?)")
	require.NotNil(t, rtaRes.CallGraph, "rta.Analyze(roots, buildCallGraph=true) must populate CallGraph")
	require.NotNil(t, rtaRes.CallGraph.Nodes[mcpRoot],
		"runMCPServer must be a node in the RTA callgraph — otherwise the BFS scans nothing and this half of the audit passes vacuously")
	require.NotNil(t, rtaRes.CallGraph.Nodes[auditRoot],
		"runAuditWindow must be a node in the RTA callgraph — otherwise the positive-path proof below is vacuous")

	// Assumption A1 (23-RESEARCH.md): log whether reflect.Value.Call was
	// itself genuinely reached from runMCPServer, so the team knows which
	// reflect-sweep-pollution case they're in — informational only, not an
	// assertion (the Edge.Site filter below is correct and safe either way).
	genuineFromMCPAll := bfsFromRootGenuine(rtaRes.CallGraph, mcpRoot)
	reflectValueCallReached := false
	for f := range genuineFromMCPAll.visited {
		if f != nil && f.String() == "(reflect.Value).Call" {
			reflectValueCallReached = true
			break
		}
	}
	t.Logf("Assumption A1: (reflect.Value).Call genuinely reached from runMCPServer = %v", reflectValueCallReached)

	// Negative half (Property 3): no cpg-owned function genuinely reachable
	// from runMCPServer may contain a static call instruction to
	// remotecommand.NewSPDYExecutor.
	genuineCpgOwnedFromMCP := restrictToCpgOwned(genuineFromMCPAll.visited)

	// WR-03: non-vacuity floor for the negative half. Without this, an
	// over-pruned or refactor-broken BFS that reduced the genuine MCP-reachable
	// set to near-empty would pass this security assertion silently (Property 3
	// is not covered by the unfiltered sibling test). runMCPServer STATICALLY
	// calls session.NewManager (cmd/cpg/mcp.go) — a guaranteed genuine
	// (non-bare, non-synthetic) edge — so that symbol MUST be present; its
	// absence means the BFS is broken and the audit is worthless.
	require.Greater(t, len(genuineCpgOwnedFromMCP), 1,
		"the genuine MCP-reachable cpg-owned set must be non-trivially populated; a size of 1 means the BFS never descended and this security half passes vacuously (WR-03)")
	require.Contains(t, symbolSet(genuineCpgOwnedFromMCP), "github.com/SoulKyu/cpg/pkg/session.NewManager",
		"session.NewManager is a static callee of runMCPServer and MUST appear in the genuine MCP-reachable set; its absence means the BFS is broken and the negative assertion below is vacuous (WR-03)")

	// WR-02: scan not only the genuinely-reachable functions but also their
	// lexically-nested closures (once/defer/go bodies), which bfsFromRootGenuine
	// necessarily prunes at the bare-func() edge — so an exec constructor buried
	// inside such a closure of an MCP-reachable cpg function is still caught.
	for f := range withAnonFuncs(genuineCpgOwnedFromMCP) {
		for _, b := range f.Blocks {
			for _, instr := range b.Instrs {
				call, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				if callee := call.Common().StaticCallee(); callee != nil && callee.String() == execConstructorSymbol {
					// f may be a nested closure not itself in the BFS parent
					// map; anchor the diagnostic path on its outermost lexical
					// ancestor, which IS a genuinely-reachable function.
					enclosing := f
					for enclosing.Parent() != nil {
						enclosing = enclosing.Parent()
					}
					t.Errorf("SEC-01/AUD-04: %s genuinely (non-reflect) reaches %s from runMCPServer\n  call path: %s",
						f.String(), execConstructorSymbol, callPathFrom(genuineFromMCPAll, mcpRoot, enclosing))
				}
			}
		}
	}

	// Positive half (AUD-04's "reachable ONLY from audit-window" other side):
	// the exec constructor must be genuinely reachable from runAuditWindow —
	// non-vacuous proof that this audit is actually watching something real.
	genuineFromAudit := bfsFromRootGenuine(rtaRes.CallGraph, auditRoot)
	foundExecCaller := false
	for f := range restrictToCpgOwned(genuineFromAudit.visited) {
		for _, b := range f.Blocks {
			for _, instr := range b.Instrs {
				call, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				if callee := call.Common().StaticCallee(); callee != nil && callee.String() == execConstructorSymbol {
					foundExecCaller = true
					t.Logf("SEC-01/AUD-04: exec constructor genuinely reachable via %s",
						callPathFrom(genuineFromAudit, auditRoot, f))
				}
			}
		}
	}
	require.True(t, foundExecCaller,
		"SEC-01/AUD-04: remotecommand.NewSPDYExecutor must be genuinely reachable from runAuditWindow — audit is vacuous otherwise")
}
