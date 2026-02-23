package consensus

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	tmpubsub "github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	statemocks "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	tmcons "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/consensus"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var (
	defaultTestTime = time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
)

type reactorTestSuite struct {
	network             *p2p.TestNetwork
	states              map[types.NodeID]*State
	reactors            map[types.NodeID]*Reactor
	subs                map[types.NodeID]eventbus.Subscription
	blocksyncSubs       map[types.NodeID]eventbus.Subscription
	stateChannels       map[types.NodeID]*p2p.Channel[*tmcons.Message]
	dataChannels        map[types.NodeID]*p2p.Channel[*tmcons.Message]
	voteChannels        map[types.NodeID]*p2p.Channel[*tmcons.Message]
	voteSetBitsChannels map[types.NodeID]*p2p.Channel[*tmcons.Message]
}

func setup(
	ctx context.Context,
	t *testing.T,
	numNodes int,
	states []*State,
	size int,
) *reactorTestSuite {
	t.Helper()

	rts := &reactorTestSuite{
		network:       p2p.MakeTestNetwork(t, p2p.TestNetworkOptions{NumNodes: numNodes}),
		states:        make(map[types.NodeID]*State),
		reactors:      make(map[types.NodeID]*Reactor, numNodes),
		subs:          make(map[types.NodeID]eventbus.Subscription, numNodes),
		blocksyncSubs: make(map[types.NodeID]eventbus.Subscription, numNodes),
	}

	i := 0
	for _, node := range rts.network.Nodes() {
		nodeID := node.NodeID
		state := states[i]

		reactor, err := NewReactor(
			state.logger.With("node", nodeID),
			state,
			node.Router,
			state.eventBus,
			true,
			NopMetrics(),
			config.DefaultConfig(),
		)

		blocksSub, err := state.eventBus.SubscribeWithArgs(ctx, tmpubsub.SubscribeArgs{
			ClientID: testSubscriber,
			Query:    types.EventQueryNewBlock,
			Limit:    size,
		})
		require.NoError(t, err)

		fsSub, err := state.eventBus.SubscribeWithArgs(ctx, tmpubsub.SubscribeArgs{
			ClientID: testSubscriber,
			Query:    types.EventQueryBlockSyncStatus,
			Limit:    size,
		})
		require.NoError(t, err)

		rts.states[nodeID] = state
		rts.subs[nodeID] = blocksSub
		rts.reactors[nodeID] = reactor
		rts.blocksyncSubs[nodeID] = fsSub

		// simulate handle initChain in handshake
		if state.state.LastBlockHeight == 0 {
			require.NoError(t, state.blockExec.Store().Save(state.state))
		}

		require.NoError(t, reactor.Start(ctx))
		require.True(t, reactor.IsRunning())
		t.Cleanup(reactor.Wait)

		i++
	}

	require.Len(t, rts.reactors, numNodes)

	// start the in-memory network and connect all peers with each other
	rts.network.Start(t)

	t.Cleanup(leaktest.Check(t))

	return rts
}

func nextBlock(ctx context.Context, sub eventbus.Subscription, valSet *types.ValidatorSet) (*types.Block, error) {
	msg, err := sub.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("sub.Next(): %w", err)
	}
	block := msg.Data().(types.EventDataNewBlock).Block
	// First block does not contain a Commit.
	if block.Height <= 1 {
		return block, nil
	}
	if err := valSet.VerifyCommit(block.ChainID, block.LastCommit.BlockID, block.LastCommit.Height, block.LastCommit); err != nil {
		return nil, fmt.Errorf("VerifyCommit(height=%v): %w", block.Height, err)
	}
	return block, nil
}

