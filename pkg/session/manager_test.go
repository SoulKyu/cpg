package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/SoulKyu/cpg/pkg/flowsource"
	"github.com/SoulKyu/cpg/pkg/hubble"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)

// closedFlowSource emits a fixed list of flows then closes both channels
// immediately, so RunPipelineWithSource drains it to completion on its own
// (no ctx cancellation needed) — for Start/Status file-count/Stop-summary
// tests. Mirrors pkg/hubble/pipeline_test.go's unexported mockFlowSource;
// redefined here because that type cannot be imported across packages.
type closedFlowSource struct {
	flows []*flowpb.Flow
}

func (c *closedFlowSource) StreamDroppedFlows(_ context.Context, _ []string, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	flowCh := make(chan *flowpb.Flow, len(c.flows))
	lostCh := make(chan *flowpb.LostEvent)
	for _, f := range c.flows {
		flowCh <- f
	}
	close(flowCh)
	close(lostCh)
	return flowCh, lostCh, nil
}

// blockingFlowSource emits at most one initial flow, then blocks until ctx
// is cancelled before closing both channels — keeps a session in
// StateCapturing for as long as the test needs it. Mirrors pipeline_test.go's
// channelFlowSource + ctx-cancel idiom, redefined locally for the same
// cross-package reason as closedFlowSource above.
type blockingFlowSource struct {
	flow *flowpb.Flow
}

func (b *blockingFlowSource) StreamDroppedFlows(ctx context.Context, _ []string, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	flowCh := make(chan *flowpb.Flow, 1)
	lostCh := make(chan *flowpb.LostEvent)
	if b.flow != nil {
		flowCh <- b.flow
	}
	go func() {
		<-ctx.Done()
		close(flowCh)
		close(lostCh)
	}()
	return flowCh, lostCh, nil
}

// wedgedRunPipeline returns a Manager.runPipeline replacement that ignores
// ctx entirely and blocks until release is closed — simulating a pipeline
// step that never observes cancellation, to prove Shutdown's bounded fan-out
// still returns promptly (SESS-05) even then.
func wedgedRunPipeline(release <-chan struct{}) func(context.Context, hubble.PipelineConfig) error {
	return func(_ context.Context, _ hubble.PipelineConfig) error {
		<-release
		return nil
	}
}

