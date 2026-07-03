package consensus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	cstypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	tmpubsub "github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	tmos "github.com/sei-protocol/sei-chain/sei-tendermint/libs/os"
	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func mustGenesisChainID(cfg *config.Config) string {
	genDoc, err := types.GenesisDocFromFile(cfg.GenesisFile())
	if err != nil {
		panic(err)
	}
	return genDoc.ChainID
}

const (
	testSubscriber = "test-client"

	// genesis, chain_id, priv_val
	ensureTimeout = 5 * time.Second
)

// A cleanupFunc cleans up any config / test files created for a particular
// test.
type cleanupFunc func()

type testState struct {
	*State
	testRoutines sync.WaitGroup
}

func (cs *testState) startRoutines(ctx context.Context, maxSteps int) {
	cs.testRoutines.Go(func() {
		if err := cs.timeoutTicker.Run(ctx); err != nil {
			logger.Error("cs.timeoutTicker.Run()", "err", err)
		}
	})
	cs.testRoutines.Go(func() { _ = cs.receiveRoutine(ctx, maxSteps) })
}

func (cs *testState) waitForTestRoutines() {
	cs.testRoutines.Wait()
}

func (cs *testState) address(ctx context.Context) types.Address {
	pv, ok := cs.privValidator.Get()
	if !ok {
		panic("privValidator not set")
	}
	pubKey, err := pv.GetPubKey(ctx)
	if err != nil {
		panic(fmt.Errorf("pv.GetPubKey(): %w", err))
	}
	return pubKey.Address()
}

func unwrapTestStates(css []*testState) []*State {
	states := make([]*State, len(css))
	for i, cs := range css {
		states[i] = cs.State
	}
	return states
}

