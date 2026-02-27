package consensus

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fortytw2/leaktest"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/assert"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	sf "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// These tests ensure we can always recover from failure at any part of the consensus process.
// There are two general failure scenarios: failure during consensus, and failure while applying the block.
// Only the latter interacts with the app and store,
// but the former has to deal with restrictions on re-use of priv_validator keys.
// The `WAL Tests` are for failures during the consensus;
// the `Handshake Tests` are for failures in applying the block.
// With the help of the WAL, we can recover from it all!

//------------------------------------------------------------------------------------------
// WAL Tests

func randPort() int {
	// returns between base and base + spread
	base, spread := 20000, 20000
	// nolint:gosec // G404: Use of weak random number generator
	return base + rand.Intn(spread)
}

// makeAddrs constructs local TCP addresses for node services.
// It uses consecutive ports from a random starting point, so that concurrent
// instances are less likely to collide.
func makeAddrs() (p2pAddr, rpcAddr string) {
	const addrTemplate = "tcp://127.0.0.1:%d"
	start := randPort()
	return fmt.Sprintf(addrTemplate, start), fmt.Sprintf(addrTemplate, start+1)
}

// getConfig returns a config for test cases
func getConfig(t *testing.T) *config.Config {
	c, err := config.ResetTestRoot(t.TempDir(), t.Name())
	require.NoError(t, err)

	p2pAddr, rpcAddr := makeAddrs()
	c.P2P.ListenAddress = p2pAddr
	c.RPC.ListenAddress = rpcAddr
	return c
}

func waitForBlock(ctx context.Context, cs *State, lastBlock int64) error {
	newBlockSub, err := cs.eventBus.SubscribeWithArgs(ctx, pubsub.SubscribeArgs{
		ClientID: testSubscriber,
		Query:    types.EventQueryNewBlock,
	})
	if err != nil {
		return fmt.Errorf("cs.eventBus.SubscribeWithArgs(): %w", err)
	}
	for {
		msg, err := newBlockSub.Next(ctx)
		if err != nil {
			return fmt.Errorf("newBlockSub.Next(): %w", err)
		}
		if msg.Data().(types.EventDataNewBlock).Block.Header.Height >= lastBlock {
			return nil
		}
	}
}

// runStateUntilBlock runs consensus state until block lastBlock is produced.
func runStateUntilBlock(t *testing.T, cfg *config.Config, lastBlock int64) {
	ctx := t.Context()
	state, err := sm.MakeGenesisStateFromFile(cfg.GenesisFile())
	require.NoError(t, err)
	state.Version.Consensus.App = kvstore.ProtocolVersion // simulate handshake, receive app version
	cs := newStateWithConfigAndBlockStore(
		ctx,
		log.NewNopLogger(),
		cfg,
		state,
		loadPrivValidator(cfg),
		kvstore.NewApplication(),
		store.NewBlockStore(dbm.NewMemDB()),
	)
	defer cs.wal.Close()

	// substitute the WAL so that replaying messages doesn't write to the real WAL.
	// TODO(gprusak): this is a hack, fix it.
	realWAL := cs.wal
	cs.wal, err = openWAL(filepath.Join(t.TempDir(), "other dir"))
	require.NoError(t, err)
	defer cs.wal.Close()
	// Replay manually all messages from WAL except for the msgs of latest height.
	// We need this because the state constructed above is for genesis.
	_, lastHeightMsgs, err := realWAL.ReadLastHeightMsgs()
	require.NoError(t, err)
	allMsgs := dumpWAL(t, realWAL)
	msgs := allMsgs[0 : len(allMsgs)-len(lastHeightMsgs)]
	if len(msgs) > 0 {
		require.NoError(t, cs.updateStateFromStore())
		for _, msg := range msgs {
			cs.readReplayMessage(ctx, msg)
		}
	}
	// Return the real WAL in place.
	cs.wal.Close()
	cs.wal = realWAL

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(cs.Run(ctx)) })
		return waitForBlock(ctx, cs, lastBlock)
	}); err != nil {
		panic(err)
	}
}

func sendTxs(ctx context.Context, cs *State) error {
	for i := range 256 {
		if ctx.Err() != nil {
			return nil
		}
		tx := []byte{byte(i)}
		if err := cs.txNotifier.(mempool.Mempool).CheckTx(ctx, tx, nil, mempool.TxInfo{}); err != nil {
			return fmt.Errorf("cs.mempool.CheckTx(): %w", err)
		}
	}
	return nil
}

// TestWALCrash uses crashing WAL to test we can recover from any WAL failure.
func TestWALCrash(t *testing.T) {
	testCases := []struct {
		name         string
		sendTxsFn    func(context.Context, dbm.DB, *State) error
		heightToStop int64
	}{
		{"empty block",
			func(ctx context.Context, stateDB dbm.DB, cs *State) error { return nil },
			1},
		{"many non-empty blocks",
			func(ctx context.Context, stateDB dbm.DB, cs *State) error { return sendTxs(ctx, cs) },
			3},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			consensusReplayConfig, err := ResetConfig(t.TempDir(), tc.name)
			require.NoError(t, err)
			crashWALandCheckLiveness(t, consensusReplayConfig, tc.sendTxsFn, tc.heightToStop)
		})
	}
}