// failingRunPipeline returns a Manager.runPipeline replacement that blocks
// until either release closes (returning err, a genuine non-context error)
// or ctx is cancelled (returning ctx.Err(), a context.Canceled/
// DeadlineExceeded cancellation). Unlike wedgedRunPipeline above, this
// stand-in DOES observe ctx cancellation, so a cleanup Shutdown can still
// unblock it even if the test never closes release itself.
func failingRunPipeline(release <-chan struct{}, err error) func(context.Context, hubble.PipelineConfig) error {
	return func(ctx context.Context, _ hubble.PipelineConfig) error {
		select {
		case <-release:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// someFlow returns a single dropped ingress TCP flow for tests that only
// need a session to look "active" (blockingFlowSource callers).
func someFlow() *flowpb.Flow {
	return testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"production",
		8080,
	)
}

// twoFlows returns two distinct dropped flows — the same fixture
// pkg/hubble/pipeline_test.go's TestRunPipeline_OnFinalFiresOnce uses to
// prove FlowsSeen == 2 downstream of the aggregator.
func twoFlows() []*flowpb.Flow {
	return []*flowpb.Flow{
		testdata.IngressTCPFlow(
			[]string{"k8s:app=client"},
			[]string{"k8s:app=server"},
			"production",
			8080,
		),
		testdata.EgressUDPFlow(
			[]string{"k8s:app=server"},
			[]string{"k8s:app=dns"},
			"production",
			53,
		),
	}
}

// newTestManager builds a Manager wired to source via an injected
// runPipeline (hubble.RunPipelineWithSource + the fake — no real cluster, no
// MCP SDK) with bounded deadlines shrunk to 100ms so every bounded-wait path
// in the suite runs fast.
func newTestManager(t *testing.T, source flowsource.FlowSource) *Manager {
	t.Helper()
	m := NewManager(context.Background(), zaptest.NewLogger(t), &bytes.Buffer{}, "vTest")
	m.runPipeline = func(ctx context.Context, cfg hubble.PipelineConfig) error {
		return hubble.RunPipelineWithSource(ctx, cfg, source)
	}
	m.stopWait = 100 * time.Millisecond
	m.removeWait = 100 * time.Millisecond
	return m
}

// startOutcome pairs a Start() result with its error, for tests that race
// multiple concurrent Start calls and collect each goroutine's outcome.
type startOutcome struct {
	res StartResult
	err error
}

// stopOutcome mirrors startOutcome for concurrent Stop() calls.
type stopOutcome struct {
	res StopResult
	err error
}

// TestManager_Start proves SESS-01: Start returns quickly (non-blocking),
// an opaque sess_<uuid> id, and a tmpdir that exists on disk immediately.
func TestManager_Start(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	begin := time.Now()
	result, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	elapsed := time.Since(begin)
	require.NoError(t, err)
	assert.Less(t, elapsed, m.stopWait, "Start must return quickly, not block on the pipeline")
	assert.True(t, strings.HasPrefix(result.SessionID, "sess_"))

	status, err := m.Status(result.SessionID)
	require.NoError(t, err)
	assert.Equal(t, "capturing", status.State)
	_, statErr := os.Stat(status.TmpDir)
	assert.NoError(t, statErr, "session tmpdir should exist")

	m.Shutdown()
}

// TestManager_Start_RejectsConcurrent proves SESS-02's sequential case: a
// second start_session while a session is already capturing is rejected
// with an actionable error naming the active session_id.
func TestManager_Start_RejectsConcurrent(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	first, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	_, err = m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), first.SessionID)
	assert.Contains(t, err.Error(), "already running")

	m.Shutdown()
}

// TestManager_Start_ConcurrentStartRejectsSecond proves SESS-02 under TRUE
// concurrency (not just sequential ordering): two goroutines race Start on
// an idle Manager, gated on a shared start barrier. Exactly one must win the
// slot and no orphaned session/tmpdir may remain.
func TestManager_Start_ConcurrentStartRejectsSecond(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	outcomes := make([]startOutcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
			outcomes[i] = startOutcome{res: res, err: err}
		}(i)
	}
	close(start) // release both goroutines together to race the slot claim
	wg.Wait()

	var winners, losers int
	var winnerID, loserID string
	for _, o := range outcomes {
		if o.err == nil {
			winners++
			winnerID = o.res.SessionID
			assert.True(t, strings.HasPrefix(o.res.SessionID, "sess_"))
		} else {
			losers++
			loserID = o.res.SessionID
			assert.Contains(t, o.err.Error(), "already running")
		}
	}
	require.Equal(t, 1, winners, "exactly one concurrent Start should win the slot")
	require.Equal(t, 1, losers, "exactly one concurrent Start should be rejected")

	status, err := m.Status(winnerID)
	require.NoError(t, err)
	winnerTmpDir := status.TmpDir
	_, statErr := os.Stat(winnerTmpDir)
	assert.NoError(t, statErr, "the winner's tmpdir should exist — no orphan")

	if loserID != "" {
		_, err := m.Status(loserID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found or expired")
	}

	m.Shutdown()
	_, statErr = os.Stat(winnerTmpDir)
	assert.True(t, os.IsNotExist(statErr), "winner's tmpdir should be removed after Shutdown")
}

// TestManager_Start_PurgesStoppedSession proves D-04: starting while a
// stopped session is retained silently purges the old tmpdir and notes the
// discarded id, rather than rejecting the new start.
func TestManager_Start_PurgesStoppedSession(t *testing.T) {
	m := newTestManager(t, &closedFlowSource{flows: twoFlows()})

	first, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	stopRes, err := m.Stop(first.SessionID)
	require.NoError(t, err)
	oldTmpDir := stopRes.TmpDir

	second, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)
	assert.Equal(t, first.SessionID, second.DiscardedSession)

	_, statErr := os.Stat(oldTmpDir)
	assert.True(t, os.IsNotExist(statErr), "old tmpdir should be purged")

	status, err := m.Status(second.SessionID)
	require.NoError(t, err)
	assert.Equal(t, "capturing", status.State)

	m.Shutdown()
}