func configSetup(t *testing.T) *config.Config {
	t.Helper()

	cfg, err := ResetConfig(t.TempDir(), "consensus_reactor_test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(cfg.RootDir) })

	consensusReplayConfig, err := ResetConfig(t.TempDir(), "consensus_replay_test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(consensusReplayConfig.RootDir) })

	configStateTest, err := ResetConfig(t.TempDir(), "consensus_state_test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(configStateTest.RootDir) })

	configMempoolTest, err := ResetConfig(t.TempDir(), "consensus_mempool_test")
	configMempoolTest.Mempool.DuplicateTxsCacheSize = 0
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(configMempoolTest.RootDir) })

	configByzantineTest, err := ResetConfig(t.TempDir(), "consensus_byzantine_test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(configByzantineTest.RootDir) })

	walDir := filepath.Dir(cfg.Consensus.WalFile())
	ensureDir(walDir, 0700)

	return cfg
}

func ensureDir(dir string, mode os.FileMode) {
	if err := tmos.EnsureDir(dir, mode); err != nil {
		panic(fmt.Errorf("tmos.EnsureDir(%s,%v): %w", dir, mode, err))
	}
}

func ResetConfig(dir, name string) (*config.Config, error) {
	return config.ResetTestRoot(dir, name)
}

//-------------------------------------------------------------------------------
// validator stub (a kvstore consensus peer we control)

type validatorStub struct {
	Index  int32 // Validator index. NOTE: we don't assume validator set changes.
	Height int64
	Round  int32
	clock  tmtime.Source
	types.PrivValidator
	VotingPower int64
	lastVote    *types.Vote
}

const testMinPower int64 = 10

func newValidatorStub(privValidator types.PrivValidator, valIndex int32) *validatorStub {
	return &validatorStub{
		Index:         valIndex,
		PrivValidator: privValidator,
		VotingPower:   testMinPower,
		clock:         tmtime.DefaultSource{},
	}
}

func (vs *validatorStub) signVote(
	ctx context.Context,
	voteType tmproto.SignedMsgType,
	chainID string,
	blockID types.BlockID,
) (*types.Vote, error) {
	pubKey, err := vs.PrivValidator.GetPubKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("can't get pubkey: %w", err)
	}

	vote := &types.Vote{
		Type:             voteType,
		Height:           vs.Height,
		Round:            vs.Round,
		BlockID:          blockID,
		Timestamp:        vs.clock.Now(),
		ValidatorAddress: pubKey.Address(),
		ValidatorIndex:   vs.Index,
	}
	v := vote.ToProto()
	if err = vs.PrivValidator.SignVote(ctx, chainID, v); err != nil {
		return nil, fmt.Errorf("sign vote failed: %w", err)
	}

	// ref: signVote in FilePV, the vote should use the previous vote info when the sign data is the same.
	if signDataIsEqual(vs.lastVote, v) {
		sig, ok := vs.lastVote.Signature.Get()
		if !ok {
			panic("last vote missing signature")
		}
		v.Signature = sig.Bytes()
		v.Timestamp = vs.lastVote.Timestamp
	}
	vote.Signature = utils.Some(utils.OrPanic1(crypto.SigFromBytes(v.Signature)))
	vote.Timestamp = v.Timestamp
	return vote, nil
}

// Sign vote for type/hash/header
func signVote(
	ctx context.Context,
	t *testing.T,
	vs *validatorStub,
	voteType tmproto.SignedMsgType,
	chainID string,
	blockID types.BlockID,
) *types.Vote {
	v, err := vs.signVote(ctx, voteType, chainID, blockID)
	require.NoError(t, err, "failed to sign vote")
	vs.lastVote = v
	return v
}

func signVotes(
	ctx context.Context,
	t *testing.T,
	voteType tmproto.SignedMsgType,
	chainID string,
	blockID types.BlockID,
	vss ...*validatorStub,
) []*types.Vote {
	votes := make([]*types.Vote, len(vss))
	for i, vs := range vss {
		votes[i] = signVote(ctx, t, vs, voteType, chainID, blockID)
	}
	return votes
}

func incrementHeight(vss ...*validatorStub) {
	for _, vs := range vss {
		vs.Height++
	}
}

func incrementRound(vss ...*validatorStub) {
	for _, vs := range vss {
		vs.Round++
	}
}

type ValidatorStubsByPower []*validatorStub

func (vss ValidatorStubsByPower) Len() int {
	return len(vss)
}

func sortVValidatorStubsByPower(ctx context.Context, t *testing.T, vss []*validatorStub) []*validatorStub {
	t.Helper()
	sort.Slice(vss, func(i, j int) bool {
		vssi, err := vss[i].GetPubKey(ctx)
		require.NoError(t, err)

		vssj, err := vss[j].GetPubKey(ctx)
		require.NoError(t, err)

		if vss[i].VotingPower == vss[j].VotingPower {
			return bytes.Compare(vssi.Address(), vssj.Address()) == -1
		}
		return vss[i].VotingPower > vss[j].VotingPower
	})

	for idx, vs := range vss {
		vs.Index = int32(idx)
	}

	return vss
}

//-------------------------------------------------------------------------------
// Functions for transitioning the consensus state

func (cs *testState) startTestRound(ctx context.Context, height int64, round int32) {
	cs.enterNewRound(ctx, height, round, "")
	cs.startRoutines(ctx, 0)
}

// Create proposal block from cs1 but sign it with vs.
func (cs *testState) decideProposal(
	ctx context.Context,
	t *testing.T,
	vs *validatorStub,
	height int64,
	round int32,
) (proposal *types.Proposal, block *types.Block) {
	t.Helper()

	cs.mtx.Lock()
	block, err := cs.createProposalBlock(ctx)
	require.NoError(t, err)
	blockParts, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	validRound := cs.roundState.ValidRound()
	chainID := cs.state.ChainID
	cs.mtx.Unlock()

	require.NotNil(t, block, "Failed to createProposalBlock. Did you forget to add commit for previous block?")

	// Make proposal
	pubKey, err := vs.PrivValidator.GetPubKey(ctx)
	require.NoError(t, err)

	address := pubKey.Address()
	polRound, propBlockID := validRound, types.BlockID{Hash: block.Hash(), PartSetHeader: blockParts.Header()}
	proposal = types.NewProposal(height, round, polRound, propBlockID, block.Header.Time, block.GetTxHashes(), block.Header, block.LastCommit, block.Evidence, address)
	p := proposal.ToProto()
	require.NoError(t, vs.SignProposal(ctx, chainID, p))
	proposal.Signature = utils.OrPanic1(crypto.SigFromBytes(p.Signature))
	return
}

func (cs *testState) addVotes(votes ...*types.Vote) {
	for _, vote := range votes {
		cs.peerMsgQueue <- msgInfo{Msg: &VoteMessage{vote}}
	}
}

func (cs *testState) signAddVotes(
	ctx context.Context,
	t *testing.T,
	voteType tmproto.SignedMsgType,
	chainID string,
	blockID types.BlockID,
	vss ...*validatorStub,
) {
	cs.addVotes(signVotes(ctx, t, voteType, chainID, blockID, vss...)...)
}

func (cs *testState) validatePrevote(
	ctx context.Context,
	t *testing.T,
	round int32,
	privVal *validatorStub,
	blockHash []byte,
) {
	t.Helper()

	cs.mtx.RLock()
	defer cs.mtx.RUnlock()

	prevotes := cs.roundState.Votes().Prevotes(round)
	pubKey, err := privVal.GetPubKey(ctx)
	require.NoError(t, err)

	address := pubKey.Address()

	vote, ok := prevotes.GetByAddress(address)
	require.True(t, ok, "Failed to find prevote from validator")

	if blockHash == nil {
		require.Nil(t, vote.BlockID.Hash, "Expected prevote to be for nil, got %X", vote.BlockID.Hash)
	} else {
		require.True(t, bytes.Equal(vote.BlockID.Hash, blockHash), "Expected prevote to be for %X, got %X", blockHash, vote.BlockID.Hash)
	}
}

func (cs *testState) validateLastPrecommit(ctx context.Context, t *testing.T, privVal *validatorStub, blockHash []byte) {
	t.Helper()

	votes := cs.roundState.LastCommit()
	pv, err := privVal.GetPubKey(ctx)
	require.NoError(t, err)
	address := pv.Address()

	vote, ok := votes.GetByAddress(address)
	require.True(t, ok)

	require.True(t, bytes.Equal(vote.BlockID.Hash, blockHash),
		"Expected precommit to be for %X, got %X", blockHash, vote.BlockID.Hash)
}

func (cs *testState) validatePrecommit(
	ctx context.Context,
	t *testing.T,
	thisRound,
	lockRound int32,
	privVal *validatorStub,
	votedBlockHash,
	lockedBlockHash []byte,
) {
	t.Helper()

	precommits := cs.roundState.Votes().Precommits(thisRound)
	pv, err := privVal.GetPubKey(ctx)
	require.NoError(t, err)
	address := pv.Address()

	vote, ok := precommits.GetByAddress(address)
	require.True(t, ok, "Failed to find precommit from validator")

	if votedBlockHash == nil {
		require.Nil(t, vote.BlockID.Hash, "Expected precommit to be for nil")
	} else {
		require.True(t, bytes.Equal(vote.BlockID.Hash, votedBlockHash), "Expected precommit to be for proposal block")
	}

	rs := cs.GetRoundState()
	if lockedBlockHash == nil {
		require.False(t, rs.LockedRound != lockRound || rs.LockedBlock != nil,
			"Expected to be locked on nil at round %d. Got locked at round %d with block %v",
			lockRound,
			rs.LockedRound,
			rs.LockedBlock)
	} else {
		require.False(t, rs.LockedRound != lockRound || !bytes.Equal(rs.LockedBlock.Hash(), lockedBlockHash),
			"Expected block to be locked on round %d, got %d. Got locked block %X, expected %X",
			lockRound,
			rs.LockedRound,
			rs.LockedBlock.Hash(),
			lockedBlockHash)
	}
}

func (cs *testState) subscribeToVoter(ctx context.Context, t *testing.T, addr []byte) <-chan tmpubsub.Message {
	t.Helper()

	ch := make(chan tmpubsub.Message, 1)
	if err := cs.eventBus.Observe(ctx, func(msg tmpubsub.Message) error {
		vote := msg.Data().(types.EventDataVote)
		// we only fire for our own votes
		if bytes.Equal(addr, vote.Vote.ValidatorAddress) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ch <- msg:
			}
		}
		return nil
	}, types.EventQueryVote); err != nil {
		t.Fatalf("Failed to observe query %v: %v", types.EventQueryVote, err)
	}
	return ch
}

