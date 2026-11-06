package execution

import (
	"context"
	"errors"
	"fmt"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const testChainID = "execution-test"

// eventLog records the order in which the app hash builder and the app see each block.
type eventLog = utils.Mutex[*[]string]

func logEvent(events *eventLog, format string, args ...any) {
	for log := range events.Lock() {
		*log = append(*log, fmt.Sprintf(format, args...))
	}
}

// recordingApp is an application that records each call execution makes.
type recordingApp struct {
	abci.BaseApplication
	events *eventLog
	state  utils.Mutex[*recordingAppState]
}

type recordingAppState struct {
	last       int64
	pending    int64
	inited     bool
	lastHeader utils.Option[*tmproto.Header]
	finalized  []*abci.RequestFinalizeBlock
	// failCommitAt makes Commit fail for the block of this height.
	failCommitAt utils.Option[int64]
}

// testGasPerBlock is the gas FinalizeBlock reports for every block.
const testGasPerBlock = 7

var errCommitFailed = errors.New("commit failed")

func newRecordingApp(events *eventLog, last int64) *recordingApp {
	return &recordingApp{events: events, state: utils.NewMutex(&recordingAppState{last: last})}
}

func (a *recordingApp) Info() *abci.ResponseInfo {
	return &abci.ResponseInfo{LastBlockHeight: a.LastBlockHeight()}
}

func (a *recordingApp) LastBlockHeight() int64 {
	for state := range a.state.Lock() {
		return state.last
	}
	panic("unreachable")
}

func (a *recordingApp) InitChain(*abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	for state := range a.state.Lock() {
		state.inited = true
	}
	return &abci.ResponseInitChain{}, nil
}

func (a *recordingApp) InitLastHeader(header *tmproto.Header) {
	for state := range a.state.Lock() {
		state.lastHeader = utils.Some(header)
	}
}

func (a *recordingApp) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	logEvent(a.events, "finalize %d", req.Header.Height)
	for state := range a.state.Lock() {
		state.pending = req.Header.Height
		state.finalized = append(state.finalized, req)
	}
	return &abci.ResponseFinalizeBlock{TxResults: []*abci.ExecTxResult{{GasUsed: testGasPerBlock}}}, nil
}

func (a *recordingApp) Commit(context.Context) (*abci.ResponseCommit, error) {
	for state := range a.state.Lock() {
		if fail, ok := state.failCommitAt.Get(); ok && fail == state.pending {
			return nil, errCommitFailed
		}
		state.last = state.pending
	}
	return &abci.ResponseCommit{}, nil
}

func (a *recordingApp) finalized() []*abci.RequestFinalizeBlock {
	for state := range a.state.Lock() {
		return state.finalized
	}
	panic("unreachable")
}

// recordingBuilder is an app hash builder that records each block hash reported
// to it. Execution calls nothing else on it.
type recordingBuilder struct {
	builder.AppHashBuilder
	events *eventLog
}

func (b recordingBuilder) ReportBlockHash(_ context.Context, blockHeight uint64, _ [32]byte) error {
	logEvent(b.events, "report %d", blockHeight)
	return nil
}

// testEnv is an executor over a fresh BlockStore holding no blocks, and the road
// blocks a test writes to it.
type testEnv struct {
	events *eventLog
	store  *blockstore.Store
	blocks []*atypes.RoadBlock
	genDoc *types.GenesisDoc
}

func newTestEnv(t *testing.T, rng utils.Rng, count int) *testEnv {
	t.Helper()
	registry, keys := epoch.GenRegistry(rng, 1)
	ep := registry.MustEpoch(0)
	blocks := make([]*atypes.RoadBlock, 0, count)
	prev := utils.None[*atypes.CommitQC]()
	for range count {
		qc, laneBlocks := data.TestCommitQC(rng, ep, keys, prev)
		blocks = append(blocks, atypes.NewRoadBlock(qc, laneBlocks))
		prev = utils.Some(qc.QC())
	}
	store, err := blockstore.New(memblock.NewBlockDB())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return &testEnv{
		events: new(utils.NewMutex(&[]string{})),
		store:  store,
		blocks: blocks,
		genDoc: &types.GenesisDoc{
			ChainID:         testChainID,
			InitialHeight:   int64(ep.RoadRange().First),
			ConsensusParams: types.DefaultConsensusParams(),
		},
	}
}

// write stores blocks [from, to) and, when flush is set, makes them durable.
func (env *testEnv) write(from, to int, flush bool) error {
	for _, block := range env.blocks[from:to] {
		if err := env.store.WriteBlock(block); err != nil {
			return err
		}
	}
	if flush {
		return env.store.Flush()
	}
	return nil
}