// TestManager_Status proves SESS-03: coarse state/elapsed/file counts for a
// capturing session, and that Elapsed freezes once the session is stopped.
func TestManager_Status(t *testing.T) {
	m := newTestManager(t, &closedFlowSource{flows: twoFlows()})

	res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		status, err := m.Status(res.SessionID)
		return err == nil && status.PolicyFileCount > 0
	}, 5*time.Second, 5*time.Millisecond, "policy file should eventually land under the session tmpdir")

	status, err := m.Status(res.SessionID)
	require.NoError(t, err)
	assert.NotEmpty(t, status.State)
	assert.NotEmpty(t, status.Elapsed)
	assert.GreaterOrEqual(t, status.EvidenceFileCount, 0)
	assert.NotEmpty(t, status.TmpDir)

	_, err = m.Stop(res.SessionID)
	require.NoError(t, err)

	status1, err := m.Status(res.SessionID)
	require.NoError(t, err)
	status2, err := m.Status(res.SessionID)
	require.NoError(t, err)
	assert.Equal(t, status1.Elapsed, status2.Elapsed, "elapsed must be frozen after stop, not still ticking")

	m.Shutdown()
}

// TestManager_Status_StoppedSessionStaysQueryable proves D-02: a retained
// stopped session_id does NOT trigger SESS-06 on get_status.
func TestManager_Status_StoppedSessionStaysQueryable(t *testing.T) {
	m := newTestManager(t, &closedFlowSource{flows: twoFlows()})

	res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	_, err = m.Stop(res.SessionID)
	require.NoError(t, err)

	status, err := m.Status(res.SessionID)
	require.NoError(t, err, "a retained stopped session must stay queryable, never SESS-06")
	assert.Equal(t, "stopped", status.State)

	m.Shutdown()
}

// TestManager_Stop proves SESS-04: Stop cancels+finalizes and returns a
// summary fed by the OnFinal hook, and D-01: the tmpdir survives Stop.
func TestManager_Stop(t *testing.T) {
	m := newTestManager(t, &closedFlowSource{flows: twoFlows()})

	res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	// Wait for the pipeline to naturally finish draining both flows (the
	// policy file landing on disk proves the aggregator already processed
	// and counted both) before calling Stop. Calling Stop immediately would
	// race Stop's own ctx cancellation against the aggregator's channel
	// drain: Go's select does not guarantee draining an already-ready
	// channel over an also-ready ctx.Done(), which would make FlowsSeen
	// non-deterministic (0, 1, or 2 depending on scheduling).
	require.Eventually(t, func() bool {
		status, statusErr := m.Status(res.SessionID)
		return statusErr == nil && status.PolicyFileCount > 0
	}, 5*time.Second, 5*time.Millisecond, "pipeline should finish processing both flows before Stop is called")

	stopRes, err := m.Stop(res.SessionID)
	require.NoError(t, err)
	assert.False(t, stopRes.AlreadyStopped)
	assert.Equal(t, uint64(2), stopRes.FlowsSeen, "OnFinal must have fed the summary")
	assert.NotEmpty(t, stopRes.ClusterHealthPath)
	assert.True(t, strings.HasSuffix(stopRes.ClusterHealthPath, "cluster-health.json"))
	assert.Empty(t, stopRes.Error, "a clean drain must carry no error")

	_, statErr := os.Stat(stopRes.TmpDir)
	assert.NoError(t, statErr, "tmpdir must survive stop (D-01 retention)")

	m.Shutdown()
}