func (cs *testState) subscribeToVoterBuffered(ctx context.Context, t *testing.T, addr []byte) <-chan tmpubsub.Message {
	t.Helper()
	votesSub, err := cs.eventBus.SubscribeWithArgs(ctx, tmpubsub.SubscribeArgs{
		ClientID: testSubscriber,
		Query:    types.EventQueryVote,
		Limit:    10})
	if err != nil {
		t.Fatalf("failed to subscribe %s to %v", testSubscriber, types.EventQueryVote)
	}
	ch := make(chan tmpubsub.Message, 10)
	go func() {
		for {
			msg, err := votesSub.Next(ctx)
			if err != nil {
				if !errors.Is(err, tmpubsub.ErrTerminated) && !errors.Is(err, context.Canceled) {
					t.Errorf("error terminating pubsub %s", err)
				}
				return
			}
			vote := msg.Data().(types.EventDataVote)
			// we only fire for our own votes
			if bytes.Equal(addr, vote.Vote.ValidatorAddress) {
				select {
				case <-ctx.Done():
				case ch <- msg:
				}
			}
		}
	}()
	return ch
}

//-------------------------------------------------------------------------------
// consensus states

func newState(
	t *testing.T,
	state sm.State,
	pv types.PrivValidator,
	app *proxy.Proxy,
) *testState {
	t.Helper()

	cfg, err := config.ResetTestRoot(t.TempDir(), "consensus_state_test")
	require.NoError(t, err)

	return newStateWithConfig(t, cfg, state, pv, app)
}

func newStateWithConfig(
	t *testing.T,
	thisConfig *config.Config,
	state sm.State,
	pv types.PrivValidator,
	app *proxy.Proxy,
) *testState {
	return newStateWithConfigAndBlockStore(t, thisConfig, state, pv, app, store.NewBlockStore(dbm.NewMemDB()))
}