func (env *testEnv) executor(app *recordingApp) *Executor {
	return New(&Config{
		GenDoc:         env.genDoc,
		App:            proxy.New(app),
		AppHashBuilder: recordingBuilder{events: env.events},
		BlockStore:     env.store,
	})
}

func (env *testEnv) road(i int) atypes.RoadIndex {
	return env.blocks[i].QC().Index()
}

// waitExecuted waits until executor has committed road n.
func waitExecuted(ctx context.Context, executor *Executor, n atypes.RoadIndex) error {
	_, err := executor.ExecutedBlocks().Wait(ctx, func(w atypes.ExecutedBlocks) bool { return w.Latest().Number >= n })
	return err
}

func TestInitAppStartsAFreshAppAtTheGenesisHeight(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 1)
	app := newRecordingApp(env.events, 0)

	next, err := env.executor(app).initApp(t.Context())
	require.NoError(t, err)
	require.Equal(t, env.road(0), next)
	for state := range app.state.Lock() {
		require.True(t, state.inited)
	}
}

// TestInitAppRestoresTheTipHeaderFromTheBlockStore verifies the restart path:
// the committed tip's header comes from the BlockStore, and execution resumes
// after it.
func TestInitAppRestoresTheTipHeaderFromTheBlockStore(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 3)
	require.NoError(t, env.write(0, 3, true))
	tip := env.road(1)
	app := newRecordingApp(env.events, int64(tip))

	next, err := env.executor(app).initApp(t.Context())
	require.NoError(t, err)
	require.Equal(t, tip+1, next)
	for state := range app.state.Lock() {
		require.False(t, state.inited)
		header := state.lastHeader.OrPanic("InitLastHeader was not called")
		require.Equal(t, int64(tip), header.Height)
		require.Equal(t, testChainID, header.ChainID)
		require.True(t, env.blocks[1].Header().Timestamp().Equal(header.Time))
	}
}

func TestInitAppFailsWhenTheTipIsPruned(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 1)
	require.NoError(t, env.write(0, 1, true))
	// A tip of 0 would read as a fresh app.
	require.True(t, env.road(0) > 1)
	app := newRecordingApp(env.events, int64(env.road(0))-1)

	_, err := env.executor(app).initApp(t.Context())
	require.ErrorIs(t, err, atypes.ErrPruned)
}

// TestRunExecutesOnlyDurableBlocks verifies that execution waits for a block's
// Flush: a written block that is not durable yet is not executed until it is.
func TestRunExecutesOnlyDurableBlocks(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 3)
	app := newRecordingApp(env.events, 0)
	executor := env.executor(app)

	var executedBeforeFlush int
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(executor.Run(ctx)) })
		if err := env.write(0, 2, true); err != nil {
			return err
		}
		if err := env.write(2, 3, false); err != nil {
			return err
		}
		if err := waitExecuted(ctx, executor, env.road(1)); err != nil {
			return err
		}
		// Block 3 is written but not durable, so execution cannot have reached it.
		executedBeforeFlush = len(app.finalized())
		if err := env.store.Flush(); err != nil {
			return err
		}
		return waitExecuted(ctx, executor, env.road(2))
	}))
	require.Equal(t, 2, executedBeforeFlush)
	require.Equal(t, 3, len(app.finalized()))
}

// TestRunReportsEachBlockHashBeforeExecutingIt verifies the order execution
// drives the app hash builder and the app in, and the request it builds.
func TestRunReportsEachBlockHashBeforeExecutingIt(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 2)
	require.NoError(t, env.write(0, 2, true))
	app := newRecordingApp(env.events, 0)
	executor := env.executor(app)

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(executor.Run(ctx)) })
		return waitExecuted(ctx, executor, env.road(1))
	}))

	for log := range env.events.Lock() {
		require.Equal(t, []string{
			fmt.Sprintf("report %d", env.road(0)), fmt.Sprintf("finalize %d", env.road(0)),
			fmt.Sprintf("report %d", env.road(1)), fmt.Sprintf("finalize %d", env.road(1)),
		}, *log)
	}
	for i, req := range app.finalized() {
		want := env.blocks[i].SuperBlock()
		require.Equal(t, int64(want.Height), req.Header.Height)
		require.Equal(t, testChainID, req.Header.ChainID)
		require.True(t, want.Timestamp.Equal(req.Header.Time))
		require.Equal(t, want.Hash[:], req.Hash)
		require.Equal(t, len(want.Txs), len(req.Txs))
	}
}

// TestNewPublishesTheAppTipAsExecuted verifies that the executed-block feed
// starts at the app's committed tip, before any block runs.
func TestNewPublishesTheAppTipAsExecuted(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 1)
	tip := env.road(0)
	executor := env.executor(newRecordingApp(env.events, int64(tip)))
	require.Equal(t, atypes.ExecutedBlock{Number: tip}, executor.ExecutedBlocks().Load().Latest())
}

