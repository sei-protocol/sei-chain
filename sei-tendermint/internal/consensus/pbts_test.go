package consensus

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	cstypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	tmpubsub "github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	tmtimemocks "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const (
	// blockTimeIota is used in the test harness as the time between
	// blocks when not otherwise specified.
	blockTimeIota = time.Millisecond

	// The PBTS harness needs a proposer schedule where the observed validator
	// leads at height 1 and again after three consecutive non-observed leaders.
	// Search sequential validator powers until a validator set with that shape is found.
	maxPBTSPowerSearch = int64(64)
)

// pbtsTestHarness constructs a Tendermint network that can be used for testing the
// implementation of the Proposer-Based timestamps algorithm.
// It runs a series of consensus heights and captures timing of votes and events.
type pbtsTestHarness struct {
	// configuration options set by the user of the test harness.
	pbtsTestConfiguration

	// The timestamp of the first block produced by the network.
	firstBlockTime time.Time

	// The Tendermint consensus state machine being run during
	// a run of the pbtsTestHarness.
	observedState *testState

	// A stub for signing votes and messages using the key
	// from the observedState.
	observedValidator *validatorStub

	// All validator stubs participating in the harness, including the observed validator.
	validators []*validatorStub

	// A list of simulated validators that interact with the observedState and are
	// fully controlled by the test harness.
	otherValidators []*validatorStub

	// Precomputed leader addresses by height, 1-indexed in comments and 0-indexed in storage.
	leaderSchedule []types.Address

	// First height after genesis where the schedule matches the PBTS harness shape:
	// non-observed, non-observed, non-observed, observed.
	patternStartHeight int64

	// The mock time source used by all of the validator stubs in the test harness.
	// This mock clock allows the test harness to produce votes and blocks with arbitrary
	// timestamps.
	validatorClock *tmtimemocks.Source

	chainID string

	// channels for verifying that the observed validator completes certain actions.
	ensureProposalCh, roundCh, blockCh, ensureVoteCh <-chan tmpubsub.Message

	// channel of events from the observed validator annotated with the timestamp
	// the event was received.
	eventCh <-chan timestampedEvent

	currentHeight int64
	currentRound  int32
}

type pbtsTestConfiguration struct {
	// The timestamp consensus parameters to be used by the state machine under test.
	synchronyParams types.SynchronyParams

	// The setting to use for the TimeoutPropose configuration parameter.
	timeoutPropose time.Duration

	// The genesis time
	genesisTime time.Time

	// The times offset from height 1 block time of the block proposed at height 2.
	height2ProposedBlockOffset time.Duration

	// The time offset from height 1 block time at which the proposal at height 2 should be delivered.
	height2ProposalTimeDeliveryOffset time.Duration

	// The time offset from height 1 block time of the block proposed at height 4.
	// At height 4, the proposed block and the deliver offsets are the same so
	// that timely-ness does not affect height 4.
	height4ProposedBlockOffset time.Duration
}

func newPBTSTestHarness(ctx context.Context, t *testing.T, tc pbtsTestConfiguration) pbtsTestHarness {
	t.Helper()
	const validators = 4
	cfg := configSetup(t)
	clock := new(tmtimemocks.Source)

	if tc.genesisTime.IsZero() {
		tc.genesisTime = time.Now()
	}

	if tc.height4ProposedBlockOffset == 0 {

		// Set a default height4ProposedBlockOffset.
		// Use a proposed block time that is greater than the time that the
		// block at height 2 was delivered. Height 3 is not relevant for testing
		// and always occurs blockTimeIota before height 4. If not otherwise specified,
		// height 4 therefore occurs 2*blockTimeIota after height 2.
		tc.height4ProposedBlockOffset = tc.height2ProposalTimeDeliveryOffset + 2*blockTimeIota
	}
	consensusParams := factory.ConsensusParams()
	consensusParams.Timeout.Propose = tc.timeoutPropose
	consensusParams.Synchrony = tc.synchronyParams
	state, privVals, leaderSchedule, patternStartHeight := findPBTSTestHarnessGenesisState(
		ctx,
		t,
		cfg,
		tc,
		consensusParams,
		validators,
	)
	vss := make([]*validatorStub, validators)
	for i := 0; i < validators; i++ {
		vss[i] = newValidatorStub(privVals[i], int32(i))
	}
	vss = permutePBTSTestValidators(ctx, t, vss, leaderSchedule[0])
	observed := vss[0]
	cs := newStateWithConfig(t, cfg, state, observed.PrivValidator, kvstore.NewProxy())
	incrementHeight(vss[1:]...)

	for _, vs := range vss {
		vs.clock = clock
	}
	pubKey, err := observed.PrivValidator.GetPubKey(ctx)
	require.NoError(t, err)

	eventCh := timestampedCollector(ctx, t, cs.eventBus)

	return pbtsTestHarness{
		pbtsTestConfiguration: tc,
		observedValidator:     observed,
		validators:            vss,
		observedState:         cs,
		otherValidators:       vss[1:],
		leaderSchedule:        leaderSchedule,
		patternStartHeight:    patternStartHeight,
		validatorClock:        clock,
		currentHeight:         1,
		chainID:               config.TestLoadGenesis(cfg).ChainID,
		roundCh:               subscribe(ctx, t, cs.eventBus, types.EventQueryNewRound),
		ensureProposalCh:      subscribe(ctx, t, cs.eventBus, types.EventQueryCompleteProposal),
		blockCh:               subscribe(ctx, t, cs.eventBus, types.EventQueryNewBlock),
		ensureVoteCh:          cs.subscribeToVoterBuffered(ctx, t, pubKey.Address()),
		eventCh:               eventCh,
	}
}