func newStateWithConfigAndBlockStore(
	t *testing.T,
	thisConfig *config.Config,
	state sm.State,
	pv types.PrivValidator,
	app *proxy.Proxy,
	blockStore *store.BlockStore,
) *testState {
	t.Helper()
	ctx := t.Context()

	// Make Mempool

	mempool := mempool.NewTxMempool(
		thisConfig.Mempool.ToMempoolConfig(),
		app,
		mempool.NewMetrics(),
		mempool.NopTxConstraintsFetcher,
	)

	evpool := sm.EmptyEvidencePool{}

	// Make State
	stateDB := dbm.NewMemDB()
	stateStore := sm.NewStore(stateDB)
	if err := stateStore.Save(state); err != nil {
		panic(fmt.Errorf("stateStore.Save(): %w", err))
	}

	eventBus := eventbus.NewDefault()
	if err := eventBus.Start(ctx); err != nil {
		panic(fmt.Errorf("eventBus.Start(): %w", err))
	}

	blockExec := sm.NewBlockExecutor(stateStore, app, mempool, evpool, blockStore, eventBus, sm.NewMetrics(), types.DefaultConsensusPolicy())
	wal, err := OpenWAL(thisConfig.Consensus.WalFile())
	if err != nil {
		panic(err)
	}
	stateHandle := &testState{State: NewState(
		thisConfig.Consensus,
		wal,
		stateStore,
		blockExec,
		blockStore,
		mempool,
		evpool,
		eventBus,
		[]trace.TracerProviderOption{},
		NewMetrics(),
	)}
	if err := stateHandle.updateStateFromStore(); err != nil {
		panic(err)
	}

	stateHandle.SetPrivValidator(ctx, utils.Some(pv))
	t.Cleanup(func() {
		stateHandle.waitForTestRoutines()
		eventBus.Wait()
		wal.Close()
	})

	return stateHandle
}

func loadPrivValidator(cfg *config.Config) *privval.FilePV {
	privValidatorKeyFile := cfg.PrivValidator.KeyFile()
	ensureDir(filepath.Dir(privValidatorKeyFile), 0700)
	privValidatorStateFile := cfg.PrivValidator.StateFile()
	privValidator, err := privval.LoadOrGenFilePV(privValidatorKeyFile, privValidatorStateFile)
	if err != nil {
		panic(fmt.Errorf("privval.LoadOrGenFilePV(): %w", err))
	}
	if err := privValidator.Reset(); err != nil {
		panic(fmt.Errorf("privValidator.Reset(): %w", err))
	}
	return privValidator
}

type makeStateArgs struct {
	config          *config.Config
	consensusParams *types.ConsensusParams
	validators      int
	application     *proxy.Proxy
	nonLeaderLocal  bool
}

func makeState(ctx context.Context, t *testing.T, args makeStateArgs) (*testState, []*validatorStub) {
	t.Helper()
	// Get State
	validators := 4
	if args.validators != 0 {
		validators = args.validators
	}
	app := kvstore.NewProxy()
	if args.application != nil {
		app = args.application
	}
	if args.config == nil {
		args.config = configSetup(t)
	}
	c := factory.ConsensusParams()
	if args.consensusParams != nil {
		c = args.consensusParams
	}

	state, privVals := makeGenesisState(ctx, t, args.config, genesisStateArgs{
		Params:     c,
		Validators: validators,
	})

	vss := make([]*validatorStub, validators)
	localIndex := 0
	if args.nonLeaderLocal {
		rs := &cstypes.RoundState{
			HRS: cstypes.HRS{
				Height: 1,
				Round:  0,
			},
			Validators: state.Validators.Copy(),
		}
		leaderAddr := rs.Leader().Address()
		found := false
		for i, pv := range privVals {
			pubKey, err := pv.GetPubKey(ctx)
			require.NoError(t, err)
			if !bytes.Equal(pubKey.Address(), leaderAddr) {
				localIndex = i
				found = true
				break
			}
		}
		if !found {
			t.Fatal("expected at least one non-leader validator")
		}
	}

	cs := newStateWithConfig(t, args.config, state, privVals[localIndex], app)

	for i := 0; i < validators; i++ {
		vss[i] = newValidatorStub(privVals[i], int32(i))
	}
	for i, vs := range vss {
		if i == localIndex {
			continue
		}
		vs.Height++
	}

	return cs, vss
}

func validatorStubByAddress(ctx context.Context, t *testing.T, vss []*validatorStub, addr []byte) *validatorStub {
	t.Helper()

	for _, vs := range vss {
		pubKey, err := vs.GetPubKey(ctx)
		require.NoError(t, err)
		if bytes.Equal(pubKey.Address(), addr) {
			return vs
		}
	}

	t.Fatalf("failed to find validator stub for address %X", addr)
	return nil
}