// TestManager_Stop_Idempotent proves D-03: a second stop on the same id
// returns the same summary with an already-stopped marker, never an error.
func TestManager_Stop_Idempotent(t *testing.T) {
	m := newTestManager(t, &closedFlowSource{flows: twoFlows()})

	res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	first, err := m.Stop(res.SessionID)
	require.NoError(t, err)

	second, err := m.Stop(res.SessionID)
	require.NoError(t, err)
	assert.True(t, second.AlreadyStopped)
	assert.Equal(t, first.FlowsSeen, second.FlowsSeen)

	m.Shutdown()
}

// TestManager_UnknownSessionID proves SESS-06: an unknown session_id on
// either get_status or stop_session returns the crisp "not found or
// expired" error, never a generic failure.
func TestManager_UnknownSessionID(t *testing.T) {
	m := newTestManager(t, &closedFlowSource{flows: twoFlows()})

	_, err := m.Status("sess_bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found or expired")

	_, err = m.Stop("sess_bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found or expired")
}

// TestManager_ConcurrentStop proves Pitfall F: two concurrent Stop calls for
// the same session both return promptly (bounded by stopWait, not 2x it)
// with no error and a consistent summary — sync.Once serializes the actual
// teardown so neither caller pays a redundant full timeout.
func TestManager_ConcurrentStop(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	outcomes := make([]stopOutcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			r, e := m.Stop(res.SessionID)
			outcomes[i] = stopOutcome{res: r, err: e}
		}(i)
	}

	begin := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(begin)

	assert.Less(t, elapsed, 2*m.stopWait, "both concurrent stops should return promptly, not each pay the full timeout")
	for _, o := range outcomes {
		require.NoError(t, o.err)
	}
	// AlreadyStopped may legitimately differ between the two callers
	// (whichever call's very first read observes the state already flipped
	// reports true), but the underlying teardown result is identical either
	// way — both reads happen only after the single real teardown completed.
	assert.Equal(t, outcomes[0].res.FlowsSeen, outcomes[1].res.FlowsSeen)
	assert.Equal(t, outcomes[0].res.Duration, outcomes[1].res.Duration)
	assert.Equal(t, outcomes[0].res.ClusterHealthPath, outcomes[1].res.ClusterHealthPath)

	m.Shutdown()
}

// TestManager_ConcurrentStatusAndStop hammers Status in a tight loop while
// Stop runs concurrently on the same session, proving State/StoppedAt are
// only ever touched under m.mu (zero -race reports) and that a final Status
// after Stop reports a frozen "stopped" state.
func TestManager_ConcurrentStatusAndStop(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	stopLooping := make(chan struct{})
	looperDone := make(chan struct{})
	go func() {
		defer close(looperDone)
		for {
			select {
			case <-stopLooping:
				return
			default:
				_, _ = m.Status(res.SessionID)
			}
		}
	}()

	_, err = m.Stop(res.SessionID)
	require.NoError(t, err)

	close(stopLooping)
	<-looperDone

	status1, err := m.Status(res.SessionID)
	require.NoError(t, err)
	assert.Equal(t, "stopped", status1.State)

	status2, err := m.Status(res.SessionID)
	require.NoError(t, err)
	assert.Equal(t, status1.Elapsed, status2.Elapsed, "elapsed must be frozen once stopped")

	m.Shutdown()
}

// TestManager_ConcurrentShutdownAndStop races Stop and Shutdown against each
// other on the same active session: both must return within a small
// multiple of the bounded deadlines, Stop must never panic regardless of
// interleaving, and the tmpdir must be gone afterward. Shutdown nils
// m.session immediately on entry (Task 1's explicit lock discipline), so a
// concurrent Stop that loses the lock race legitimately observes the slot
// already cleared and returns the well-formed SESS-06 "not found or
// expired" error instead of a summary — that is correct SESS-06/D-02
// behavior (Shutdown is, in effect, an unconditional purge), not a bug.
func TestManager_ConcurrentShutdownAndStop(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	status, err := m.Status(res.SessionID)
	require.NoError(t, err)
	tmpDir := status.TmpDir

	start := make(chan struct{})
	var wg sync.WaitGroup
	var stopErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, stopErr = m.Stop(res.SessionID)
	}()
	go func() {
		defer wg.Done()
		<-start
		m.Shutdown()
	}()

	begin := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(begin)

	assert.Less(t, elapsed, 4*(m.stopWait+m.removeWait), "both should return within a small multiple of the bounded deadlines")
	// If Stop lost the lock race against Shutdown's immediate m.session=nil,
	// it must still fail gracefully with exactly the SESS-06 shape — never a
	// nil-pointer panic or some other malformed error.
	if stopErr != nil {
		assert.Contains(t, stopErr.Error(), "not found or expired")
	}

	_, statErr := os.Stat(tmpDir)
	assert.True(t, os.IsNotExist(statErr), "tmpdir should be removed after Shutdown")
}