func findPBTSTestHarnessGenesisState(
	ctx context.Context,
	t *testing.T,
	cfg *config.Config,
	tc pbtsTestConfiguration,
	consensusParams *types.ConsensusParams,
	validators int,
) (sm.State, []types.PrivValidator, []types.Address, int64) {
	t.Helper()

	for power := int64(1); power <= maxPBTSPowerSearch; power++ {
		state, privVals := makeGenesisState(ctx, t, cfg, genesisStateArgs{
			Params:     consensusParams,
			Time:       tc.genesisTime,
			Validators: validators,
			Power:      power,
		})
		leaderSchedule := pbtsLeaderSchedule(state.Validators.Copy(), 32)
		vss := make([]*validatorStub, validators)
		for i := 0; i < validators; i++ {
			vss[i] = newValidatorStub(privVals[i], int32(i))
		}
		vss = permutePBTSTestValidators(ctx, t, vss, leaderSchedule[0])
		patternStartHeight, ok := findPBTSPatternStart(ctx, t, vss, leaderSchedule, 2)
		if ok {
			return state, privVals, leaderSchedule, patternStartHeight
		}
	}

	t.Fatalf(
		"failed to find PBTS proposer pattern with validator power in range [1,%d]",
		maxPBTSPowerSearch,
	)
	return sm.State{}, nil, nil, 0
}

func pbtsLeaderSchedule(validators *types.ValidatorSet, maxHeight int64) []types.Address {
	leaders := make([]types.Address, 0, maxHeight)
	simValidators := validators.Copy()
	for height := int64(1); height <= maxHeight; height++ {
		rs := &cstypes.RoundState{
			HRS: cstypes.HRS{
				Height: height,
				Round:  0,
			},
			Validators: simValidators.Copy(),
		}
		leaders = append(leaders, rs.Leader().Address())
	}
	return leaders
}

func permutePBTSTestValidators(
	ctx context.Context,
	t *testing.T,
	vss []*validatorStub,
	observedLeader types.Address,
) []*validatorStub {
	t.Helper()

	ordered := make([]*validatorStub, 0, len(vss))
	used := make(map[string]struct{})
	for _, addr := range []types.Address{observedLeader} {
		var vs *validatorStub
		for _, candidate := range vss {
			pubKey, err := candidate.GetPubKey(ctx)
			require.NoError(t, err)
			if bytes.Equal(pubKey.Address(), addr) {
				vs = candidate
				break
			}
		}
		require.NotNil(t, vs)
		key := string(addr)
		if _, ok := used[key]; ok {
			continue
		}
		used[key] = struct{}{}
		ordered = append(ordered, vs)
	}

	for _, vs := range vss {
		pubKey, err := vs.GetPubKey(ctx)
		require.NoError(t, err)
		if _, ok := used[string(pubKey.Address())]; ok {
			continue
		}
		ordered = append(ordered, vs)
	}

	require.Len(t, ordered, len(vss))
	return ordered
}