// TestRunReplaysStoredBlocksAfterRestart verifies that a restarted app executes
// exactly the stored blocks above its committed tip.
func TestRunReplaysStoredBlocksAfterRestart(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 3)
	require.NoError(t, env.write(0, 3, true))
	app := newRecordingApp(env.events, int64(env.road(0)))
	executor := env.executor(app)

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(executor.Run(ctx)) })
		return waitExecuted(ctx, executor, env.road(2))
	}))
	finalized := app.finalized()
	require.Equal(t, 2, len(finalized))
	require.Equal(t, int64(env.road(1)), finalized[0].Header.Height)
	require.Equal(t, int64(env.road(2)), finalized[1].Header.Height)
}

func gasUsedTotal(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, metrics.GasUsed.Write(&m))
	return m.GetCounter().GetValue()
}

// TestRunCountsGasOnlyAfterCommit verifies that a block's gas is counted only
// once the app has committed it.
func TestRunCountsGasOnlyAfterCommit(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 2)
	require.NoError(t, env.write(0, 2, true))
	app := newRecordingApp(env.events, 0)
	for state := range app.state.Lock() {
		state.failCommitAt = utils.Some(int64(env.road(1)))
	}
	executor := env.executor(app)
	before := gasUsedTotal(t)

	require.ErrorIs(t, executor.Run(t.Context()), errCommitFailed)
	require.Equal(t, before+testGasPerBlock, gasUsedTotal(t), "only the committed block is counted")
	require.Equal(t, env.road(0), executor.ExecutedBlocks().Load().Latest().Number)
}

func TestFinalizeBlockGasUsed(t *testing.T) {
	resp := &abci.ResponseFinalizeBlock{
		TxResults: []*abci.ExecTxResult{
			{GasUsed: 10},
			nil,
			{GasUsed: -1},
			{GasUsed: 20},
		},
	}
	require.Equal(t, int64(30), finalizeBlockGasUsed(resp))
}

// preparingApp is a recordingApp that also prepares blocks, and records whether
// each finalized block was prepared first.
type preparingApp struct {
	*recordingApp
	prepareErr func(height int64) error
	prep       utils.Mutex[*preparingAppState]
}

type preparingAppState struct {
	prepared map[int64]bool
	misses   int
}

func newPreparingApp(events *eventLog) *preparingApp {
	return &preparingApp{
		recordingApp: newRecordingApp(events, 0),
		prep:         utils.NewMutex(&preparingAppState{prepared: map[int64]bool{}}),
	}
}

func (a *preparingApp) PrepareBlock(_ context.Context, req *abci.RequestFinalizeBlock) error {
	if a.prepareErr != nil {
		if err := a.prepareErr(req.Header.Height); err != nil {
			return err
		}
	}
	for state := range a.prep.Lock() {
		state.prepared[req.Header.Height] = true
	}
	return nil
}

func (a *preparingApp) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	for state := range a.prep.Lock() {
		if !state.prepared[req.Header.Height] {
			state.misses++
		}
	}
	return a.recordingApp.FinalizeBlock(ctx, req)
}

func (env *testEnv) preparingExecutor(app *preparingApp) *Executor {
	return New(&Config{
		GenDoc:         env.genDoc,
		App:            proxy.New(app),
		AppHashBuilder: recordingBuilder{events: env.events},
		BlockStore:     env.store,
	})
}

// Every block is handed to PrepareBlock before FinalizeBlock runs it.
func TestRunPreparesEveryBlockBeforeFinalizingIt(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 6)
	require.NoError(t, env.write(0, 6, true))
	app := newPreparingApp(env.events)
	executor := env.preparingExecutor(app)

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(executor.Run(ctx)) })
		return waitExecuted(ctx, executor, env.road(5))
	}))
	require.Equal(t, 6, len(app.finalized()))
	for state := range app.prep.Lock() {
		require.Equal(t, 0, state.misses)
	}
}

// A PrepareBlock error stops execution with that error.
func TestRunStopsOnAPrepareError(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 3)
	require.NoError(t, env.write(0, 3, true))
	errPrepare := errors.New("prepare failed")
	failAt := int64(env.road(1))
	app := newPreparingApp(env.events)
	app.prepareErr = func(height int64) error {
		if height == failAt {
			return errPrepare
		}
		return nil
	}
	require.ErrorIs(t, env.preparingExecutor(app).Run(t.Context()), errPrepare)
}

// The app's prepared queue holds every block execution prepares at once.
func TestPrepareAheadFitsTheAppsPreparedQueue(t *testing.T) {
	require.LessOrEqual(t, prepareAheadBlocks+2, evmonlyapp.MaxPreparedBlocks)
}