// TestManager_Shutdown proves SESS-05: Shutdown cancels the active session,
// bounded-waits for pipeline exit, and removes the tmpdir.
func TestManager_Shutdown(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	status, err := m.Status(res.SessionID)
	require.NoError(t, err)
	tmpDir := status.TmpDir

	begin := time.Now()
	m.Shutdown()
	elapsed := time.Since(begin)

	assert.Less(t, elapsed, 4*(m.stopWait+m.removeWait), "shutdown should return promptly")
	_, statErr := os.Stat(tmpDir)
	assert.True(t, os.IsNotExist(statErr))
}

// TestManager_Shutdown_WedgedStepDoesNotBlock proves the SESS-05 bounded
// fan-out: even when the pipeline step never observes ctx cancellation,
// Shutdown still returns within a small multiple of stopWait+removeWait.
func TestManager_Shutdown_WedgedStepDoesNotBlock(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	m.runPipeline = wedgedRunPipeline(release)

	_, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		m.Shutdown()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(4 * (m.stopWait + m.removeWait)):
		t.Fatal("Shutdown did not return within the bounded deadline despite a wedged pipeline step")
	}
}

// TestManager_Start_ShutdownRacesSetup proves the finalize guard: a
// Shutdown that fires while a Start is still mid-setup (gated via the
// injectable resolveSetupFn seam, before the tmpdir is published onto the
// session) makes that Start abort cleanly through the m.session != s check,
// with Shutdown itself returning bounded and no orphaned tmpdir/session.
func TestManager_Start_ShutdownRacesSetup(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(closeRelease)

	m.resolveSetupFn = func(_ context.Context, _ StartArgs) (string, func(), map[string]*ciliumv2.CiliumNetworkPolicy, error) {
		close(entered)
		<-release
		return "bypass:1", func() {}, nil, nil
	}

	before, err := filepath.Glob(filepath.Join(os.TempDir(), "cpg-session-*"))
	require.NoError(t, err)

	startDone := make(chan startOutcome, 1)
	go func() {
		res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
		startDone <- startOutcome{res: res, err: err}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Start never reached the setup seam")
	}

	shutdownDone := make(chan struct{})
	go func() {
		m.Shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
	case <-time.After(4 * (m.stopWait + m.removeWait)):
		t.Fatal("Shutdown did not return within the bounded deadline while a Start was mid-setup")
	}

	closeRelease()

	var outcome startOutcome
	select {
	case outcome = <-startDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Start never returned after the setup seam was released")
	}
	require.Error(t, outcome.err)
	errText := outcome.err.Error()
	assert.True(t, strings.Contains(errText, "shutting down") || strings.Contains(errText, "aborted"),
		"error should indicate the finalize guard fired: %q", errText)

	after, err := filepath.Glob(filepath.Join(os.TempDir(), "cpg-session-*"))
	require.NoError(t, err)
	assert.Len(t, after, len(before), "no orphaned session tmpdir should remain after Shutdown raced Start's setup window")

	_, err = m.Status("sess_anything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found or expired", "the slot must be nil after Shutdown")
}