func findPBTSPatternStart(
	ctx context.Context,
	t *testing.T,
	vss []*validatorStub,
	leaderSchedule []types.Address,
	startHeight int64,
) (int64, bool) {
	t.Helper()

	observedPubKey, err := vss[0].GetPubKey(ctx)
	require.NoError(t, err)
	observedAddr := observedPubKey.Address()

	for height := startHeight; height+3 <= int64(len(leaderSchedule)); height++ {
		h0 := leaderSchedule[height-1]
		h1 := leaderSchedule[height]
		h2 := leaderSchedule[height+1]
		finish := leaderSchedule[height+2]

		if bytes.Equal(h0, observedAddr) || bytes.Equal(h1, observedAddr) || bytes.Equal(h2, observedAddr) {
			continue
		}
		if !bytes.Equal(finish, observedAddr) {
			continue
		}
		return height, true
	}

	return 0, false
}

func (p *pbtsTestHarness) proposerAtHeight(ctx context.Context, t *testing.T, height int64) *validatorStub {
	t.Helper()
	require.GreaterOrEqual(t, len(p.leaderSchedule), int(height))
	return validatorStubByAddress(ctx, t, p.validators, p.leaderSchedule[height-1])
}

func (p *pbtsTestHarness) advanceToHeight(ctx context.Context, t *testing.T, targetHeight int64) {
	t.Helper()
	nextProposedTime := p.firstBlockTime.Add(blockTimeIota)
	for p.currentHeight < targetHeight {
		signer := p.proposerAtHeight(ctx, t, p.currentHeight).PrivValidator
		p.nextHeight(ctx, t, signer, nextProposedTime, nextProposedTime, nextProposedTime.Add(blockTimeIota), false)
		nextProposedTime = nextProposedTime.Add(blockTimeIota)
	}
}

func (p *pbtsTestHarness) observedValidatorProposerHeight(ctx context.Context, t *testing.T, previousBlockTime time.Time) (heightResult, time.Time) {
	p.validatorClock.On("Now").Return(p.genesisTime.Add(p.height2ProposedBlockOffset)).Times(6)

	ensureNewRound(t, p.roundCh, p.currentHeight, p.currentRound)

	timeout := time.Until(previousBlockTime.Add(ensureTimeout))
	ensureProposalWithTimeout(t, p.ensureProposalCh, p.currentHeight, p.currentRound, nil, timeout)

	rs := p.observedState.GetRoundState()
	bid := types.BlockID{Hash: rs.ProposalBlock.Hash(), PartSetHeader: rs.ProposalBlockParts.Header()}
	ensurePrevote(t, p.ensureVoteCh, p.currentHeight, p.currentRound)
	p.observedState.signAddVotes(ctx, t, tmproto.PrevoteType, p.chainID, bid, p.otherValidators...)

	ensurePrecommit(t, p.ensureVoteCh, p.currentHeight, p.currentRound)
	p.observedState.signAddVotes(ctx, t, tmproto.PrecommitType, p.chainID, bid, p.otherValidators...)

	ensureNewBlock(t, p.blockCh, p.currentHeight)

	vk, err := p.observedValidator.GetPubKey(ctx)
	require.NoError(t, err)
	res := collectHeightResults(ctx, t, p.eventCh, p.currentHeight, vk.Address())

	p.currentHeight++
	incrementHeight(p.otherValidators...)
	return res, rs.ProposalBlock.Time
}

func (p *pbtsTestHarness) height2(ctx context.Context, t *testing.T) heightResult {
	p.advanceToHeight(ctx, t, p.patternStartHeight)
	signer := p.proposerAtHeight(ctx, t, p.currentHeight).PrivValidator
	return p.nextHeight(ctx, t, signer,
		p.firstBlockTime.Add(p.height2ProposalTimeDeliveryOffset),
		p.firstBlockTime.Add(p.height2ProposedBlockOffset),
		p.firstBlockTime.Add(p.height2ProposedBlockOffset+10*blockTimeIota),
		true)
}

func (p *pbtsTestHarness) intermediateHeights(ctx context.Context, t *testing.T) {
	require.Equal(t, p.patternStartHeight+1, p.currentHeight)

	signer := p.proposerAtHeight(ctx, t, p.currentHeight).PrivValidator
	p.nextHeight(ctx, t, signer,
		p.firstBlockTime.Add(p.height2ProposedBlockOffset+10*blockTimeIota),
		p.firstBlockTime.Add(p.height2ProposedBlockOffset+10*blockTimeIota),
		p.firstBlockTime.Add(p.height4ProposedBlockOffset),
		false)

	signer = p.proposerAtHeight(ctx, t, p.currentHeight).PrivValidator
	p.nextHeight(ctx, t, signer,
		p.firstBlockTime.Add(p.height4ProposedBlockOffset),
		p.firstBlockTime.Add(p.height4ProposedBlockOffset),
		time.Now(),
		false)
}

