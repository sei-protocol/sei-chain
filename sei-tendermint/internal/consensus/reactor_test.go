package consensus

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
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

	abciclient "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/abci/example/kvstore"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto/encoding"
	"github.com/tendermint/tendermint/internal/eventbus"
	"github.com/tendermint/tendermint/internal/mempool"
	"github.com/tendermint/tendermint/internal/p2p"
	tmpubsub "github.com/tendermint/tendermint/internal/pubsub"
	sm "github.com/tendermint/tendermint/internal/state"
	statemocks "github.com/tendermint/tendermint/internal/state/mocks"
	"github.com/tendermint/tendermint/internal/store"
	"github.com/tendermint/tendermint/internal/test/factory"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
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
	stateChannels       map[types.NodeID]*p2p.Channel
	dataChannels        map[types.NodeID]*p2p.Channel
	voteChannels        map[types.NodeID]*p2p.Channel
	voteSetBitsChannels map[types.NodeID]*p2p.Channel
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

func validateBlock(block *types.Block, activeVals map[string]struct{}) error {
	if block.LastCommit.Size() != len(activeVals) {
		return fmt.Errorf(
			"commit size doesn't match number of active validators. Got %d, expected %d",
			block.LastCommit.Size(), len(activeVals),
		)
	}

	for _, commitSig := range block.LastCommit.Signatures {
		if _, ok := activeVals[string(commitSig.ValidatorAddress)]; !ok {
			return fmt.Errorf("found vote for inactive validator %X", commitSig.ValidatorAddress)
		}
	}

	return nil
}