func (cs *testState) leaderAddressAtRound(height int64, round int32) []byte {
	rs := cs.GetRoundState()
	return (&cstypes.RoundState{
		HRS: cstypes.HRS{
			Height: height,
			Round:  round,
		},
		Validators: rs.Validators.Copy(),
	}).Leader().Address()
}

func (cs *testState) leaderValidatorStubAtRound(ctx context.Context, t *testing.T, vss []*validatorStub, height int64, round int32) *validatorStub {
	t.Helper()
	return validatorStubByAddress(ctx, t, vss, cs.leaderAddressAtRound(height, round))
}

func (cs *testState) nextRoundWithLeaderAddr(
	height int64,
	startRound int32,
	matches func([]byte) bool,
	maxLookahead int,
) int32 {
	for r := startRound; r < startRound+int32(maxLookahead); r++ {
		if matches(cs.leaderAddressAtRound(height, r)) {
			return r
		}
	}

	return -1
}

func (cs *testState) nextRoundForLocalLeader(ctx context.Context, t *testing.T, height int64, startRound int32, maxLookahead int) int32 {
	t.Helper()

	localAddr := cs.address(ctx)
	round := cs.nextRoundWithLeaderAddr(height, startRound, func(addr []byte) bool {
		return bytes.Equal(addr, localAddr)
	}, maxLookahead)
	require.NotEqual(t, int32(-1), round, "failed to find a local leader round")
	return round
}

func (cs *testState) nextRoundForNonLocalLeader(ctx context.Context, t *testing.T, height int64, startRound int32, maxLookahead int) int32 {
	t.Helper()

	localAddr := cs.address(ctx)
	round := cs.nextRoundWithLeaderAddr(height, startRound, func(addr []byte) bool {
		return !bytes.Equal(addr, localAddr)
	}, maxLookahead)
	require.NotEqual(t, int32(-1), round, "failed to find a non-local leader round")
	return round
}

func (cs *testState) findStartRoundForLocalLeaderPattern(
	ctx context.Context,
	t *testing.T,
	height int64,
	startRound int32,
	pattern []bool,
	maxLookahead int,
) int32 {
	t.Helper()

	localAddr := cs.address(ctx)
	for candidate := startRound; candidate < startRound+int32(maxLookahead); candidate++ {
		matches := true
		for offset, wantLocalLeader := range pattern {
			isLocalLeader := bytes.Equal(cs.leaderAddressAtRound(height, candidate+int32(offset)), localAddr)
			if isLocalLeader != wantLocalLeader {
				matches = false
				break
			}
		}
		if matches {
			return candidate
		}
	}

	t.Fatalf("failed to find leader pattern %v", pattern)
	return -1
}

func incrementRoundTo(targetRound int32, vss ...*validatorStub) {
	if len(vss) == 0 {
		return
	}

	delta := targetRound - vss[0].Round
	for i := int32(0); i < delta; i++ {
		incrementRound(vss...)
	}
}

//-------------------------------------------------------------------------------

func ensureNoMessageBeforeTimeout(t *testing.T, ch <-chan tmpubsub.Message, timeout time.Duration,
	errorMessage string) {
	t.Helper()
	select {
	case <-time.After(timeout):
		break
	case <-ch:
		t.Fatal(errorMessage)
	}
}

func ensureNoNewEventOnChannel(t *testing.T, ch <-chan tmpubsub.Message) {
	t.Helper()
	ensureNoMessageBeforeTimeout(
		t,
		ch,
		ensureTimeout,
		"We should be stuck waiting, not receiving new event on the channel")
}

func ensureNoNewRoundStep(t *testing.T, stepCh <-chan tmpubsub.Message) {
	t.Helper()
	ensureNoMessageBeforeTimeout(
		t,
		stepCh,
		ensureTimeout,
		"We should be stuck waiting, not receiving NewRoundStep event")
}

func ensureNoNewTimeout(t *testing.T, stepCh <-chan tmpubsub.Message, timeout int64) {
	t.Helper()
	timeoutDuration := time.Duration(timeout*10) * time.Nanosecond
	ensureNoMessageBeforeTimeout(
		t,
		stepCh,
		timeoutDuration,
		"We should be stuck waiting, not receiving NewTimeout event")
}

func ensureNewEvent(t *testing.T, ch <-chan tmpubsub.Message, height int64, round int32) {
	t.Helper()
	msg := ensureMessageBeforeTimeout(t, ch, ensureTimeout)
	roundStateEvent, ok := msg.Data().(types.EventDataRoundState)
	require.True(t, ok,
		"expected a EventDataRoundState, got %T. Wrong subscription channel?",
		msg.Data())

	require.Equal(t, height, roundStateEvent.Height)
	require.Equal(t, round, roundStateEvent.Round)
	// TODO: We could check also for a step at this point!
}