func (p *pbtsTestHarness) height5(ctx context.Context, t *testing.T) (heightResult, time.Time) {
	require.Equal(t, p.patternStartHeight+3, p.currentHeight, "expected current height to be the matched observed proposer height")
	return p.observedValidatorProposerHeight(ctx, t, p.firstBlockTime.Add(p.height4ProposedBlockOffset))
}

func (p *pbtsTestHarness) nextHeight(
	ctx context.Context,
	t *testing.T,
	proposer types.PrivValidator,
	deliverTime,
	proposedTime,
	nextProposedTime time.Time,
	expectProposalID bool,
) heightResult {
	p.validatorClock.On("Now").Return(nextProposedTime).Times(6)

	ensureNewRound(t, p.roundCh, p.currentHeight, p.currentRound)

	p.observedState.mtx.Lock()
	b, err := p.observedState.createProposalBlock(ctx)
	require.NoError(t, err)
	b.Height = p.currentHeight
	b.Time = proposedTime
	b.Header.Height = p.currentHeight
	b.Header.Time = proposedTime
	ps, err := b.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	validRound := p.observedState.roundState.ValidRound()
	chainID := p.observedState.state.ChainID
	p.observedState.mtx.Unlock()

	k, err := proposer.GetPubKey(ctx)
	require.NoError(t, err)
	bid := types.BlockID{Hash: b.Hash(), PartSetHeader: ps.Header()}
	prop := types.NewProposal(p.currentHeight, 0, validRound, bid, proposedTime, b.GetTxHashes(), b.Header, b.LastCommit, b.Evidence, k.Address())
	tp := prop.ToProto()

	if err := proposer.SignProposal(ctx, chainID, tp); err != nil {
		t.Fatalf("error signing proposal: %s", err)
	}

	prop.Signature = utils.OrPanic1(crypto.SigFromBytes(tp.Signature))
	if err := p.sendProposalAndPartsAt(ctx, prop, ps, deliverTime); err != nil {
		t.Fatal(err)
	}
	if expectProposalID {
		ensureProposal(t, p.ensureProposalCh, p.currentHeight, 0, bid)
	} else {
		ensureProposalWithTimeout(t, p.ensureProposalCh, p.currentHeight, 0, nil, ensureTimeout)
	}

	ensurePrevote(t, p.ensureVoteCh, p.currentHeight, p.currentRound)
	p.observedState.signAddVotes(ctx, t, tmproto.PrevoteType, p.chainID, bid, p.otherValidators...)

	ensurePrecommit(t, p.ensureVoteCh, p.currentHeight, p.currentRound)
	p.observedState.signAddVotes(ctx, t, tmproto.PrecommitType, p.chainID, bid, p.otherValidators...)

	vk, err := p.observedValidator.GetPubKey(ctx)
	require.NoError(t, err)
	res := collectHeightResults(ctx, t, p.eventCh, p.currentHeight, vk.Address())
	ensureNewBlock(t, p.blockCh, p.currentHeight)

	p.currentHeight++
	incrementHeight(p.otherValidators...)
	return res
}

func (p *pbtsTestHarness) sendProposalAndPartsAt(
	ctx context.Context,
	prop *types.Proposal,
	ps *types.PartSet,
	recvTime time.Time,
) error {
	peerID := types.NodeID("peerID")
	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.observedState.peerMsgQueue <- msgInfo{&ProposalMessage{prop}, peerID, recvTime}:
	}

	for i := 0; i < int(ps.Total()); i++ {
		part := ps.GetPart(i)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case p.observedState.peerMsgQueue <- msgInfo{
			&BlockPartMessage{prop.Height, prop.Round, part},
			peerID,
			recvTime,
		}:
		}
	}
	return nil
}

func timestampedCollector(ctx context.Context, t *testing.T, eb *eventbus.EventBus) <-chan timestampedEvent {
	t.Helper()

	// Since eventCh is not read until the end of each height, it must be large
	// enough to hold all of the events produced during a single height.
	eventCh := make(chan timestampedEvent, 100)

	if err := eb.Observe(ctx, func(msg tmpubsub.Message) error {
		eventCh <- timestampedEvent{
			ts: time.Now(),
			m:  msg,
		}
		return nil
	}, types.EventQueryVote, types.EventQueryCompleteProposal); err != nil {
		t.Fatalf("Failed to observe query %v: %v", types.EventQueryVote, err)
	}
	return eventCh
}