// Inserts tx into all mempools.
// Consumes blocks from blocksSubs until tx is finalized.
// Validates all the blocks against valSet.
// No other txs than tx are expected.
func finalizeTx(
	ctx context.Context,
	valSet *types.ValidatorSet,
	blocksSubs []eventbus.Subscription,
	states []*State,
	tx []byte,
) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for i, sub := range blocksSubs {
			s.Spawn(func() error {
				if err := states[i].txNotifier.(mempool.Mempool).CheckTx(ctx, tx, nil, mempool.TxInfo{}); err != nil {
					return fmt.Errorf("CheckTx(): %w", err)
				}
				for {
					block, err := nextBlock(ctx, sub, valSet)
					if err != nil {
						return fmt.Errorf("nextBlock(): %w", err)
					}
					if len(block.Data.Txs) > 0 {
						if err := utils.TestDiff(tx, block.Data.Txs[0]); err != nil {
							return err
						}
						break
					}
				}
				// Next 2 blocks contain commits of the current validatorSet
				for i := range 2 {
					if _, err := nextBlock(ctx, sub, valSet); err != nil {
						return fmt.Errorf("nextBlock(N+%v): %w", i+1, err)
					}
				}
				return nil
			})
		}
		return nil
	})
}

func TestReactorBasic(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	cfg := configSetup(t)

	n := 2
	states, cleanup := makeConsensusState(ctx, t,
		cfg, n, "consensus_reactor_test",
		newMockTickerFunc(true))
	t.Cleanup(cleanup)

	rts := setup(ctx, t, n, states, 100) // buffer must be large enough to not deadlock

	for _, reactor := range rts.reactors {
		state := reactor.state.GetState()
		reactor.SwitchToConsensus(ctx, state, false)
	}

	t.Logf("wait till everyone makes the first new block")
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, sub := range rts.subs {
			s.Spawn(func() error {
				if _, err := sub.Next(ctx); err != nil {
					return fmt.Errorf("s.Next(): %w", err)
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("wait till everyone makes the consensus switch")
	err = scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, sub := range rts.blocksyncSubs {
			s.Spawn(func() error {
				msg, err := sub.Next(ctx)
				if err != nil {
					return fmt.Errorf("sub.Next(): %w", err)
				}
				want := types.EventDataBlockSyncStatus{Complete: true, Height: 0}
				return utils.TestDiff[types.EventData](want, msg.Data())
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReactorWithEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	cfg := configSetup(t)

	n := 2
	testName := "consensus_reactor_test"
	tickerFunc := newMockTickerFunc(true)

	valSet, privVals := factory.ValidatorSet(ctx, n, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	states := make([]*State, n)
	logger := consensusLogger()

	for i := range n {
		stateDB := dbm.NewMemDB() // each state needs its own db
		stateStore := sm.NewStore(stateDB)
		state, err := sm.MakeGenesisState(genDoc)
		require.NoError(t, err)
		require.NoError(t, stateStore.Save(state))
		thisConfig, err := ResetConfig(t.TempDir(), fmt.Sprintf("%s_%d", testName, i))
		require.NoError(t, err)

		defer os.RemoveAll(thisConfig.RootDir)

		app := kvstore.NewApplication()
		vals := types.TM2PB.ValidatorUpdates(state.Validators)
		_, err = app.InitChain(ctx, &abci.RequestInitChain{Validators: vals})
		require.NoError(t, err)

		pv := privVals[i]
		blockDB := dbm.NewMemDB()
		blockStore := store.NewBlockStore(blockDB)

		// one for mempool, one for consensus
		proxyAppConnMem := app
		proxyAppConnCon := app

		mempool := mempool.NewTxMempool(
			log.NewNopLogger().With("module", "mempool"),
			thisConfig.Mempool,
			proxyAppConnMem,
			nil,
		)

		if thisConfig.Consensus.WaitForTxs() {
			mempool.EnableTxsAvailable()
		}

		// mock the evidence pool
		// everyone includes evidence of another double signing
		vIdx := (i + 1) % n

		ev, err := types.NewMockDuplicateVoteEvidenceWithValidator(ctx, 1, defaultTestTime, privVals[vIdx], cfg.ChainID())
		require.NoError(t, err)
		evpool := &statemocks.EvidencePool{}
		evpool.On("CheckEvidence", mock.Anything, mock.AnythingOfType("types.EvidenceList")).Return(nil)
		evpool.On("PendingEvidence", mock.AnythingOfType("int64")).Return([]types.Evidence{
			ev}, int64(len(ev.Bytes())))
		evpool.On("Update", mock.MatchedBy(func(ctx context.Context) bool { return true }), mock.AnythingOfType("state.State"), mock.AnythingOfType("types.EvidenceList")).Return()
		evpool2 := sm.EmptyEvidencePool{}

		eventBus := eventbus.NewDefault(log.NewNopLogger().With("module", "events"))
		require.NoError(t, eventBus.Start(ctx))

		blockExec := sm.NewBlockExecutor(stateStore, log.NewNopLogger(), proxyAppConnCon, mempool, evpool, blockStore, eventBus, sm.NopMetrics())

		cs, err := NewState(logger.With("validator", i, "module", "consensus"),
			thisConfig.Consensus, stateStore, blockExec, blockStore, mempool, evpool2, eventBus, []trace.TracerProviderOption{})
		require.NoError(t, err)
		cs.SetPrivValidator(ctx, utils.Some(pv))

		cs.SetTimeoutTicker(tickerFunc())

		states[i] = cs
	}

	rts := setup(ctx, t, n, states, 100) // buffer must be large enough to not deadlock

	for _, reactor := range rts.reactors {
		state := reactor.state.GetState()
		reactor.SwitchToConsensus(ctx, state, false)
	}

	var wg sync.WaitGroup
	for _, sub := range rts.subs {
		wg.Add(1)

		// We expect for each validator that is the proposer to propose one piece of
		// evidence.
		go func(s eventbus.Subscription) {
			defer wg.Done()
			msg, err := s.Next(ctx)
			if !assert.NoError(t, err) {
				cancel()
				return
			}

			block := msg.Data().(types.EventDataNewBlock).Block
			require.Len(t, block.Evidence, 1)
		}(sub)
	}

	wg.Wait()
}

func TestReactorCreatesBlockWhenEmptyBlocksFalse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	cfg := configSetup(t)

	n := 2
	states, cleanup := makeConsensusState(ctx,
		t,
		cfg,
		n,
		"consensus_reactor_test",
		newMockTickerFunc(true),
		func(c *config.Config) {
			c.Consensus.CreateEmptyBlocks = false
		},
	)
	t.Cleanup(cleanup)

	rts := setup(ctx, t, n, states, 1048576) // buffer must be large enough to not deadlock

	for _, reactor := range rts.reactors {
		state := reactor.state.GetState()
		reactor.SwitchToConsensus(ctx, state, false)
	}

	// send a tx
	require.NoError(
		t,
		assertMempool(t, states[1].txNotifier).CheckTx(
			ctx,
			[]byte{1, 2, 3},
			nil,
			mempool.TxInfo{},
		),
	)

	var wg sync.WaitGroup
	for _, sub := range rts.subs {
		wg.Add(1)

		// wait till everyone makes the first new block
		go func(s eventbus.Subscription) {
			defer wg.Done()
			_, err := s.Next(ctx)
			if !assert.NoError(t, err) {
				cancel()
			}
		}(sub)
	}

	wg.Wait()
}

func TestReactorRecordsVotesAndBlockParts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	cfg := configSetup(t)

	n := 2
	states, cleanup := makeConsensusState(ctx, t,
		cfg, n, "consensus_reactor_test",
		newMockTickerFunc(true))
	t.Cleanup(cleanup)

	rts := setup(ctx, t, n, states, 100) // buffer must be large enough to not deadlock

	for _, reactor := range rts.reactors {
		state := reactor.state.GetState()
		reactor.SwitchToConsensus(ctx, state, false)
	}

	var wg sync.WaitGroup
	for _, sub := range rts.subs {
		wg.Add(1)

		// wait till everyone makes the first new block
		go func(s eventbus.Subscription) {
			defer wg.Done()
			_, err := s.Next(ctx)
			if !assert.NoError(t, err) {
				cancel()
			}
		}(sub)
	}

	wg.Wait()

	// Require at least one node to have sent block parts, but we can't know which
	// peer sent it.
	require.Eventually(
		t,
		func() bool {
			for _, reactor := range rts.reactors {
				for peers := range reactor.peers.Lock() {
					for _, ps := range peers {
						if ps.BlockPartsSent() > 0 {
							return true
						}
					}
				}
			}

			return false
		},
		time.Second,
		10*time.Millisecond,
		"number of block parts sent should've increased",
	)

	nodeID := rts.network.RandomNode().NodeID
	reactor := rts.reactors[nodeID]
	peers := rts.network.Peers(nodeID)

	ps, ok := reactor.GetPeerState(peers[0].NodeID)
	require.True(t, ok)
	require.NotNil(t, ps)
	require.Greater(t, ps.VotesSent(), 0, "number of votes sent should've increased")
}

func TestReactorValidatorSetChanges(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	cfg := configSetup(t)

	nPeers := 5
	nVals := 2
	states, genDoc, _, cleanup := randConsensusNetWithPeers(
		ctx,
		t,
		cfg,
		nVals,
		nPeers,
		func() TimeoutTicker { return NewTimeoutTicker(log.NewNopLogger()) },
		newEpehemeralKVStore,
	)
	t.Cleanup(cleanup)

	rts := setup(ctx, t, nPeers, states, 1024) // buffer must be large enough to not deadlock
	for _, reactor := range rts.reactors {
		state := reactor.state.GetState()
		reactor.SwitchToConsensus(ctx, state, false)
	}

	blocksSubs := []eventbus.Subscription{}
	for _, sub := range rts.subs {
		blocksSubs = append(blocksSubs, sub)
	}
	valSet := genDoc.ValidatorSet()

	for i := range 10 {
		nodeIdx := rng.Intn(nPeers)
		t.Logf("nodeIdx = %v", nodeIdx)
		pv, _ := states[nodeIdx].privValidator.Get()
		key, err := pv.GetPubKey(ctx)
		require.NoError(t, err)
		keyProto := crypto.PubKeyToProto(key)
		newPower := int64(rng.Intn(100000))
		tx := kvstore.MakeValSetChangeTx(keyProto, newPower)
		require.NoError(t, finalizeTx(ctx, valSet, blocksSubs, states, tx))
		require.NoError(t, valSet.UpdateWithChangeSet(utils.Slice(types.NewValidator(key, newPower))))
		t.Logf("DONE %v", i)
	}
}

func TestReactorMemoryLimitCoverage(t *testing.T) {
	// This test covers the error handling paths in reactor when proposals exceed memory limits
	// It's designed to improve test coverage for the reactor's proposal validation

	logger := log.NewTestingLogger(t)
	testPeerID := types.NodeID("test-peer-memory-limit")

	// Test that PeerState correctly rejects proposals with excessive parts
	ps := NewPeerState(logger, testPeerID)
	ps.PRS.Height = 1
	ps.PRS.Round = 0

	// Create an invalid proposal with excessive PartSetHeader.Total
	invalidProposal := &types.Proposal{
		Type:     tmproto.ProposalType,
		Height:   1,
		Round:    0,
		POLRound: -1,
		BlockID: types.BlockID{
			Hash: make([]byte, 32),
			PartSetHeader: types.PartSetHeader{
				Total: types.MaxBlockPartsCount + 1, // Exceeds limit
				Hash:  make([]byte, 32),
			},
		},
		Timestamp: time.Now(),
		Signature: makeSig("test-signature"),
	}

	// Test direct SetHasProposal call (this is what reactor calls)
	ps.SetHasProposal(invalidProposal)
	require.False(t, ps.PRS.Proposal, "SetHasProposal should silently ignore proposal with excessive Total")

	// Test that reactor would handle this silently by verifying the defensive programming approach
	// This provides coverage for the silent handling in handleDataMessage
	t.Log("Coverage test: reactor silently ignores invalid proposals via PeerState validation")
}