// TestManager_Start_SetupFailureRollsBackSlot proves a failed synchronous
// setup releases the single slot rather than wedging it: the pre-failure
// tmpdir is removed and a subsequent Start can claim the slot again.
func TestManager_Start_SetupFailureRollsBackSlot(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	m.resolveSetupFn = func(_ context.Context, _ StartArgs) (string, func(), map[string]*ciliumv2.CiliumNetworkPolicy, error) {
		return "", func() {}, nil, fmt.Errorf("injected setup failure")
	}

	before, err := filepath.Glob(filepath.Join(os.TempDir(), "cpg-session-*"))
	require.NoError(t, err)

	_, startErr := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.Error(t, startErr)
	assert.Contains(t, startErr.Error(), "injected setup failure")

	after, err := filepath.Glob(filepath.Join(os.TempDir(), "cpg-session-*"))
	require.NoError(t, err)
	assert.Len(t, after, len(before), "the pre-failure tmpdir must be removed, not orphaned")

	m.resolveSetupFn = func(_ context.Context, _ StartArgs) (string, func(), map[string]*ciliumv2.CiliumNetworkPolicy, error) {
		return "bypass:1", func() {}, nil, nil
	}

	res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err, "the failed setup must release the slot, not wedge it")
	assert.True(t, strings.HasPrefix(res.SessionID, "sess_"))

	m.Shutdown()
}

// TestManager_PipelineErrorAutonomouslyStopsSession proves WR-01 (Truth 2 /
// SESS-03 reopened gap): a pipeline that exits on its own with a genuine
// (non-context-cancellation) error autonomously transitions the session to
// stopped and surfaces the error on both get_status and stop_session, with
// zero stop_session calls required to observe the transition. This
// exercises the launch goroutine's s.pipelineErr.Store + guarded State
// transition (manager.go) and Session.pipelineErr's surfacing through
// Status/buildSummary (session.go) — indirectly, through the public
// Start/Status/Stop API, since pipelineErr is unexported.
//
// Load-bearing property: the post-close(fail) assertion state == "stopped"
// is FALSE under the pre-fix launch goroutine, which discarded the pipeline
// error onto s.done and never transitioned State — this test fails against
// that old behavior, which is the point.
func TestManager_PipelineErrorAutonomouslyStopsSession(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	fail := make(chan struct{})
	sentinel := errors.New("relay connection reset by peer")
	m.runPipeline = failingRunPipeline(fail, sentinel)

	res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)

	status, err := m.Status(res.SessionID)
	require.NoError(t, err)
	assert.Equal(t, "capturing", status.State, "session must be live before the pipeline fails")
	assert.Empty(t, status.Error, "no crash has occurred yet")

	close(fail)

	require.Eventually(t, func() bool {
		status, statusErr := m.Status(res.SessionID)
		return statusErr == nil && status.State == "stopped" && strings.Contains(status.Error, "relay connection reset")
	}, 5*time.Second, 5*time.Millisecond,
		"session must autonomously transition to stopped and surface the pipeline error, with zero stop_session calls")

	stopRes, err := m.Stop(res.SessionID)
	require.NoError(t, err)
	assert.Contains(t, stopRes.Error, "relay connection reset",
		"stop_session must surface the same crash error, distinguishable from a clean stop")

	m.Shutdown()
}