func dumpWAL(t *testing.T, wal *WAL) []WALMessage {
	t.Helper()
	var msgs []WALMessage
	for inner := range wal.inner.Lock() {
		for offset := inner.MinOffset(); offset <= 0; offset++ {
			entries, err := inner.ReadFile(offset)
			require.NoError(t, err)
			for _, entry := range entries {
				msg, err := walFromBytes(entry)
				require.NoError(t, err)
				msgs = append(msgs, msg)
			}
		}
	}
	return msgs
}

func resetWAL(t *testing.T, walPath string, msgs []WALMessage) {
	t.Helper()
	dirPath := filepath.Dir(walPath)
	entries, err := os.ReadDir(dirPath)
	require.NoError(t, err)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), filepath.Base(walPath)) {
			require.NoError(t, os.Remove(filepath.Join(dirPath, e.Name())))
		}
	}
	wal, err := openWAL(walPath)
	require.NoError(t, err)
	defer wal.Close()
	for _, msg := range msgs {
		require.NoError(t, wal.Append(msg))
	}
	require.NoError(t, wal.Sync())
}

func crashWALandCheckLiveness(
	t *testing.T,
	cfg *config.Config,
	sendTxsFn func(context.Context, dbm.DB, *State) error,
	heightToStop int64,
) {
	t.Helper()
	ctx := t.Context()
	t.Logf("Generate WAL with sendTxsFn running in parallel.")
	state, err := sm.MakeGenesisStateFromFile(cfg.GenesisFile())
	require.NoError(t, err)
	state.Version.Consensus.App = kvstore.ProtocolVersion // simulate handshake, receive app version
	cs := newStateWithConfigAndBlockStore(
		ctx,
		log.NewNopLogger(),
		cfg,
		state,
		loadPrivValidator(cfg),
		kvstore.NewApplication(),
		store.NewBlockStore(dbm.NewMemDB()),
	)
	defer cs.wal.Close()
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return sendTxsFn(ctx, dbm.NewMemDB(), cs) })
		s.SpawnBg(func() error { return utils.IgnoreCancel(cs.Run(ctx)) })
		return waitForBlock(ctx, cs, heightToStop)
	}))
	cs.wal.Close()
	wal, err := openWAL(cfg.Consensus.WalFile())
	require.NoError(t, err)
	defer wal.Close()
	msgs := dumpWAL(t, wal)
	wal.Close()
	t.Logf("Iterate over prefixes of generated WAL and start from there.")
	for i := range msgs {
		// WARNING: when bootstaping, WAL is initialized with EndHeight{0} marker,
		// so we skip it to avoid inserting it twice.
		resetWAL(t, cfg.Consensus.WalFile(), msgs[1:i+1])
		runStateUntilBlock(t, cfg, heightToStop+1)
	}
}

// ------------------------------------------------------------------------------------------
type simulatorTestSuite struct {
	GenesisState sm.State
	Config       *config.Config
	Chain        []*types.Block
	Commits      []*types.Commit
	CleanupFunc  cleanupFunc

	Mempool mempool.Mempool
	Evpool  sm.EvidencePool
}

const (
	numBlocks = 6
)

//---------------------------------------
// Test handshake/replay

// 0 - all synced up
// 1 - saved block but app and state are behind
// 2 - save block and committed but state is behind
// 3 - save block and committed with truncated block store and state behind
var modes = []uint{0, 1, 2, 3}