func ensureNewRound(t *testing.T, roundCh <-chan tmpubsub.Message, height int64, round int32) {
	t.Helper()
	msg := ensureMessageBeforeTimeout(t, roundCh, ensureTimeout)
	newRoundEvent, ok := msg.Data().(types.EventDataNewRound)
	require.True(t, ok, "expected a EventDataNewRound, got %T. Wrong subscription channel?",
		msg.Data())

	require.Equal(t, height, newRoundEvent.Height)
	require.Equal(t, round, newRoundEvent.Round)
}

func ensureNewTimeout(t *testing.T, timeoutCh <-chan tmpubsub.Message, height int64, round int32) {
	t.Helper()
	ensureNewEvent(t, timeoutCh, height, round)
}

func ensureNewProposal(t *testing.T, proposalCh <-chan tmpubsub.Message, height int64, round int32) types.BlockID {
	t.Helper()
	msg := ensureMessageBeforeTimeout(t, proposalCh, ensureTimeout)
	proposalEvent, ok := msg.Data().(types.EventDataCompleteProposal)
	require.True(t, ok, "expected a EventDataCompleteProposal, got %T. Wrong subscription channel?",
		msg.Data())
	require.Equal(t, height, proposalEvent.Height)
	require.Equal(t, round, proposalEvent.Round)
	return proposalEvent.BlockID
}

func ensureNewValidBlock(t *testing.T, validBlockCh <-chan tmpubsub.Message, height int64, round int32) {
	t.Helper()
	ensureNewEvent(t, validBlockCh, height, round)
}

func ensureNewBlock(t *testing.T, blockCh <-chan tmpubsub.Message, height int64) {
	t.Helper()
	msg := ensureMessageBeforeTimeout(t, blockCh, ensureTimeout)
	blockEvent, ok := msg.Data().(types.EventDataNewBlock)
	require.True(t, ok, "expected a EventDataNewBlock, got %T. Wrong subscription channel?",
		msg.Data())
	require.Equal(t, height, blockEvent.Block.Height)
}

func ensureNewBlockHeader(t *testing.T, blockCh <-chan tmpubsub.Message, height int64, blockHash tmbytes.HexBytes) {
	t.Helper()
	msg := ensureMessageBeforeTimeout(t, blockCh, ensureTimeout)
	blockHeaderEvent, ok := msg.Data().(types.EventDataNewBlockHeader)
	require.True(t, ok, "expected a EventDataNewBlockHeader, got %T. Wrong subscription channel?",
		msg.Data())

	require.Equal(t, height, blockHeaderEvent.Header.Height)
	require.True(t, bytes.Equal(blockHeaderEvent.Header.Hash(), blockHash))
}

func ensureLock(t *testing.T, lockCh <-chan tmpubsub.Message, height int64, round int32) {
	t.Helper()
	ensureNewEvent(t, lockCh, height, round)
}

func ensureRelock(t *testing.T, relockCh <-chan tmpubsub.Message, height int64, round int32) {
	t.Helper()
	ensureNewEvent(t, relockCh, height, round)
}

func ensureProposal(t *testing.T, proposalCh <-chan tmpubsub.Message, height int64, round int32, propID types.BlockID) {
	ensureProposalWithTimeout(t, proposalCh, height, round, &propID, ensureTimeout)
}

func ensureProposalWithTimeout(t *testing.T, proposalCh <-chan tmpubsub.Message, height int64, round int32, propID *types.BlockID, timeout time.Duration) {
	t.Helper()
	msg := ensureMessageBeforeTimeout(t, proposalCh, timeout)
	proposalEvent, ok := msg.Data().(types.EventDataCompleteProposal)
	require.True(t, ok, "expected a EventDataCompleteProposal, got %T. Wrong subscription channel?",
		msg.Data())
	require.Equal(t, height, proposalEvent.Height)
	require.Equal(t, round, proposalEvent.Round)
	if propID != nil {
		require.True(t, proposalEvent.BlockID.Equals(*propID),
			"Proposed block does not match expected block (%v != %v)", proposalEvent.BlockID, propID)
	}
}

func ensurePrecommit(t *testing.T, voteCh <-chan tmpubsub.Message, height int64, round int32) {
	t.Helper()
	ensureVote(t, voteCh, height, round, tmproto.PrecommitType)
}

func ensurePrevote(t *testing.T, voteCh <-chan tmpubsub.Message, height int64, round int32) {
	t.Helper()
	ensureVote(t, voteCh, height, round, tmproto.PrevoteType)
}

func ensurePrevoteMatch(t *testing.T, voteCh <-chan tmpubsub.Message, height int64, round int32, hash []byte) {
	t.Helper()
	ensureVoteMatch(t, voteCh, height, round, hash, tmproto.PrevoteType)
}