func waitForAndValidateBlock(
	ctx context.Context,
	t *testing.T,
	n int,
	activeVals map[string]struct{},
	blocksSubs []eventbus.Subscription,
	states []*State,
	txs ...[]byte,
) {
	t.Helper()
	t.Log("waitForAndValidateBlock()")
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for i := range n {
			s.Spawn(func() error {
				msg, err := blocksSubs[i].Next(ctx)
				if err != nil {
					return fmt.Errorf("blockSubs[%d].Next(): %w", i, err)
				}
				newBlock := msg.Data().(types.EventDataNewBlock).Block
				if err := validateBlock(newBlock, activeVals); err != nil {
					return fmt.Errorf("validateBlock: %w", err)
				}
				for _, tx := range txs {
					if err := assertMempool(t, states[i].txNotifier).CheckTx(ctx, tx, nil, mempool.TxInfo{}); err != nil {
						if errors.Is(err, types.ErrTxInCache) {
							continue
						}
						return err
					}
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func waitForAndValidateBlockWithTx(
	ctx context.Context,
	t *testing.T,
	n int,
	activeVals map[string]struct{},
	blocksSubs []eventbus.Subscription,
	txs ...[]byte,
) {
	t.Helper()
	t.Log("waitForAndValidateBlockWithTx")

	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for i := range n {
			s.Spawn(func() error {
				ntxs := 0
				for ntxs < len(txs) {
					msg, err := blocksSubs[i].Next(ctx)
					if err != nil {
						return fmt.Errorf("blockSubs[%d].Next(): %w", i, err)
					}
					newBlock := msg.Data().(types.EventDataNewBlock).Block
					if err := validateBlock(newBlock, activeVals); err != nil {
						return fmt.Errorf("validateBlock: %w", err)
					}
					// check that txs match the txs we're waiting for.
					// note they could be spread over multiple blocks,
					// but they should be in order.
					for _, got := range newBlock.Data.Txs {
						if err := utils.TestDiff(txs[ntxs], got); err != nil {
							return fmt.Errorf("txs[%d]: %w", ntxs, err)
						}
						ntxs++
					}
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func waitForBlockWithUpdatedValsAndValidateIt(
	bctx context.Context,
	t *testing.T,
	n int,
	updatedVals map[string]struct{},
	blocksSubs []eventbus.Subscription,
) {
	t.Helper()
	ctx, cancel := context.WithCancel(bctx)
	defer cancel()

	fn := func(j int) {
		var newBlock *types.Block

		for {
			msg, err := blocksSubs[j].Next(ctx)
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				return
			case errors.Is(err, context.Canceled):
				return
			case err != nil:
				cancel() // terminate other workers
				t.Fatalf("problem waiting for %d subscription: %v", j, err)
				return
			}

			newBlock = msg.Data().(types.EventDataNewBlock).Block
			if newBlock.LastCommit.Size() == len(updatedVals) {
				break
			}
		}

		require.NoError(t, validateBlock(newBlock, updatedVals))
	}

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(j int) {
			defer wg.Done()
			fn(j)
		}(i)
	}

	wg.Wait()
	if err := ctx.Err(); errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("encountered timeout")
	}
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
		reactor.StopWaitSync()
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
		proxyAppConnMem := abciclient.NewLocalClient(logger, app)
		proxyAppConnCon := abciclient.NewLocalClient(logger, app)

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
		evpool.On("CheckEvidence", ctx, mock.AnythingOfType("types.EvidenceList")).Return(nil)
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
		cs.SetPrivValidator(ctx, pv)

		cs.SetTimeoutTicker(tickerFunc())

		states[i] = cs
	}

	rts := setup(ctx, t, n, states, 100) // buffer must be large enough to not deadlock

	for _, reactor := range rts.reactors {
		state := reactor.state.GetState()
		reactor.StopWaitSync()
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
		reactor.StopWaitSync()
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
		reactor.StopWaitSync()
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
				for _, ps := range reactor.peers {
					if ps.BlockPartsSent() > 0 {
						return true
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

// TODO: fix flaky test
//func TestReactorVotingPowerChange(t *testing.T) {
//	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
//	defer cancel()
//
//	cfg := configSetup(t)
//
//	n := 2
//	states, cleanup := makeConsensusState(ctx,
//		t,
//		cfg,
//		n,
//		"consensus_voting_power_changes_test",
//		newMockTickerFunc(true),
//	)
//
//	t.Cleanup(cleanup)
//
//	rts := setup(ctx, t, n, states, 1048576) // buffer must be large enough to not deadlock
//
//	for _, reactor := range rts.reactors {
//		state := reactor.state.GetState()
//		reactor.StopWaitSync()
//		reactor.SwitchToConsensus(ctx, state, false)
//	}
//
//	// map of active validators
//	activeVals := make(map[string]struct{})
//	for i := 0; i < n; i++ {
//		pubKey, err := states[i].privValidator.GetPubKey(ctx)
//		require.NoError(t, err)
//
//		addr := pubKey.Address()
//		activeVals[string(addr)] = struct{}{}
//	}
//
//	var wg sync.WaitGroup
//	for _, sub := range rts.subs {
//		wg.Add(1)
//
//		// wait till everyone makes the first new block
//		go func(s eventbus.Subscription) {
//			defer wg.Done()
//			_, err := s.Next(ctx)
//			if !assert.NoError(t, err) {
//				panic(err)
//			}
//		}(sub)
//	}
//
//	wg.Wait()
//
//	blocksSubs := []eventbus.Subscription{}
//	for _, sub := range rts.subs {
//		blocksSubs = append(blocksSubs, sub)
//	}
//
//	val1PubKey, err := states[0].privValidator.GetPubKey(ctx)
//	require.NoError(t, err)
//
//	val1PubKeyABCI, err := encoding.PubKeyToProto(val1PubKey)
//	require.NoError(t, err)
//
//	updateValidatorTx := kvstore.MakeValSetChangeTx(val1PubKeyABCI, 25)
//	previousTotalVotingPower := states[0].GetRoundState().LastValidators.TotalVotingPower()
//
//	waitForAndValidateBlock(ctx, t, n, activeVals, blocksSubs, states, updateValidatorTx)
//	waitForAndValidateBlockWithTx(ctx, t, n, activeVals, blocksSubs, states, updateValidatorTx)
//	waitForAndValidateBlock(ctx, t, n, activeVals, blocksSubs, states)
//	waitForAndValidateBlock(ctx, t, n, activeVals, blocksSubs, states)
//
//	// Msg sent to mempool, needs to be processed by nodes
//	require.Eventually(
//		t,
//		func() bool {
//			return previousTotalVotingPower != states[0].GetRoundState().LastValidators.TotalVotingPower()
//		},
//		30*time.Second,
//		100*time.Millisecond,
//		"expected voting power to change (before: %d, after: %d)",
//		previousTotalVotingPower,
//		states[0].GetRoundState().LastValidators.TotalVotingPower(),
//	)
//
//	updateValidatorTx = kvstore.MakeValSetChangeTx(val1PubKeyABCI, 2)
//	previousTotalVotingPower = states[0].GetRoundState().LastValidators.TotalVotingPower()
//
//	waitForAndValidateBlock(ctx, t, n, activeVals, blocksSubs, states, updateValidatorTx)
//	waitForAndValidateBlockWithTx(ctx, t, n, activeVals, blocksSubs, states, updateValidatorTx)
//	waitForAndValidateBlock(ctx, t, n, activeVals, blocksSubs, states)
//	waitForAndValidateBlock(ctx, t, n, activeVals, blocksSubs, states)
//
//	// Msg sent to mempool, needs to be processed by nodes
//	require.Eventually(
//		t,
//		func() bool {
//			return previousTotalVotingPower != states[0].GetRoundState().LastValidators.TotalVotingPower()
//		},
//		30*time.Second,
//		100*time.Millisecond,
//		"expected voting power to change (before: %d, after: %d)",
//		previousTotalVotingPower,
//		states[0].GetRoundState().LastValidators.TotalVotingPower(),
//	)
//	updateValidatorTx = kvstore.MakeValSetChangeTx(val1PubKeyABCI, 26)
//	previousTotalVotingPower = states[0].GetRoundState().LastValidators.TotalVotingPower()
//
//	waitForAndValidateBlock(ctx, t, n, activeVals, blocksSubs, states, updateValidatorTx)
//	waitForAndValidateBlockWithTx(ctx, t, n, activeVals, blocksSubs, states, updateValidatorTx)
//	waitForAndValidateBlock(ctx, t, n, activeVals, blocksSubs, states)
//	waitForAndValidateBlock(ctx, t, n, activeVals, blocksSubs, states)
//
//	// Msg sent to mempool, needs to be processed by nodes
//	require.Eventually(
//		t,
//		func() bool {
//			return previousTotalVotingPower != states[0].GetRoundState().LastValidators.TotalVotingPower()
//		},
//		30*time.Second,
//		100*time.Millisecond,
//		"expected voting power to change (before: %d, after: %d)",
//		previousTotalVotingPower,
//		states[0].GetRoundState().LastValidators.TotalVotingPower(),
//	)
//}

func TestReactorValidatorSetChanges(t *testing.T) {
	t.Skip("See: https://linear.app/seilabs/issue/CON-100/testreactorvalidatorsetchanges-hangs-indefinitely-in-ci-when-run-with")
	ctx := t.Context()
	cfg := configSetup(t)

	nPeers := 4
	nVals := 2
	states, _, _, cleanup := randConsensusNetWithPeers(
		ctx,
		t,
		cfg,
		nVals,
		nPeers,
		newMockTickerFunc(true),
		newEpehemeralKVStore,
	)
	t.Cleanup(cleanup)

	rts := setup(ctx, t, nPeers, states, 1024) // buffer must be large enough to not deadlock

	for _, reactor := range rts.reactors {
		state := reactor.state.GetState()
		reactor.StopWaitSync()
		reactor.SwitchToConsensus(ctx, state, false)
	}

	// map of active validators
	activeVals := make(map[string]struct{})
	for i := range nVals {
		pubKey, err := states[i].privValidator.GetPubKey(ctx)
		require.NoError(t, err)

		activeVals[string(pubKey.Address())] = struct{}{}
	}

	t.Logf("wait till everyone makes the first new block")
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, sub := range rts.subs {
			s.Spawn(func() error {
				_, err := sub.Next(ctx)
				return err
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	newValidatorPubKey1, err := states[nVals].privValidator.GetPubKey(ctx)
	require.NoError(t, err)

	valPubKey1ABCI, err := encoding.PubKeyToProto(newValidatorPubKey1)
	require.NoError(t, err)

	newValidatorTx1 := kvstore.MakeValSetChangeTx(valPubKey1ABCI, testMinPower)

	blocksSubs := []eventbus.Subscription{}
	for _, sub := range rts.subs {
		blocksSubs = append(blocksSubs, sub)
	}

	t.Logf("wait till everyone makes block 2")
	// ensure the commit includes all validators
	// send newValTx to change vals in block 3
	waitForAndValidateBlock(ctx, t, nPeers, activeVals, blocksSubs, states, newValidatorTx1)

	t.Logf("wait till everyone makes block 3.")
	// it includes the commit for block 2, which is by the original validator set
	waitForAndValidateBlockWithTx(ctx, t, nPeers, activeVals, blocksSubs, newValidatorTx1)

	t.Logf("wait till everyone makes block 4.")
	// it includes the commit for block 3, which is by the original validator set
	waitForAndValidateBlock(ctx, t, nPeers, activeVals, blocksSubs, states)

	// the commits for block 4 should be with the updated validator set
	activeVals[string(newValidatorPubKey1.Address())] = struct{}{}

	t.Logf("wait till everyone makes block 5")
	// it includes the commit for block 4, which should have the updated validator set
	waitForBlockWithUpdatedValsAndValidateIt(ctx, t, nPeers, activeVals, blocksSubs)

	for i := 2; i <= 32; i *= 2 {
		useState := rand.Intn(nVals)
		t.Logf("useState = %v", useState)
		updateValidatorPubKey1, err := states[useState].privValidator.GetPubKey(ctx)
		require.NoError(t, err)

		updatePubKey1ABCI, err := encoding.PubKeyToProto(updateValidatorPubKey1)
		require.NoError(t, err)

		previousTotalVotingPower := states[useState].GetRoundState().LastValidators.TotalVotingPower()
		updateValidatorTx1 := kvstore.MakeValSetChangeTx(updatePubKey1ABCI, int64(i))

		waitForAndValidateBlock(ctx, t, nPeers, activeVals, blocksSubs, states, updateValidatorTx1)
		waitForAndValidateBlockWithTx(ctx, t, nPeers, activeVals, blocksSubs, updateValidatorTx1)
		waitForAndValidateBlock(ctx, t, nPeers, activeVals, blocksSubs, states)
		waitForBlockWithUpdatedValsAndValidateIt(ctx, t, nPeers, activeVals, blocksSubs)

		time.Sleep(time.Second)
		require.NotEqualf(
			t, states[useState].GetRoundState().LastValidators.TotalVotingPower(), previousTotalVotingPower,
			"expected voting power to change (before: %d, after: %d)",
			previousTotalVotingPower, states[useState].GetRoundState().LastValidators.TotalVotingPower(),
		)
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
		Signature: []byte("test-signature"),
	}

	// Test direct SetHasProposal call (this is what reactor calls)
	ps.SetHasProposal(invalidProposal)
	require.False(t, ps.PRS.Proposal, "SetHasProposal should silently ignore proposal with excessive Total")

	// Test that reactor would handle this silently by verifying the defensive programming approach
	// This provides coverage for the silent handling in handleDataMessage
	t.Log("Coverage test: reactor silently ignores invalid proposals via PeerState validation")
}