// This is actually not a test, it's for storing validator change tx data for testHandshakeReplay
func setupSimulator(ctx context.Context, t *testing.T) *simulatorTestSuite {
	t.Helper()
	cfg := configSetup(t)

	sim := &simulatorTestSuite{
		Mempool: emptyMempool{},
		Evpool:  sm.EmptyEvidencePool{},
	}

	nPeers := 7
	nVals := 4

	css, genDoc, cfg, cleanup := randConsensusNetWithPeers(
		ctx,
		t,
		cfg,
		nVals,
		nPeers,
		newMockTickerFunc(true),
		newEpehemeralKVStore)
	sim.Config = cfg
	defer func() { t.Cleanup(cleanup) }()

	var err error
	sim.GenesisState, err = sm.MakeGenesisState(genDoc)
	require.NoError(t, err)

	partSize := types.BlockPartSizeBytes

	newRoundCh := subscribe(ctx, t, css[0].eventBus, types.EventQueryNewRound)
	proposalCh := subscribe(ctx, t, css[0].eventBus, types.EventQueryCompleteProposal)

	vss := make([]*validatorStub, nPeers)
	for i := range nPeers {
		pv, _ := css[i].privValidator.Get()
		vss[i] = newValidatorStub(pv, int32(i))
	}
	height, round := css[0].roundState.Height(), css[0].roundState.Round()

	// start the machine
	startTestRound(ctx, css[0], height, round)
	incrementHeight(vss...)
	ensureNewRound(t, newRoundCh, height, 0)
	ensureNewProposal(t, proposalCh, height, round)
	rs := css[0].GetRoundState()

	signAddVotes(ctx, t, css[0], tmproto.PrecommitType, sim.Config.ChainID(),
		types.BlockID{Hash: rs.ProposalBlock.Hash(), PartSetHeader: rs.ProposalBlockParts.Header()},
		vss[1:nVals]...)

	ensureNewRound(t, newRoundCh, height+1, 0)

	// HEIGHT 2
	height++
	incrementHeight(vss...)
	pv, _ := css[nVals].privValidator.Get()
	newValidatorPubKey1, err := pv.GetPubKey(ctx)
	require.NoError(t, err)
	valPubKey1ABCI := crypto.PubKeyToProto(newValidatorPubKey1)
	newValidatorTx1 := kvstore.MakeValSetChangeTx(valPubKey1ABCI, testMinPower)
	err = assertMempool(t, css[0].txNotifier).CheckTx(ctx, newValidatorTx1, nil, mempool.TxInfo{})
	assert.NoError(t, err)
	propBlock, err := css[0].createProposalBlock(ctx) // changeProposer(t, cs1, vs2)
	require.NoError(t, err)
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: propBlock.Hash(), PartSetHeader: propBlockParts.Header()}

	pubKey, err := vss[1].PrivValidator.GetPubKey(ctx)
	require.NoError(t, err)
	proposal := types.NewProposal(vss[1].Height, round, -1, blockID, propBlock.Header.Time, propBlock.GetTxKeys(), propBlock.Header, propBlock.LastCommit, propBlock.Evidence, pubKey.Address())
	p := proposal.ToProto()
	if err := vss[1].SignProposal(ctx, cfg.ChainID(), p); err != nil {
		t.Fatal("failed to sign bad proposal", err)
	}
	proposal.Signature = utils.OrPanic1(crypto.SigFromBytes(p.Signature))

	// set the proposal block
	if err := css[0].SetProposalAndBlock(ctx, proposal, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}
	ensureNewProposal(t, proposalCh, height, round)
	rs = css[0].GetRoundState()
	signAddVotes(ctx, t, css[0], tmproto.PrecommitType, sim.Config.ChainID(),
		types.BlockID{Hash: rs.ProposalBlock.Hash(), PartSetHeader: rs.ProposalBlockParts.Header()},
		vss[1:nVals]...)
	ensureNewRound(t, newRoundCh, height+1, 0)

	// HEIGHT 3
	height++
	incrementHeight(vss...)
	pv, _ = css[nVals].privValidator.Get()
	updateValidatorPubKey1, err := pv.GetPubKey(ctx)
	require.NoError(t, err)
	updatePubKey1ABCI := crypto.PubKeyToProto(updateValidatorPubKey1)
	updateValidatorTx1 := kvstore.MakeValSetChangeTx(updatePubKey1ABCI, 25)
	err = assertMempool(t, css[0].txNotifier).CheckTx(ctx, updateValidatorTx1, nil, mempool.TxInfo{})
	assert.NoError(t, err)
	propBlock, err = css[0].createProposalBlock(ctx) // changeProposer(t, cs1, vs2)
	require.NoError(t, err)
	propBlockParts, err = propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	blockID = types.BlockID{Hash: propBlock.Hash(), PartSetHeader: propBlockParts.Header()}
	pubKey, err = vss[2].PrivValidator.GetPubKey(ctx)
	require.NoError(t, err)
	proposal = types.NewProposal(vss[1].Height, round, -1, blockID, propBlock.Header.Time, propBlock.GetTxKeys(), propBlock.Header, propBlock.LastCommit, propBlock.Evidence, pubKey.Address())
	p = proposal.ToProto()
	if err := vss[2].SignProposal(ctx, cfg.ChainID(), p); err != nil {
		t.Fatal("failed to sign bad proposal", err)
	}
	proposal.Signature = utils.OrPanic1(crypto.SigFromBytes(p.Signature))

	// set the proposal block
	if err := css[0].SetProposalAndBlock(ctx, proposal, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}
	ensureNewProposal(t, proposalCh, height, round)
	rs = css[0].GetRoundState()
	signAddVotes(ctx, t, css[0], tmproto.PrecommitType, sim.Config.ChainID(),
		types.BlockID{Hash: rs.ProposalBlock.Hash(), PartSetHeader: rs.ProposalBlockParts.Header()},
		vss[1:nVals]...)
	ensureNewRound(t, newRoundCh, height+1, 0)

	// HEIGHT 4
	height++
	incrementHeight(vss...)
	pv, _ = css[nVals+1].privValidator.Get()
	newValidatorPubKey2, err := pv.GetPubKey(ctx)
	require.NoError(t, err)
	newVal2ABCI := crypto.PubKeyToProto(newValidatorPubKey2)
	newValidatorTx2 := kvstore.MakeValSetChangeTx(newVal2ABCI, testMinPower)
	err = assertMempool(t, css[0].txNotifier).CheckTx(ctx, newValidatorTx2, nil, mempool.TxInfo{})
	assert.NoError(t, err)
	pv, _ = css[nVals+2].privValidator.Get()
	newValidatorPubKey3, err := pv.GetPubKey(ctx)
	require.NoError(t, err)
	newVal3ABCI := crypto.PubKeyToProto(newValidatorPubKey3)
	newValidatorTx3 := kvstore.MakeValSetChangeTx(newVal3ABCI, testMinPower)
	err = assertMempool(t, css[0].txNotifier).CheckTx(ctx, newValidatorTx3, nil, mempool.TxInfo{})
	assert.NoError(t, err)
	propBlock, err = css[0].createProposalBlock(ctx) // changeProposer(t, cs1, vs2)
	require.NoError(t, err)
	propBlockParts, err = propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	blockID = types.BlockID{Hash: propBlock.Hash(), PartSetHeader: propBlockParts.Header()}
	newVss := make([]*validatorStub, nVals+1)
	copy(newVss, vss[:nVals+1])
	newVss = sortVValidatorStubsByPower(ctx, t, newVss)

	valIndexFn := func(cssIdx int) int {
		for i, vs := range newVss {
			vsPubKey, err := vs.GetPubKey(ctx)
			require.NoError(t, err)

			pv, _ := css[cssIdx].privValidator.Get()
			cssPubKey, err := pv.GetPubKey(ctx)
			require.NoError(t, err)

			if vsPubKey == cssPubKey {
				return i
			}
		}
		t.Fatalf("validator css[%d] not found in newVss", cssIdx)
		return -1
	}

	selfIndex := valIndexFn(0)
	require.NotEqual(t, -1, selfIndex)
	pubKey, err = vss[3].PrivValidator.GetPubKey(ctx)
	require.NoError(t, err)
	proposal = types.NewProposal(vss[3].Height, round, -1, blockID, propBlock.Header.Time, propBlock.GetTxKeys(), propBlock.Header, propBlock.LastCommit, propBlock.Evidence, pubKey.Address())
	p = proposal.ToProto()
	if err := vss[3].SignProposal(ctx, cfg.ChainID(), p); err != nil {
		t.Fatal("failed to sign bad proposal", err)
	}
	proposal.Signature = utils.OrPanic1(crypto.SigFromBytes(p.Signature))

	// set the proposal block
	if err := css[0].SetProposalAndBlock(ctx, proposal, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}
	ensureNewProposal(t, proposalCh, height, round)

	removeValidatorTx2 := kvstore.MakeValSetChangeTx(newVal2ABCI, 0)
	err = assertMempool(t, css[0].txNotifier).CheckTx(ctx, removeValidatorTx2, nil, mempool.TxInfo{})
	assert.NoError(t, err)

	rs = css[0].GetRoundState()
	for i := 0; i < nVals+1; i++ {
		if i == selfIndex {
			continue
		}
		signAddVotes(ctx, t, css[0],
			tmproto.PrecommitType, sim.Config.ChainID(),
			types.BlockID{Hash: rs.ProposalBlock.Hash(), PartSetHeader: rs.ProposalBlockParts.Header()},
			newVss[i])
	}
	ensureNewRound(t, newRoundCh, height+1, 0)

	// HEIGHT 5
	height++
	incrementHeight(vss...)
	// Reflect the changes to vss[nVals] at height 3 and resort newVss.
	newVssIdx := valIndexFn(nVals)
	require.NotEqual(t, -1, newVssIdx)

	newVss[newVssIdx].VotingPower = 25
	newVss = sortVValidatorStubsByPower(ctx, t, newVss)

	selfIndex = valIndexFn(0)
	require.NotEqual(t, -1, selfIndex)
	ensureNewProposal(t, proposalCh, height, round)
	rs = css[0].GetRoundState()
	for i := 0; i < nVals+1; i++ {
		if i == selfIndex {
			continue
		}
		signAddVotes(ctx, t, css[0],
			tmproto.PrecommitType, sim.Config.ChainID(),
			types.BlockID{Hash: rs.ProposalBlock.Hash(), PartSetHeader: rs.ProposalBlockParts.Header()},
			newVss[i])
	}
	ensureNewRound(t, newRoundCh, height+1, 0)

	// HEIGHT 6
	height++
	incrementHeight(vss...)
	removeValidatorTx3 := kvstore.MakeValSetChangeTx(newVal3ABCI, 0)
	err = assertMempool(t, css[0].txNotifier).CheckTx(ctx, removeValidatorTx3, nil, mempool.TxInfo{})
	assert.NoError(t, err)
	propBlock, err = css[0].createProposalBlock(ctx) // changeProposer(t, cs1, vs2)
	require.NoError(t, err)
	propBlockParts, err = propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	blockID = types.BlockID{Hash: propBlock.Hash(), PartSetHeader: propBlockParts.Header()}
	newVss = make([]*validatorStub, nVals+3)
	copy(newVss, vss[:nVals+3])
	newVss = sortVValidatorStubsByPower(ctx, t, newVss)

	selfIndex = valIndexFn(0)
	require.NotEqual(t, -1, selfIndex)
	pubKey, err = vss[1].PrivValidator.GetPubKey(ctx)
	require.NoError(t, err)
	proposal = types.NewProposal(vss[1].Height, round, -1, blockID, propBlock.Header.Time, propBlock.GetTxKeys(), propBlock.Header, propBlock.LastCommit, propBlock.Evidence, pubKey.Address())
	p = proposal.ToProto()
	if err := vss[1].SignProposal(ctx, cfg.ChainID(), p); err != nil {
		t.Fatal("failed to sign bad proposal", err)
	}
	proposal.Signature = utils.OrPanic1(crypto.SigFromBytes(p.Signature))

	// set the proposal block
	if err := css[0].SetProposalAndBlock(ctx, proposal, propBlock, propBlockParts, "some peer"); err != nil {
		t.Fatal(err)
	}
	ensureNewProposal(t, proposalCh, height, round)
	rs = css[0].GetRoundState()
	for i := 0; i < nVals+3; i++ {
		if i == selfIndex {
			continue
		}
		signAddVotes(ctx, t, css[0],
			tmproto.PrecommitType, sim.Config.ChainID(),
			types.BlockID{Hash: rs.ProposalBlock.Hash(), PartSetHeader: rs.ProposalBlockParts.Header()},
			newVss[i])
	}
	ensureNewRound(t, newRoundCh, height+1, 0)

	sim.Chain = []*types.Block{}
	sim.Commits = []*types.Commit{}
	for i := 1; i <= numBlocks; i++ {
		sim.Chain = append(sim.Chain, css[0].blockStore.LoadBlock(int64(i)))
		sim.Commits = append(sim.Commits, css[0].blockStore.LoadBlockCommit(int64(i)))
	}

	return sim
}