func ensurePrecommitMatch(t *testing.T, voteCh <-chan tmpubsub.Message, height int64, round int32, hash []byte) {
	t.Helper()
	ensureVoteMatch(t, voteCh, height, round, hash, tmproto.PrecommitType)
}

func ensureVoteMatch(t *testing.T, voteCh <-chan tmpubsub.Message, height int64, round int32, hash []byte, voteType tmproto.SignedMsgType) {
	t.Helper()
	select {
	case <-time.After(ensureTimeout):
		t.Fatal("Timeout expired while waiting for NewVote event")
	case msg := <-voteCh:
		voteEvent, ok := msg.Data().(types.EventDataVote)
		require.True(t, ok, "expected a EventDataVote, got %T. Wrong subscription channel?",
			msg.Data())

		vote := voteEvent.Vote
		assert.Equal(t, height, vote.Height, "expected height %d, but got %d", height, vote.Height)
		assert.Equal(t, round, vote.Round, "expected round %d, but got %d", round, vote.Round)
		assert.Equal(t, voteType, vote.Type, "expected type %s, but got %s", voteType, vote.Type)
		if hash == nil {
			require.Nil(t, vote.BlockID.Hash, "Expected prevote to be for nil, got %X", vote.BlockID.Hash)
		} else {
			require.True(t, bytes.Equal(vote.BlockID.Hash, hash), "Expected prevote to be for %X, got %X", hash, vote.BlockID.Hash)
		}
	}
}

func ensureVote(t *testing.T, voteCh <-chan tmpubsub.Message, height int64, round int32, voteType tmproto.SignedMsgType) {
	t.Helper()
	msg := ensureMessageBeforeTimeout(t, voteCh, ensureTimeout)
	voteEvent, ok := msg.Data().(types.EventDataVote)
	require.True(t, ok, "expected a EventDataVote, got %T. Wrong subscription channel?",
		msg.Data())

	vote := voteEvent.Vote
	require.Equal(t, height, vote.Height, "expected height %d, but got %d", height, vote.Height)
	require.Equal(t, round, vote.Round, "expected round %d, but got %d", round, vote.Round)
	require.Equal(t, voteType, vote.Type, "expected type %s, but got %s", voteType, vote.Type)
}

func ensureNewEventOnChannel(t *testing.T, ch <-chan tmpubsub.Message) {
	t.Helper()
	ensureMessageBeforeTimeout(t, ch, ensureTimeout)
}

func ensureMessageBeforeTimeout(t *testing.T, ch <-chan tmpubsub.Message, to time.Duration) tmpubsub.Message {
	t.Helper()
	select {
	case <-time.After(to):
		t.Fatalf("Timeout expired while waiting for message")
	case msg := <-ch:
		return msg
	}
	panic("unreachable")
}

func makeConsensusState(
	ctx context.Context,
	t *testing.T,
	cfg *config.Config,
	nValidators int,
	testName string,
	tickerFunc func() TimeoutTicker,
	configOpts ...func(*config.Config),
) ([]*testState, cleanupFunc) {
	t.Helper()
	tempDir := t.TempDir()

	valSet, privVals := factory.ValidatorSet(ctx, nValidators, 30)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	css := make([]*testState, nValidators)

	closeFuncs := make([]func() error, 0, nValidators)

	configRootDirs := make([]string, 0, nValidators)

	for i := range nValidators {
		blockStore := store.NewBlockStore(dbm.NewMemDB()) // each state needs its own db
		state, err := sm.MakeGenesisState(genDoc)
		require.NoError(t, err)
		thisConfig, err := ResetConfig(tempDir, fmt.Sprintf("%s_%d", testName, i))
		require.NoError(t, err)

		configRootDirs = append(configRootDirs, thisConfig.RootDir)

		for _, opt := range configOpts {
			opt(thisConfig)
		}

		walDir := filepath.Dir(thisConfig.Consensus.WalFile())
		ensureDir(walDir, 0700)

		app := kvstore.NewApplication()
		closeFuncs = append(closeFuncs, app.Close)

		vals := types.TM2PB.ValidatorUpdates(state.Validators)
		_, err = app.InitChain(ctx, &abci.RequestInitChain{})
		require.NoError(t, err)
		app.SetValidators(vals)

		proxyApp := proxy.New(app, proxy.NewMetrics())
		css[i] = newStateWithConfigAndBlockStore(t, thisConfig, state, privVals[i], proxyApp, blockStore)
		css[i].SetTimeoutTicker(tickerFunc())
	}

	return css, func() {
		for _, closer := range closeFuncs {
			_ = closer()
		}
		for _, dir := range configRootDirs {
			os.RemoveAll(dir)
		}
	}
}