func collectHeightResults(ctx context.Context, t *testing.T, eventCh <-chan timestampedEvent, height int64, address []byte) heightResult {
	t.Helper()
	var res heightResult
	for event := range eventCh {
		switch v := event.m.Data().(type) {
		case types.EventDataVote:
			if v.Vote.Height > height {
				t.Fatalf("received prevote from unexpected height, expected: %d, saw: %d", height, v.Vote.Height)
			}
			// Avoid recording stale prevote (possibly nil) from a previous height.
			if v.Vote.Height < height {
				continue
			}
			if !bytes.Equal(address, v.Vote.ValidatorAddress) {
				continue
			}
			if v.Vote.Type != tmproto.PrevoteType {
				continue
			}
			res.prevote = v.Vote
			res.prevoteIssuedAt = event.ts

		case types.EventDataCompleteProposal:
			if v.Height > height {
				t.Fatalf("received proposal from unexpected height, expected: %d, saw: %d", height, v.Height)
			}
			// Avoid recording stale prevote (possibly nil) from a previous height.
			if v.Height < height {
				continue
			}
			res.proposalIssuedAt = event.ts
		}
		if res.isComplete() {
			return res
		}
	}
	t.Fatalf("complete height result never seen for height %d", height)

	panic("unreachable")
}

type timestampedEvent struct {
	ts time.Time
	m  tmpubsub.Message
}

func (p *pbtsTestHarness) run(ctx context.Context, t *testing.T) resultSet {
	p.observedState.startTestRound(ctx, p.currentHeight, p.currentRound)

	r1, proposalBlockTime := p.observedValidatorProposerHeight(ctx, t, p.genesisTime)
	p.firstBlockTime = proposalBlockTime
	r2 := p.height2(ctx, t)
	p.intermediateHeights(ctx, t)
	r5, _ := p.height5(ctx, t)
	return resultSet{
		genesisHeight: r1,
		height2:       r2,
		height5:       r5,
	}
}

type resultSet struct {
	genesisHeight heightResult
	height2       heightResult
	height5       heightResult
}

type heightResult struct {
	proposalIssuedAt time.Time
	prevote          *types.Vote
	prevoteIssuedAt  time.Time
}

func (hr heightResult) isComplete() bool {
	return !hr.proposalIssuedAt.IsZero() && !hr.prevoteIssuedAt.IsZero() && hr.prevote != nil
}

// TestProposerWaitsForGenesisTime tests that a proposer will not propose a block
// until after the genesis time has passed. The test sets the genesis time in the
// future and then ensures that the observed validator waits to propose a block.
func TestProposerWaitsForGenesisTime(t *testing.T) {
	ctx := t.Context()

	// create a genesis time far (enough) in the future.
	initialTime := time.Now().Add(800 * time.Millisecond)
	tc := pbtsTestConfiguration{
		synchronyParams: types.SynchronyParams{
			Precision:    10 * time.Millisecond,
			MessageDelay: 10 * time.Millisecond,
		},
		timeoutPropose:                    10 * time.Millisecond,
		genesisTime:                       initialTime,
		height2ProposalTimeDeliveryOffset: 10 * time.Millisecond,
		height2ProposedBlockOffset:        10 * time.Millisecond,
		height4ProposedBlockOffset:        30 * time.Millisecond,
	}

	pbtsTest := newPBTSTestHarness(ctx, t, tc)
	results := pbtsTest.run(ctx, t)

	// ensure that the proposal was issued after the genesis time.
	assert.True(t, results.genesisHeight.proposalIssuedAt.After(tc.genesisTime))
}

// TestProposerWaitsForPreviousBlock tests that the proposer of a block waits until
// the block time of the previous height has passed to propose the next block.
// The test harness ensures that the observed validator will be the proposer at
// height 1 and height 5. The test sets the block time of height 4 in the future
// and then verifies that the observed validator waits until after the block time
// of height 4 to propose a block at height 5.
func TestProposerWaitsForPreviousBlock(t *testing.T) {
	ctx := t.Context()
	initialTime := time.Now().Add(time.Millisecond * 50)
	tc := pbtsTestConfiguration{
		synchronyParams: types.SynchronyParams{
			// Keep this test away from timing boundaries on loaded CI runners.
			// We are validating proposer wait behavior, not tight timely-window edges.
			Precision:    200 * time.Millisecond,
			MessageDelay: 900 * time.Millisecond,
		},
		timeoutPropose:                    250 * time.Millisecond, // Provide enough headroom for CI
		genesisTime:                       initialTime,
		height2ProposalTimeDeliveryOffset: 150 * time.Millisecond,
		height2ProposedBlockOffset:        100 * time.Millisecond,
		height4ProposedBlockOffset:        800 * time.Millisecond,
	}

	pbtsTest := newPBTSTestHarness(ctx, t, tc)
	results := pbtsTest.run(ctx, t)

	// the observed validator is the proposer at height 5.
	// ensure that the observed validator did not propose a block until after
	// the time configured for height 4.
	assert.True(t, results.height5.proposalIssuedAt.After(pbtsTest.firstBlockTime.Add(tc.height4ProposedBlockOffset)))

	// Ensure that the validator issued a prevote for a non-nil block.
	assert.NotNil(t, results.height5.prevote.BlockID.Hash)
}