// TestManager_ScopedDialTimeoutAutonomouslyStopsSession proves WR-01 (fresh
// gap found post-17-05, narrower than the original Truth 2 gap): a pipeline
// that fails with a SCOPED context.DeadlineExceeded — derived from the
// still-healthy sessionCtx, exactly the shape pkg/hubble/client.go:109-121's
// waitForConnReady returns on an unreachable/typo'd --server address — must
// still autonomously transition the session to stopped. The pre-fix guard
// classified purely on the returned error's KIND via
// errors.Is(err, context.DeadlineExceeded), which matches this scoped
// timeout too even though sessionCtx itself was never cancelled — the fix
// classifies on sessionCtx.Err() instead, the only true "this was on
// purpose" signal.
//
// Load-bearing property: the Eventually assertion below is FALSE against the
// pre-fix launch-goroutine guard — a scoped DeadlineExceeded satisfies the
// old errors.Is filter and is discarded, so State never leaves
// StateCapturing and get_status reports "capturing" forever for exactly
// this realistic connection-failure mode. This test fails against that old
// behavior, which is the point.
func TestManager_ScopedDialTimeoutAutonomouslyStopsSession(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	// Mirrors waitForConnReady exactly: derive a scoped dialCtx from the
	// passed (healthy) ctx, block until it expires, then return a wrapped
	// DeadlineExceeded from the CHILD ctx. The parent — sessionCtx, == the
	// ctx passed here — is never cancelled on this path.
	m.runPipeline = func(ctx context.Context, _ hubble.PipelineConfig) error {
		dialCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		<-dialCtx.Done()
		return fmt.Errorf("connecting to hubble relay %q: %w", "bad:1", dialCtx.Err())
	}

	res, err := m.Start(context.Background(), StartArgs{Server: "bad:1"})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		status, statusErr := m.Status(res.SessionID)
		return statusErr == nil && status.State == "stopped" && strings.Contains(status.Error, "connecting to hubble relay")
	}, 5*time.Second, 5*time.Millisecond,
		"a scoped dial-timeout DeadlineExceeded (sessionCtx itself never cancelled) must autonomously stop the session — zero stop_session calls required")

	m.Shutdown()
}

// TestManager_Start_ShutdownCancelsSetupCtx proves WR-02 (Truth 4 / SESS-05
// reopened gap): Shutdown's s.cancel() now reaches a mid-setup Start call's
// setupCtx via the Task 1 context.AfterFunc(sessionCtx, setupCancel) merge,
// so a resolveSetupFn that genuinely observes its ctx (unlike
// TestManager_Start_ShutdownRacesSetup's fake above, which blocks on a
// plain manually-closed release channel and therefore cannot prove ctx
// cancellation at all) aborts promptly instead of running to its own setup
// timeout.
//
// Load-bearing property: reqCtx is context.Background() (never cancelled)
// and Timeout is a deliberately large 30s, so setupCtx's own timeout is
// irrelevant to this test's bounded window — the ONLY way the fake can
// unblock within a few hundred milliseconds is if Shutdown's cancellation
// reached setupCtx. Pre-fix (setupCtx rooted in reqCtx alone, no merge),
// this test's bounded assertions below would time out (t.Fatal) well
// before the fake ever unblocks at the real 30s mark — proving the pre-fix
// separate-context-tree code cannot meet this bar.
func TestManager_Start_ShutdownCancelsSetupCtx(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})

	entered := make(chan struct{})
	m.resolveSetupFn = func(setupCtx context.Context, _ StartArgs) (string, func(), map[string]*ciliumv2.CiliumNetworkPolicy, error) {
		close(entered)
		<-setupCtx.Done() // load-bearing: inspects ctx instead of a manual release lever
		return "", nil, nil, setupCtx.Err()
	}

	before, err := filepath.Glob(filepath.Join(os.TempDir(), "cpg-session-*"))
	require.NoError(t, err)

	startDone := make(chan startOutcome, 1)
	go func() {
		res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1", Timeout: 30 * time.Second})
		startDone <- startOutcome{res: res, err: err}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Start never reached the setup seam")
	}

	bound := 4 * (m.stopWait + m.removeWait) // well under the 30s setup timeout

	shutdownDone := make(chan struct{})
	go func() {
		m.Shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
	case <-time.After(bound):
		t.Fatal("Shutdown did not return within the bounded deadline while a Start was mid-setup")
	}

	var outcome startOutcome
	select {
	case outcome = <-startDone:
	case <-time.After(bound):
		t.Fatal("Start did not return promptly after Shutdown — setupCtx was not cancelled by sessionCtx (WR-02 regression: pre-fix code would only unblock at the 30s setup timeout)")
	}
	require.Error(t, outcome.err, "a cancelled mid-setup resolveSetupFn must surface as a Start error")

	after, err := filepath.Glob(filepath.Join(os.TempDir(), "cpg-session-*"))
	require.NoError(t, err)
	assert.Len(t, after, len(before), "no orphaned session tmpdir should remain after Shutdown cancelled a mid-setup Start")
}