// Sync from scratch
func TestHandshakeReplayAll(t *testing.T) {
	ctx := t.Context()

	sim := setupSimulator(ctx, t)

	t.Cleanup(leaktest.Check(t))

	for _, m := range modes {
		testHandshakeReplay(ctx, t, sim, 0, m, false)
	}
	for _, m := range modes {
		testHandshakeReplay(ctx, t, sim, 0, m, true)
	}
}

// Sync many, not from scratch
func TestHandshakeReplaySome(t *testing.T) {
	ctx := t.Context()

	sim := setupSimulator(ctx, t)

	t.Cleanup(leaktest.Check(t))

	for _, m := range modes {
		testHandshakeReplay(ctx, t, sim, 2, m, false)
	}
	for _, m := range modes {
		testHandshakeReplay(ctx, t, sim, 2, m, true)
	}
}

// Sync from lagging by one
func TestHandshakeReplayOne(t *testing.T) {
	ctx := t.Context()

	sim := setupSimulator(ctx, t)

	for _, m := range modes {
		testHandshakeReplay(ctx, t, sim, numBlocks-1, m, false)
	}
	for _, m := range modes {
		testHandshakeReplay(ctx, t, sim, numBlocks-1, m, true)
	}
}

// Sync from caught up
func TestHandshakeReplayNone(t *testing.T) {
	ctx := t.Context()

	sim := setupSimulator(ctx, t)

	t.Cleanup(leaktest.Check(t))

	for _, m := range modes {
		testHandshakeReplay(ctx, t, sim, numBlocks, m, false)
	}
	for _, m := range modes {
		testHandshakeReplay(ctx, t, sim, numBlocks, m, true)
	}
}