// nPeers = nValidators + nNotValidator
func randConsensusNetWithPeers(
	ctx context.Context,
	t *testing.T,
	cfg *config.Config,
	nValidators int,
	nPeers int,
	tickerFunc func() TimeoutTicker,
) ([]*testState, *types.GenesisDoc, *config.Config, cleanupFunc) {
	t.Helper()

	valSet, privVals := factory.ValidatorSet(ctx, nValidators, testMinPower)
	genDoc := factory.GenesisDoc(cfg, time.Now(), valSet.Validators, factory.ConsensusParams())
	css := make([]*testState, nPeers)

	var peer0Config *config.Config
	configRootDirs := make([]string, 0, nPeers)
	testName := strings.ReplaceAll(t.Name(), "/", "_")
	for i := range nPeers {
		state, _ := sm.MakeGenesisState(genDoc)
		thisConfig, err := ResetConfig(t.TempDir(), fmt.Sprintf("%s_%d", testName, i))
		require.NoError(t, err)

		configRootDirs = append(configRootDirs, thisConfig.RootDir)
		ensureDir(filepath.Dir(thisConfig.Consensus.WalFile()), 0700) // dir for wal
		if i == 0 {
			peer0Config = thisConfig
		}
		var privVal types.PrivValidator
		if i < nValidators {
			privVal = privVals[i]
		} else {
			tempKeyFile, err := os.CreateTemp(t.TempDir(), "priv_validator_key_")
			require.NoError(t, err)

			tempStateFile, err := os.CreateTemp(t.TempDir(), "priv_validator_state_")
			require.NoError(t, err)

			privVal, err = privval.GenFilePV(tempKeyFile.Name(), tempStateFile.Name(), "")
			require.NoError(t, err)
		}

		app := kvstore.NewApplication()
		vals := types.TM2PB.ValidatorUpdates(state.Validators)
		state.Version.Consensus.App = kvstore.ProtocolVersion
		_, err = app.InitChain(ctx, &abci.RequestInitChain{})
		require.NoError(t, err)
		app.SetValidators(vals)
		// sm.SaveState(stateDB,state)	//height 1's validatorsInfo already saved in LoadStateFromDBOrGenesisDoc above

		proxyApp := proxy.New(app, proxy.NewMetrics())
		css[i] = newStateWithConfig(t, thisConfig, state, privVal, proxyApp)
		css[i].SetTimeoutTicker(tickerFunc())
	}
	return css, genDoc, peer0Config, func() {
		for _, dir := range configRootDirs {
			os.RemoveAll(dir)
		}
	}
}

type genesisStateArgs struct {
	Validators int
	Power      int64
	Params     *types.ConsensusParams
	Time       time.Time
}

func makeGenesisState(ctx context.Context, t *testing.T, cfg *config.Config, args genesisStateArgs) (sm.State, []types.PrivValidator) {
	t.Helper()
	if args.Power == 0 {
		args.Power = 1
	}
	if args.Validators == 0 {
		args.Power = 4
	}
	valSet, privValidators := factory.ValidatorSet(ctx, args.Validators, args.Power)
	if args.Params == nil {
		args.Params = types.DefaultConsensusParams()
	}
	if args.Time.IsZero() {
		args.Time = time.Now()
	}
	genDoc := factory.GenesisDoc(cfg, args.Time, valSet.Validators, args.Params)
	s0, err := sm.MakeGenesisState(genDoc)
	require.NoError(t, err)
	return s0, privValidators
}

func newMockTickerFunc(onlyOnce bool) func() TimeoutTicker {
	return func() TimeoutTicker {
		return &mockTicker{
			c:        make(chan timeoutInfo, 100),
			onlyOnce: onlyOnce,
		}
	}
}

// mock ticker only fires on RoundStepNewHeight
// and only once if onlyOnce=true
type mockTicker struct {
	c chan timeoutInfo

	mtx      sync.Mutex
	onlyOnce bool
	fired    bool
}

func (m *mockTicker) Run(context.Context) error { return nil }

func (m *mockTicker) ScheduleTimeout(ti timeoutInfo) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if m.onlyOnce && m.fired {
		return
	}
	if ti.Step == cstypes.RoundStepNewHeight {
		m.c <- ti
		m.fired = true
	}
}

func (m *mockTicker) Chan() <-chan timeoutInfo {
	return m.c
}

func signDataIsEqual(v1 *types.Vote, v2 *tmproto.Vote) bool {
	if v1 == nil || v2 == nil {
		return false
	}

	return v1.Type == v2.Type &&
		bytes.Equal(v1.BlockID.Hash, v2.BlockID.GetHash()) &&
		v1.Height == v2.GetHeight() &&
		v1.Round == v2.Round &&
		bytes.Equal(v1.ValidatorAddress.Bytes(), v2.GetValidatorAddress()) &&
		v1.ValidatorIndex == v2.GetValidatorIndex()
}
