package execution

import (
	"context"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
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
}

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
	return &abci.ResponseFinalizeBlock{}, nil
}

func (a *recordingApp) Commit(context.Context) (*abci.ResponseCommit, error) {
	for state := range a.state.Lock() {
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

// recordingBuilder is an app hash builder that records each block hash reported to it.
type recordingBuilder struct {
	events *eventLog
}

func (b recordingBuilder) ReportBlockHash(_ context.Context, blockHeight uint64, _ [32]byte) error {
	logEvent(b.events, "report %d", blockHeight)
	return nil
}

func (recordingBuilder) ReportStateHash(context.Context, uint64, [32]byte, map[string][]byte) error {
	return nil
}

func (recordingBuilder) ReportBUDHash(context.Context, uint64, [32]byte) error { return nil }

func (recordingBuilder) ReportReceiptHash(context.Context, uint64, apphash.ReceiptHash) error {
	return nil
}

func (recordingBuilder) GetAppHash(ctx context.Context, _ uint64) (*apphash.AppHashData, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (recordingBuilder) Bounds() (uint64, uint64, bool, error) { return 0, 0, false, nil }

func (recordingBuilder) SetupComplete(context.Context, uint64) error { return nil }

func (recordingBuilder) Close() error { return nil }

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
		Blocks:         env.store,
	})
}

func (env *testEnv) road(i int) atypes.RoadIndex {
	return env.blocks[i].QC().Index()
}

func TestInitStartsAFreshAppAtTheGenesisHeight(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 1)
	app := newRecordingApp(env.events, 0)

	next, restarted, err := env.executor(app).Init(t.Context())
	require.NoError(t, err)
	require.False(t, restarted)
	require.Equal(t, env.road(0), next)
	for state := range app.state.Lock() {
		require.True(t, state.inited)
	}
}

// TestInitRestoresTheTipHeaderFromTheBlockStore verifies the restart path: the
// committed tip's header comes from the BlockStore, and execution resumes after it.
func TestInitRestoresTheTipHeaderFromTheBlockStore(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 3)
	require.NoError(t, env.write(0, 3, true))
	tip := env.road(1)
	app := newRecordingApp(env.events, int64(tip))

	next, restarted, err := env.executor(app).Init(t.Context())
	require.NoError(t, err)
	require.True(t, restarted)
	require.Equal(t, tip+1, next)
	for state := range app.state.Lock() {
		require.False(t, state.inited)
		header := state.lastHeader.OrPanic("InitLastHeader was not called")
		require.Equal(t, int64(tip), header.Height)
		require.Equal(t, testChainID, header.ChainID)
		require.True(t, env.blocks[1].Header().Timestamp().Equal(header.Time))
	}
}

func TestInitFailsWhenTheTipIsPruned(t *testing.T) {
	rng := utils.TestRng()
	env := newTestEnv(t, rng, 1)
	require.NoError(t, env.write(0, 1, true))
	// A tip of 0 would read as a fresh app.
	require.True(t, env.road(0) > 1)
	app := newRecordingApp(env.events, int64(env.road(0))-1)

	_, _, err := env.executor(app).Init(t.Context())
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
		next, _, err := executor.Init(ctx)
		if err != nil {
			return err
		}
		committed := make(chan Committed, len(env.blocks))
		s.SpawnBg(func() error { return utils.IgnoreCancel(executor.Run(ctx, next, committed)) })
		if err := env.write(0, 2, true); err != nil {
			return err
		}
		if err := env.write(2, 3, false); err != nil {
			return err
		}
		for range 2 {
			if _, err := utils.Recv(ctx, committed); err != nil {
				return err
			}
		}
		// Block 3 is written but not durable, so execution cannot have reached it.
		executedBeforeFlush = len(app.finalized())
		if err := env.store.Flush(); err != nil {
			return err
		}
		_, err = utils.Recv(ctx, committed)
		return err
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

	var got []Committed
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		next, _, err := executor.Init(ctx)
		if err != nil {
			return err
		}
		committed := make(chan Committed, len(env.blocks))
		s.SpawnBg(func() error { return utils.IgnoreCancel(executor.Run(ctx, next, committed)) })
		for range env.blocks {
			c, err := utils.Recv(ctx, committed)
			if err != nil {
				return err
			}
			got = append(got, c)
		}
		return nil
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
		require.Equal(t, want.Height, got[i].Number)
	}
	require.Equal(t, env.road(1), executor.ExecutedBlocks().Load().Latest().Number)
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
		next, _, err := executor.Init(ctx)
		if err != nil {
			return err
		}
		committed := make(chan Committed, len(env.blocks))
		s.SpawnBg(func() error { return utils.IgnoreCancel(executor.Run(ctx, next, committed)) })
		for range 2 {
			if _, err := utils.Recv(ctx, committed); err != nil {
				return err
			}
		}
		return nil
	}))
	finalized := app.finalized()
	require.Equal(t, 2, len(finalized))
	require.Equal(t, int64(env.road(1)), finalized[0].Header.Height)
	require.Equal(t, int64(env.road(2)), finalized[1].Header.Height)
}

func TestCommitteeWeights(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	wantPK := utils.OrPanic1(atypes.PublicKeyFromBytes(sk.Public().Bytes()))
	got, err := CommitteeWeights([]abci.ValidatorUpdate{{
		PubKey: crypto.PubKeyToProto(sk.Public()),
		Power:  42,
	}})
	require.NoError(t, err)
	require.Equal(t, map[atypes.PublicKey]uint64{wantPK: 42}, got)
}

func TestCommitteeWeights_SkipsZeroPower(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	got, err := CommitteeWeights([]abci.ValidatorUpdate{{
		PubKey: crypto.PubKeyToProto(sk.Public()),
		Power:  0,
	}})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestCommitteeWeights_DuplicateKey(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	pk := crypto.PubKeyToProto(sk.Public())
	_, err := CommitteeWeights([]abci.ValidatorUpdate{
		{PubKey: pk, Power: 1},
		{PubKey: pk, Power: 2},
	})
	require.Error(t, err)
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