// Make some blocks. Start a fresh app and apply nBlocks blocks.
// Then restart the app and sync it up with the remaining blocks
func testHandshakeReplay(
	rctx context.Context,
	t *testing.T,
	sim *simulatorTestSuite,
	nBlocks int,
	mode uint,
	testValidatorsChange bool,
) {
	var chain []*types.Block
	var commits []*types.Commit
	var store *mockBlockStore
	var stateDB dbm.DB
	var genesisState sm.State

	ctx, cancel := context.WithCancel(rctx)
	t.Cleanup(cancel)

	cfg := sim.Config

	logger := log.NewNopLogger()
	if testValidatorsChange {
		testConfig, err := ResetConfig(t.TempDir(), fmt.Sprintf("%s_%v_m", t.Name(), mode))
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(testConfig.RootDir) }()
		stateDB = dbm.NewMemDB()

		genesisState = sim.GenesisState
		cfg = sim.Config
		chain = append([]*types.Block{}, sim.Chain...) // copy chain
		commits = sim.Commits
		store = newMockBlockStore(t, cfg, genesisState.ConsensusParams)
	} else { // test single node
		testConfig, err := ResetConfig(t.TempDir(), fmt.Sprintf("%s_%v_s", t.Name(), mode))
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(testConfig.RootDir) }()
		privVal, err := privval.LoadFilePV(cfg.PrivValidator.KeyFile(), cfg.PrivValidator.StateFile())
		require.NoError(t, err)

		tmpCfg := getConfig(t)
		runStateUntilBlock(t, tmpCfg, numBlocks)
		wal, err := openWAL(tmpCfg.Consensus.WalFile())
		require.NoError(t, err)
		defer wal.Close()
		chain, commits = makeBlockchainFromWAL(t, wal, numBlocks)
		_, err = privVal.GetPubKey(ctx)
		require.NoError(t, err)
		stateDB, genesisState, store = stateAndStore(t, cfg, kvstore.ProtocolVersion)
	}
	stateStore := sm.NewStore(stateDB)
	store.chain = chain
	store.commits = commits

	state := genesisState.Copy()
	// run the chain through state.ApplyBlock to build up the tendermint state
	state = buildTMStateFromChain(
		ctx,
		t,
		logger,
		sim.Mempool,
		sim.Evpool,
		stateStore,
		state,
		chain,
		mode,
		store,
	)
	latestAppHash := state.AppHash

	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	app := kvstore.NewApplication()
	if nBlocks > 0 {
		// run nBlocks against a new client to build up the app state.
		// use a throwaway tendermint state
		stateDB1 := dbm.NewMemDB()
		stateStore := sm.NewStore(stateDB1)
		err := stateStore.Save(genesisState)
		require.NoError(t, err)
		buildAppStateFromChain(ctx, t, app, stateStore, sim.Mempool, sim.Evpool, genesisState, chain, eventBus, nBlocks, mode, store)
	}

	// Prune block store if requested
	expectError := false
	if mode == 3 {
		pruned, err := store.PruneBlocks(2)
		require.NoError(t, err)
		require.Equal(t, 1, pruned)
		expectError = int64(nBlocks) < 2
	}

	// now start the app using the handshake - it should sync
	genDoc, err := sm.MakeGenesisDocFromFile(cfg.GenesisFile())
	require.NoError(t, err)
	handshaker := NewHandshaker(logger, stateStore, state, store, eventBus, genDoc)
	err = handshaker.Handshake(ctx, app)
	if expectError {
		require.Error(t, err)
		return
	}
	require.NoError(t, err, "Error on abci handshake")

	// get the latest app hash from the app
	res, err := app.Info(ctx, &abci.RequestInfo{Version: ""})
	if err != nil {
		t.Fatal(err)
	}

	// the app hash should be synced up
	if !bytes.Equal(latestAppHash, res.LastBlockAppHash) {
		t.Fatalf(
			"Expected app hashes to match after handshake/replay. got %X, expected %X",
			res.LastBlockAppHash,
			latestAppHash)
	}

	expectedBlocksToSync := numBlocks - nBlocks
	if nBlocks == numBlocks && mode > 0 {
		expectedBlocksToSync++
	} else if nBlocks > 0 && mode == 1 {
		expectedBlocksToSync++
	}

	if handshaker.NBlocks() != expectedBlocksToSync {
		t.Fatalf("Expected handshake to sync %d blocks, got %d", expectedBlocksToSync, handshaker.NBlocks())
	}
}