func TestProposerWaitTime(t *testing.T) {
	genesisTime, err := time.Parse(time.RFC3339, "2019-03-13T23:00:00Z")
	require.NoError(t, err)
	testCases := []struct {
		name              string
		previousBlockTime time.Time
		localTime         time.Time
		expectedWait      time.Duration
	}{
		{
			name:              "block time greater than local time",
			previousBlockTime: genesisTime.Add(5 * time.Nanosecond),
			localTime:         genesisTime.Add(1 * time.Nanosecond),
			expectedWait:      4 * time.Nanosecond,
		},
		{
			name:              "local time greater than block time",
			previousBlockTime: genesisTime.Add(1 * time.Nanosecond),
			localTime:         genesisTime.Add(5 * time.Nanosecond),
			expectedWait:      0,
		},
		{
			name:              "both times equal",
			previousBlockTime: genesisTime.Add(5 * time.Nanosecond),
			localTime:         genesisTime.Add(5 * time.Nanosecond),
			expectedWait:      0,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			mockSource := new(tmtimemocks.Source)
			mockSource.On("Now").Return(testCase.localTime)

			ti := proposerWaitTime(mockSource, testCase.previousBlockTime)
			assert.Equal(t, testCase.expectedWait, ti)
		})
	}
}

func TestTimelyProposal(t *testing.T) {
	ctx := t.Context()
	initialTime := time.Now()

	tc := pbtsTestConfiguration{
		synchronyParams: types.SynchronyParams{
			// Keep this test away from timing boundaries so scheduler jitter in CI does not
			// cause occasional nil prevotes for an otherwise timely proposal.
			Precision:    25 * time.Millisecond,
			MessageDelay: 300 * time.Millisecond,
		},
		timeoutPropose:                    80 * time.Millisecond,
		genesisTime:                       initialTime,
		height2ProposedBlockOffset:        15 * time.Millisecond,
		height2ProposalTimeDeliveryOffset: 30 * time.Millisecond,
	}

	pbtsTest := newPBTSTestHarness(ctx, t, tc)
	results := pbtsTest.run(ctx, t)
	require.NotNil(t, results.height2.prevote.BlockID.Hash)
}

func TestTooFarInThePastProposal(t *testing.T) {
	ctx := t.Context()

	// localtime > proposedBlockTime + MsgDelay + Precision
	tc := pbtsTestConfiguration{
		synchronyParams: types.SynchronyParams{
			Precision:    1 * time.Millisecond,
			MessageDelay: 10 * time.Millisecond,
		},
		timeoutPropose:                    50 * time.Millisecond,
		height2ProposedBlockOffset:        15 * time.Millisecond,
		height2ProposalTimeDeliveryOffset: 27 * time.Millisecond,
	}

	pbtsTest := newPBTSTestHarness(ctx, t, tc)
	results := pbtsTest.run(ctx, t)

	require.Nil(t, results.height2.prevote.BlockID.Hash)
}

func TestTooFarInTheFutureProposal(t *testing.T) {
	ctx := t.Context()

	// localtime < proposedBlockTime - Precision
	tc := pbtsTestConfiguration{
		synchronyParams: types.SynchronyParams{
			Precision:    1 * time.Millisecond,
			MessageDelay: 10 * time.Millisecond,
		},
		timeoutPropose:                    50 * time.Millisecond,
		height2ProposedBlockOffset:        100 * time.Millisecond,
		height2ProposalTimeDeliveryOffset: 10 * time.Millisecond,
		height4ProposedBlockOffset:        150 * time.Millisecond,
	}

	pbtsTest := newPBTSTestHarness(ctx, t, tc)
	results := pbtsTest.run(ctx, t)

	require.Nil(t, results.height2.prevote.BlockID.Hash)
}
