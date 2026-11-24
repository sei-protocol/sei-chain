package execution

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	dto "github.com/prometheus/client_model/go"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/execution/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
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
	abevm.Application
	events *eventLog
	state  utils.Watch[*recordingAppState]
}

type recordingAppState struct {
	last       int64
	inited     bool
	lastHeader utils.Option[time.Time]
	finalized  []abevm.FinalizeBlockReq
	// entered counts the FinalizeBlock calls that have started.
	entered int
	// failFinalizeAt makes FinalizeBlock fail for the block of this height.
	failFinalizeAt utils.Option[int64]
	// holdFinalizeAt makes FinalizeBlock for this height wait until another FinalizeBlock has started.
	holdFinalizeAt utils.Option[int64]
	// holdReturnAt makes FinalizeBlock for this height return only once the next block has finalized.
	holdReturnAt utils.Option[int64]
}

// testGasPerBlock is the gas FinalizeBlock reports for every block.
const testGasPerBlock = 7

var errFinalizeFailed = errors.New("finalization failed")

func newRecordingApp(events *eventLog, last int64) *recordingApp {
	return &recordingApp{events: events, state: utils.NewWatch(&recordingAppState{last: last})}
}

func (a *recordingApp) EvmChainID() uint64 { return 1 }

func (a *recordingApp) Info() *abevm.ResponseInfo {
	return &abevm.ResponseInfo{LastBlockHeight: a.LastBlockHeight()}
}

func (a *recordingApp) LastBlockHeight() int64 {
	for state := range a.state.Lock() {
		return state.last
	}
	panic("unreachable")
}

func (a *recordingApp) InitChain(req abevm.InitConfig) error {
	for state := range a.state.Lock() {
		state.inited = true
		state.last = utils.Clamp[int64](req.InitialHeight) - 1
	}
	return nil
}

func (a *recordingApp) RestoreBlockTime(blockTime time.Time) {
	for state := range a.state.Lock() {
		state.lastHeader = utils.Some(blockTime)
	}
}

// FinalizeBlock waits until the previous block has finalized, as execution may call it ahead.
func (a *recordingApp) FinalizeBlock(ctx context.Context, req abevm.FinalizeBlockReq) (abevm.FinalizeBlockResp, error) {
	height := utils.Clamp[int64](req.Height)
	for state, ctrl := range a.state.Lock() {
		state.entered++
		ctrl.Updated()
		if hold, ok := state.holdFinalizeAt.Get(); ok && hold == height {
			if err := ctrl.WaitUntil(ctx, func() bool { return state.entered >= 2 }); err != nil {
				return abevm.FinalizeBlockResp{}, err
			}
		}
		if err := ctrl.WaitUntil(ctx, func() bool { return height <= state.last+1 }); err != nil {
			return abevm.FinalizeBlockResp{}, err
		}
		logEvent(a.events, "finalize %d", height)
		state.finalized = append(state.finalized, req)
		if fail, ok := state.failFinalizeAt.Get(); ok && fail == height {
			return abevm.FinalizeBlockResp{}, errFinalizeFailed
		}
		state.last = height
		ctrl.Updated()
		if hold, ok := state.holdReturnAt.Get(); ok && hold == height {
			if err := ctrl.WaitUntil(ctx, func() bool { return state.last > height }); err != nil {
				return abevm.FinalizeBlockResp{}, err
			}
		}
	}
	return abevm.FinalizeBlockResp{GasUsed: testGasPerBlock}, nil
}

func (a *recordingApp) finalized() []abevm.FinalizeBlockReq {
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
	keys   []atypes.SecretKey
	genDoc *types.GenesisDoc
}