func applyBlock(
	ctx context.Context,
	t *testing.T,
	stateStore sm.Store,
	mempool mempool.Mempool,
	evpool sm.EvidencePool,
	st sm.State,
	blk *types.Block,
	appClient abci.Application,
	blockStore *mockBlockStore,
	eventBus *eventbus.EventBus,
) sm.State {
	testPartSize := types.BlockPartSizeBytes
	blockExec := sm.NewBlockExecutor(stateStore, log.NewNopLogger(), appClient, mempool, evpool, blockStore, eventBus, sm.NopMetrics())

	bps, err := blk.MakePartSet(testPartSize)
	require.NoError(t, err)
	blkID := types.BlockID{Hash: blk.Hash(), PartSetHeader: bps.Header()}
	newState, err := blockExec.ApplyBlock(ctx, st, blkID, blk, nil)
	require.NoError(t, err)
	return newState
}

func buildAppStateFromChain(
	ctx context.Context,
	t *testing.T,
	appClient abci.Application,
	stateStore sm.Store,
	mempool mempool.Mempool,
	evpool sm.EvidencePool,
	state sm.State,
	chain []*types.Block,
	eventBus *eventbus.EventBus,
	nBlocks int,
	mode uint,
	blockStore *mockBlockStore,
) {
	t.Helper()
	// start a new app without handshake, play nBlocks blocks
	state.Version.Consensus.App = kvstore.ProtocolVersion // simulate handshake, receive app version
	validators := types.TM2PB.ValidatorUpdates(state.Validators)
	_, err := appClient.InitChain(ctx, &abci.RequestInitChain{
		Validators: validators,
	})
	require.NoError(t, err)

	require.NoError(t, stateStore.Save(state)) // save height 1's validatorsInfo

	switch mode {
	case 0:
		for i := range nBlocks {
			block := chain[i]
			state = applyBlock(ctx, t, stateStore, mempool, evpool, state, block, appClient, blockStore, eventBus)
		}
	case 1, 2, 3:
		for i := 0; i < nBlocks-1; i++ {
			block := chain[i]
			state = applyBlock(ctx, t, stateStore, mempool, evpool, state, block, appClient, blockStore, eventBus)
		}

		if mode == 2 || mode == 3 {
			// update the kvstore height and apphash
			// as if we ran commit but not
			state = applyBlock(ctx, t, stateStore, mempool, evpool, state, chain[nBlocks-1], appClient, blockStore, eventBus)
		}
	default:
		require.Fail(t, "unknown mode %v", mode)
	}

}

func buildTMStateFromChain(
	ctx context.Context,
	t *testing.T,
	logger log.Logger,
	mempool mempool.Mempool,
	evpool sm.EvidencePool,
	stateStore sm.Store,
	state sm.State,
	chain []*types.Block,
	mode uint,
	blockStore *mockBlockStore,
) sm.State {
	t.Helper()

	// run the whole chain against this client to build up the tendermint state
	app := kvstore.NewApplication()

	state.Version.Consensus.App = kvstore.ProtocolVersion // simulate handshake, receive app version
	validators := types.TM2PB.ValidatorUpdates(state.Validators)
	_, err := app.InitChain(ctx, &abci.RequestInitChain{
		Validators: validators,
	})
	require.NoError(t, err)

	require.NoError(t, stateStore.Save(state))

	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	switch mode {
	case 0:
		// sync right up
		for _, block := range chain {
			state = applyBlock(ctx, t, stateStore, mempool, evpool, state, block, app, blockStore, eventBus)
		}

	case 1, 2, 3:
		// sync up to the penultimate as if we stored the block.
		// whether we commit or not depends on the appHash
		for _, block := range chain[:len(chain)-1] {
			state = applyBlock(ctx, t, stateStore, mempool, evpool, state, block, app, blockStore, eventBus)
		}

		// apply the final block to a state copy so we can
		// get the right next appHash but keep the state back
		applyBlock(ctx, t, stateStore, mempool, evpool, state, chain[len(chain)-1], app, blockStore, eventBus)
	default:
		require.Fail(t, "unknown mode %v", mode)
	}

	return state
}

func TestHandshakeErrorsIfAppReturnsWrongAppHash(t *testing.T) {
	// 1. Initialize tendermint and commit 3 blocks with the following app hashes:
	//		- 0x01
	//		- 0x02
	//		- 0x03

	ctx := t.Context()

	cfg, err := ResetConfig(t.TempDir(), "handshake_test_")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(cfg.RootDir) })
	privVal, err := privval.LoadFilePV(cfg.PrivValidator.KeyFile(), cfg.PrivValidator.StateFile())
	require.NoError(t, err)
	const appVersion = 0x0
	_, err = privVal.GetPubKey(ctx)
	require.NoError(t, err)
	stateDB, state, store := stateAndStore(t, cfg, appVersion)
	stateStore := sm.NewStore(stateDB)
	genDoc, err := sm.MakeGenesisDocFromFile(cfg.GenesisFile())
	require.NoError(t, err)
	state.LastValidators = state.Validators.Copy()
	// mode = 0 for committing all the blocks
	blocks := sf.MakeBlocks(ctx, t, 3, &state, privVal)

	store.chain = blocks

	logger := log.NewNopLogger()

	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	// 2. Tendermint must panic if app returns wrong hash for the first block
	//		- RANDOM HASH
	//		- 0x02
	//		- 0x03
	{
		app := &badApp{numBlocks: 3, allHashesAreWrong: true}
		h := NewHandshaker(logger, stateStore, state, store, eventBus, genDoc)
		assert.Error(t, h.Handshake(ctx, app))
	}

	// 3. Tendermint must panic if app returns wrong hash for the last block
	//		- 0x01
	//		- 0x02
	//		- RANDOM HASH
	{
		app := &badApp{numBlocks: 3, onlyLastHashIsWrong: true}
		h := NewHandshaker(logger, stateStore, state, store, eventBus, genDoc)
		require.Error(t, h.Handshake(ctx, app))
	}
}

type badApp struct {
	abci.BaseApplication
	numBlocks           byte
	height              byte
	allHashesAreWrong   bool
	onlyLastHashIsWrong bool
}

func (app *badApp) FinalizeBlock(_ context.Context, _ *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	app.height++
	if app.onlyLastHashIsWrong {
		if app.height == app.numBlocks {
			return &abci.ResponseFinalizeBlock{AppHash: tmrand.Bytes(8)}, nil
		}
		return &abci.ResponseFinalizeBlock{AppHash: []byte{app.height}}, nil
	} else if app.allHashesAreWrong {
		return &abci.ResponseFinalizeBlock{AppHash: tmrand.Bytes(8)}, nil
	}

	panic("either allHashesAreWrong or onlyLastHashIsWrong must be set")
}

//--------------------------
// utils for making blocks

func makeBlockchainFromWAL(t *testing.T, wal *WAL, lastHeight int64) ([]*types.Block, []*types.Commit) {
	t.Helper()

	// Search for height marker
	msgs := dumpWAL(t, wal)
	height := int64(0)
	var blocks []*types.Block
	var commits []*types.Commit
	var thisBlockParts *types.PartSet
	var thisBlockCommit *types.Commit

	for _, msg := range msgs {
		if height == lastHeight {
			break
		}
		piece := readPieceFromWAL(msg)
		if piece == nil {
			continue
		}

		switch p := piece.(type) {
		case EndHeightMessage:
			// if its not the first one, we have a full block
			if thisBlockParts != nil {
				bz, err := io.ReadAll(thisBlockParts.GetReader())
				require.NoError(t, err)
				pbb := &tmproto.Block{}
				require.NoError(t, proto.Unmarshal(bz, pbb))
				block, err := types.BlockFromProto(pbb)
				require.NoError(t, err)
				require.Equal(t, block.Height, height+1,
					"read bad block from wal. got height %d, expected %d", block.Height, height+1)
				commitHeight := thisBlockCommit.Height
				require.Equal(t, commitHeight, height+1,
					"commit doesnt match. got height %d, expected %d", commitHeight, height+1)

				blocks = append(blocks, block)
				commits = append(commits, thisBlockCommit)
				height++
			}
		case *types.PartSetHeader:
			thisBlockParts = types.NewPartSetFromHeader(*p)
		case *types.Part:
			_, err := thisBlockParts.AddPart(p)
			require.NoError(t, err)
		case *types.Vote:
			if p.Type == tmproto.PrecommitType {
				thisBlockCommit = &types.Commit{
					Height:     p.Height,
					Round:      p.Round,
					BlockID:    p.BlockID,
					Signatures: []types.CommitSig{p.CommitSig()},
				}
			}
		}
	}
	return blocks, commits
}

func readPieceFromWAL(msg WALMessage) any {
	switch m := msg.any.(type) {
	case msgInfo:
		switch msg := m.Msg.(type) {
		case *ProposalMessage:
			return &msg.Proposal.BlockID.PartSetHeader
		case *BlockPartMessage:
			return msg.Part
		case *VoteMessage:
			return msg.Vote
		}
	case EndHeightMessage:
		return m
	}

	return nil
}