func newTestEnv(t *testing.T, rng utils.Rng, count int) *testEnv {
	t.Helper()
	registry, keys := epoch.GenRegistry(rng, 1)
	ep := registry.MustEpoch(0)
	blocks := make([]*atypes.RoadBlock, 0, count)
	prev := utils.None[*atypes.CommitQC]()
	for range count {
		qc, laneBlocks := data.TestCommitQCWithPayload(rng, ep, keys, prev, func() *atypes.Payload {
			return atypes.GenEthPayload(rng)
		})
		blocks = append(blocks, atypes.NewRoadBlock(qc, laneBlocks))
		prev = utils.Some(qc.QC())
	}
	params := types.DefaultConsensusParams()
	params.Block.MaxGas = 30_000_000
	store := blockstore.NewTestStore(t)
	events := new(utils.NewMutex(&[]string{}))
	return &testEnv{
		events: events,
		store:  store,
		blocks: blocks,
		keys:   keys,
		genDoc: &types.GenesisDoc{
			ChainID:         testChainID,
			InitialHeight:   int64(ep.RoadRange().First),
			ConsensusParams: params,
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

func (env *testEnv) executor(app abevm.Application) *Executor {
	return New(&Config{
		GenDoc:            env.genDoc,
		App:               abevm.NewProxy(app),
		AppHashBuilder:    recordingBuilder{events: env.events},
		BlockStore:        env.store,
		Signer:            newTestSigner(app.EvmChainID(), 0),
		MaxBlocksInFlight: gigaconfig.DefaultConfig.Execution.MaxBlocksInFlight,
	})
}

func (env *testEnv) road(i int) atypes.RoadHeight {
	return env.blocks[i].QC().Height()
}

// waitExecuted waits until executor has committed road n.
func waitExecuted(ctx context.Context, executor *Executor, n atypes.RoadHeight) error {
	_, err := executor.ExecutedBlocks().Wait(ctx, func(w atypes.ExecutedBlocks) bool { return w.Latest().Height >= n })
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
		require.True(t, env.blocks[1].Header().Timestamp().Equal(header))
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
		at := map[string]int{}
		for i, event := range *log {
			at[event] = i
		}
		report := func(i int) string { return fmt.Sprintf("report %d", env.road(i)) }
		finalize := func(i int) string { return fmt.Sprintf("finalize %d", env.road(i)) }
		require.Equal(t, 4, len(*log))
		require.True(t, at[report(0)] < at[report(1)])
		require.True(t, at[report(0)] < at[finalize(0)])
		require.True(t, at[report(1)] < at[finalize(1)])
		require.True(t, at[finalize(0)] < at[finalize(1)])
	}
	for i, req := range app.finalized() {
		want := env.blocks[i].SuperBlock()
		require.Equal(t, want.Height, req.Height)
		require.True(t, want.Timestamp.Equal(req.Time))
		require.Equal(t, common.Hash(want.Hash), req.Hash)
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
	require.Equal(t, atypes.ExecutedBlock{Height: tip}, executor.ExecutedBlocks().Load().Latest())
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
	require.Equal(t, int64(env.road(1)), int64(finalized[0].Height))
	require.Equal(t, int64(env.road(2)), int64(finalized[1].Height))
}

// cancelingApp is a recordingApp that cancels the run's context from inside FinalizeBlock().
type cancelingApp struct {
	*recordingApp
	cancel context.CancelFunc
}

func (a *cancelingApp) FinalizeBlock(
	ctx context.Context,
	req abevm.FinalizeBlockReq,
) (abevm.FinalizeBlockResp, error) {
	a.cancel()
	return a.recordingApp.FinalizeBlock(ctx, req)
}

// TestRunCompletesTheBlockInFlightWhenCanceled verifies that a cancel arriving
// while a block executes lets that block finalize.
func TestRunCompletesTheBlockInFlightWhenCanceled(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 2)
	require.NoError(t, env.write(0, 2, true))
	runCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	app := &cancelingApp{recordingApp: newRecordingApp(env.events, 0), cancel: cancel}
	executor := env.executor(app)

	require.ErrorIs(t, executor.Run(runCtx), context.Canceled)
	require.Equal(t, env.road(0), app.finalized()[0].Height)
}

func gasUsedTotal(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, metrics.GasUsed.Write(&m))
	return m.GetCounter().GetValue()
}

// TestRunCountsGasOnlyAfterFinalization verifies that a block's gas is counted only
// once the app has finalized it.
func TestRunCountsGasOnlyAfterFinalization(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 2)
	require.NoError(t, env.write(0, 2, true))
	app := newRecordingApp(env.events, 0)
	for state := range app.state.Lock() {
		state.failFinalizeAt = utils.Some(int64(env.road(1)))
	}
	executor := env.executor(app)
	before := gasUsedTotal(t)

	require.ErrorIs(t, executor.Run(t.Context()), errFinalizeFailed)
	require.Equal(t, before+testGasPerBlock, gasUsedTotal(t), "only the finalized block is counted")
	require.Equal(t, env.road(0), executor.ExecutedBlocks().Load().Latest().Height)
}

func newTestSigner(chainID uint64, workers int) *abevm.Signer {
	return abevm.NewSigner(ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), 1024, workers)
}

func TestExecutionReusesAndEvictsInjectedSignerCache(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 1)
	app := newRecordingApp(env.events, 0)
	executor := env.executor(app)
	_, err := executor.initApp(t.Context())
	require.NoError(t, err)
	block := env.blocks[0].SuperBlock()
	require.NotEmpty(t, block.Txs)
	raw := block.Txs[0]
	verified, err := executor.cfg.Signer.ParseTx(raw)
	require.NoError(t, err)
	claimed := common.Address{1}
	executor.cfg.Signer.Remember(verified.Hash(), claimed)
	_, err = executor.executeBlock(t.Context(), block)
	require.NoError(t, err)
	require.Equal(t, claimed, app.finalized()[0].Txs[0].Sender())
	reparsed, err := executor.cfg.Signer.ParseTx(raw)
	require.NoError(t, err)
	require.Equal(t, verified.Sender(), reparsed.Sender())
}

// The next block's FinalizeBlock is entered while the current one is still inside it.
func TestRunEntersTheNextBlockBeforeTheCurrentOneReturns(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 4)
	require.NoError(t, env.write(0, 4, true))
	app := newRecordingApp(env.events, 0)
	// The first block finalizes only once the next one has entered FinalizeBlock.
	for state := range app.state.Lock() {
		state.holdFinalizeAt = utils.Some(int64(env.road(0)))
	}
	executor := env.executor(app)

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(executor.Run(ctx)) })
		return waitExecuted(ctx, executor, env.road(3))
	}))
	require.Equal(t, 4, len(app.finalized()))
}

// A block whose FinalizeBlock returns before the previous block's is still
// published after it, so the executed-block window stays in road order.
func TestRunPublishesExecutedBlocksInRoadOrder(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 4)
	require.NoError(t, env.write(0, 4, true))
	// Starting from a committed tip makes the window contiguous from its first block.
	app := newRecordingApp(env.events, int64(env.road(0)))
	for state := range app.state.Lock() {
		state.holdReturnAt = utils.Some(int64(env.road(1)))
	}
	executor := env.executor(app)

	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(executor.Run(ctx)) })
		return waitExecuted(ctx, executor, env.road(3))
	}))
	executed := executor.ExecutedBlocks().Load()
	for i := range 4 {
		block, ok := executed.Get(env.road(i))
		require.True(t, ok)
		require.Equal(t, env.road(i), block.Height)
	}
	require.Equal(t, env.road(3), executed.Latest().Height)
}