// fresh state and mock store
func stateAndStore(
	t *testing.T,
	cfg *config.Config,
	appVersion uint64,
) (dbm.DB, sm.State, *mockBlockStore) {
	stateDB := dbm.NewMemDB()
	stateStore := sm.NewStore(stateDB)
	state, err := sm.MakeGenesisStateFromFile(cfg.GenesisFile())
	require.NoError(t, err)
	state.Version.Consensus.App = appVersion
	store := newMockBlockStore(t, cfg, state.ConsensusParams)
	require.NoError(t, stateStore.Save(state))

	return stateDB, state, store
}

//----------------------------------
// mock block store

type mockBlockStore struct {
	cfg     *config.Config
	params  types.ConsensusParams
	chain   []*types.Block
	commits []*types.Commit
	base    int64
	t       *testing.T
}

var _ sm.BlockStore = &mockBlockStore{}

// TODO: NewBlockStore(db.NewMemDB) ...
func newMockBlockStore(t *testing.T, cfg *config.Config, params types.ConsensusParams) *mockBlockStore {
	return &mockBlockStore{
		cfg:    cfg,
		params: params,
		t:      t,
	}
}

func (bs *mockBlockStore) Height() int64                       { return int64(len(bs.chain)) }
func (bs *mockBlockStore) Base() int64                         { return bs.base }
func (bs *mockBlockStore) Size() int64                         { return bs.Height() - bs.Base() + 1 }
func (bs *mockBlockStore) LoadBaseMeta() *types.BlockMeta      { return bs.LoadBlockMeta(bs.base) }
func (bs *mockBlockStore) LoadBlock(height int64) *types.Block { return bs.chain[height-1] }
func (bs *mockBlockStore) LoadBlockByHash(hash []byte) *types.Block {
	return bs.chain[int64(len(bs.chain))-1]
}
func (bs *mockBlockStore) LoadBlockMetaByHash(hash []byte) *types.BlockMeta { return nil }
func (bs *mockBlockStore) LoadBlockMeta(height int64) *types.BlockMeta {
	block := bs.chain[height-1]
	bps, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(bs.t, err)
	return &types.BlockMeta{
		BlockID: types.BlockID{Hash: block.Hash(), PartSetHeader: bps.Header()},
		Header:  block.Header,
	}
}
func (bs *mockBlockStore) LoadBlockPart(height int64, index int) *types.Part { return nil }
func (bs *mockBlockStore) SaveBlock(block *types.Block, blockParts *types.PartSet, seenCommit *types.Commit) {
}

func (bs *mockBlockStore) LoadBlockCommit(height int64) *types.Commit {
	return bs.commits[height-1]
}

func (bs *mockBlockStore) LoadSeenCommit() *types.Commit {
	return bs.commits[len(bs.commits)-1]
}

func (bs *mockBlockStore) PruneBlocks(height int64) (uint64, error) {
	pruned := uint64(0)
	for i := int64(0); i < height-1; i++ {
		bs.chain[i] = nil
		bs.commits[i] = nil
		pruned++
	}
	bs.base = height
	return pruned, nil
}

func (bs *mockBlockStore) DeleteLatestBlock() error { return nil }

//---------------------------------------
// Test handshake/init chain

func TestHandshakeUpdatesValidators(t *testing.T) {
	ctx := t.Context()

	logger := log.NewNopLogger()
	votePower := 10 + int64(rand.Uint32())
	val, _, err := factory.Validator(ctx, votePower)
	require.NoError(t, err)
	vals := types.NewValidatorSet([]*types.Validator{val})
	app := &initChainApp{vals: types.TM2PB.ValidatorUpdates(vals)}

	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	cfg, err := ResetConfig(t.TempDir(), "handshake_test_")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(cfg.RootDir) })

	privVal, err := privval.LoadFilePV(cfg.PrivValidator.KeyFile(), cfg.PrivValidator.StateFile())
	require.NoError(t, err)
	_, err = privVal.GetPubKey(ctx)
	require.NoError(t, err)
	stateDB, state, store := stateAndStore(t, cfg, 0x0)
	stateStore := sm.NewStore(stateDB)

	oldValAddr := state.Validators.Validators[0].Address

	// now start the app using the handshake - it should sync
	genDoc, err := sm.MakeGenesisDocFromFile(cfg.GenesisFile())
	require.NoError(t, err)

	handshaker := NewHandshaker(logger, stateStore, state, store, eventBus, genDoc)
	require.NoError(t, handshaker.Handshake(ctx, app), "error on abci handshake")

	// reload the state, check the validator set was updated
	state, err = stateStore.Load()
	require.NoError(t, err)

	newValAddr := state.Validators.Validators[0].Address
	expectValAddr := val.Address
	assert.NotEqual(t, oldValAddr, newValAddr)
	assert.Equal(t, newValAddr, expectValAddr)
}

// returns the vals on InitChain
type initChainApp struct {
	abci.BaseApplication
	vals []abci.ValidatorUpdate
}

func (ica *initChainApp) InitChain(_ context.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	return &abci.ResponseInitChain{Validators: ica.vals}, nil
}
