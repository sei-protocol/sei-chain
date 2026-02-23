package state_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	abcimocks "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	mpmocks "github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/mocks"
	sf "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

var (
	chainID             = "execution_chain"
	testPartSize uint32 = 65536
)

func TestApplyBlock(t *testing.T) {
	app := &testApp{}
	logger := log.NewNopLogger()

	ctx := t.Context()

	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	state, stateDB, _ := makeState(t, 1, 1)
	stateStore := sm.NewStore(stateDB)
	blockStore := store.NewBlockStore(dbm.NewMemDB())
	mp := &mpmocks.Mempool{}
	mp.On("Lock").Return()
	mp.On("Unlock").Return()
	mp.On("Update",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything).Return(nil)
	mp.On("TxStore").Return(nil)
	blockExec := sm.NewBlockExecutor(stateStore, logger, app, mp, sm.EmptyEvidencePool{}, blockStore, eventBus, sm.NopMetrics())

	block := sf.MakeBlock(state, 1, new(types.Commit))
	bps, err := block.MakePartSet(testPartSize)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: block.Hash(), PartSetHeader: bps.Header()}

	state, err = blockExec.ApplyBlock(ctx, state, blockID, block, nil)
	require.NoError(t, err)

	// TODO check state and mempool
	assert.EqualValues(t, 1, state.Version.Consensus.App, "App version wasn't updated")
}

// TestFinalizeBlockDecidedLastCommit ensures we correctly send the
// DecidedLastCommit to the application. The test ensures that the
// DecidedLastCommit properly reflects which validators signed the preceding
// block.
func TestFinalizeBlockDecidedLastCommit(t *testing.T) {
	ctx := t.Context()

	logger := log.NewNopLogger()
	app := &testApp{}

	state, stateDB, privVals := makeState(t, 7, 1)
	stateStore := sm.NewStore(stateDB)
	absentSig := types.NewCommitSigAbsent()

	testCases := []struct {
		name             string
		absentCommitSigs map[int]bool
	}{
		{"none absent", map[int]bool{}},
		{"one absent", map[int]bool{1: true}},
		{"multiple absent", map[int]bool{1: true, 3: true}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			blockStore := store.NewBlockStore(dbm.NewMemDB())
			evpool := &mocks.EvidencePool{}
			evpool.On("PendingEvidence", mock.Anything).Return([]types.Evidence{}, 0)
			evpool.On("Update", ctx, mock.Anything, mock.Anything).Return()
			evpool.On("CheckEvidence", ctx, mock.Anything).Return(nil)
			mp := &mpmocks.Mempool{}
			mp.On("Lock").Return()
			mp.On("Unlock").Return()
			mp.On("Update",
				mock.Anything,
				mock.Anything,
				mock.Anything,
				mock.Anything,
				mock.Anything,
				mock.Anything,
				mock.Anything).Return(nil)
			mp.On("TxStore").Return(nil)

			eventBus := eventbus.NewDefault(logger)
			require.NoError(t, eventBus.Start(ctx))

			blockExec := sm.NewBlockExecutor(stateStore, log.NewNopLogger(), app, mp, evpool, blockStore, eventBus, sm.NopMetrics())
			state, _, lastCommit := makeAndCommitGoodBlock(ctx, t, state, 1, new(types.Commit), state.NextValidators.Validators[0].Address, blockExec, privVals, nil)

			for idx, isAbsent := range tc.absentCommitSigs {
				if isAbsent {
					lastCommit.Signatures[idx] = absentSig
				}
			}

			// block for height 2
			block := sf.MakeBlock(state, 2, lastCommit)
			bps, err := block.MakePartSet(testPartSize)
			require.NoError(t, err)
			blockID := types.BlockID{Hash: block.Hash(), PartSetHeader: bps.Header()}
			_, err = blockExec.ApplyBlock(ctx, state, blockID, block, nil)
			require.NoError(t, err)

			// -> app receives a list of validators with a bool indicating if they signed
			for i, v := range app.CommitVotes {
				_, absent := tc.absentCommitSigs[i]
				assert.Equal(t, !absent, v.SignedLastBlock)
			}
		})
	}
}

// TestFinalizeBlockByzantineValidators ensures we send byzantine validators list.
func TestFinalizeBlockByzantineValidators(t *testing.T) {
	ctx := t.Context()

	app := &testApp{}
	logger := log.NewNopLogger()

	state, stateDB, privVals := makeState(t, 1, 1)
	stateStore := sm.NewStore(stateDB)

	defaultEvidenceTime := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	privVal := privVals[state.Validators.Validators[0].Address.String()]
	blockID := makeBlockID([]byte("headerhash"), 1000, []byte("partshash"))
	header := &types.Header{
		Version:            version.Consensus{Block: version.BlockProtocol, App: 1},
		ChainID:            state.ChainID,
		Height:             10,
		Time:               defaultEvidenceTime,
		LastBlockID:        blockID,
		LastCommitHash:     crypto.CRandBytes(crypto.HashSize),
		DataHash:           crypto.CRandBytes(crypto.HashSize),
		ValidatorsHash:     state.Validators.Hash(),
		NextValidatorsHash: state.Validators.Hash(),
		ConsensusHash:      crypto.CRandBytes(crypto.HashSize),
		AppHash:            crypto.CRandBytes(crypto.HashSize),
		LastResultsHash:    crypto.CRandBytes(crypto.HashSize),
		EvidenceHash:       crypto.CRandBytes(crypto.HashSize),
		ProposerAddress:    crypto.CRandBytes(crypto.AddressSize),
	}

	// we don't need to worry about validating the evidence as long as they pass validate basic
	dve, err := types.NewMockDuplicateVoteEvidenceWithValidator(ctx, 3, defaultEvidenceTime, privVal, state.ChainID)
	require.NoError(t, err)
	dve.ValidatorPower = 1000
	lcae := &types.LightClientAttackEvidence{
		ConflictingBlock: &types.LightBlock{
			SignedHeader: &types.SignedHeader{
				Header: header,
				Commit: &types.Commit{
					Height:  10,
					BlockID: makeBlockID(header.Hash(), 100, []byte("partshash")),
					Signatures: []types.CommitSig{{
						BlockIDFlag:      types.BlockIDFlagNil,
						ValidatorAddress: crypto.AddressHash([]byte("validator_address")),
						Timestamp:        defaultEvidenceTime,
						Signature:        utils.Some(utils.OrPanic1(crypto.SigFromBytes(crypto.CRandBytes(64)))),
					}},
				},
			},
			ValidatorSet: state.Validators,
		},
		CommonHeight:        8,
		ByzantineValidators: []*types.Validator{state.Validators.Validators[0]},
		TotalVotingPower:    12,
		Timestamp:           defaultEvidenceTime,
	}

	ev := []types.Evidence{dve, lcae}

	abciMb := []abci.Misbehavior{
		{
			Type:             abci.MisbehaviorType_DUPLICATE_VOTE,
			Height:           3,
			Time:             defaultEvidenceTime,
			Validator:        types.TM2PB.Validator(state.Validators.Validators[0]),
			TotalVotingPower: 10,
		},
		{
			Type:             abci.MisbehaviorType_LIGHT_CLIENT_ATTACK,
			Height:           8,
			Time:             defaultEvidenceTime,
			Validator:        types.TM2PB.Validator(state.Validators.Validators[0]),
			TotalVotingPower: 12,
		},
	}

	evpool := &mocks.EvidencePool{}
	evpool.On("PendingEvidence", mock.AnythingOfType("int64")).Return(ev, int64(100))
	evpool.On("Update", ctx, mock.AnythingOfType("state.State"), mock.AnythingOfType("types.EvidenceList")).Return()
	evpool.On("CheckEvidence", ctx, mock.AnythingOfType("types.EvidenceList")).Return(nil)
	mp := &mpmocks.Mempool{}
	mp.On("Lock").Return()
	mp.On("Unlock").Return()
	mp.On("Update",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything).Return(nil)
	mp.On("TxStore").Return(nil)

	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	blockStore := store.NewBlockStore(dbm.NewMemDB())

	blockExec := sm.NewBlockExecutor(stateStore, log.NewNopLogger(), app, mp, evpool, blockStore, eventBus, sm.NopMetrics())

	block := sf.MakeBlock(state, 1, new(types.Commit))
	block.Evidence = ev
	block.Header.EvidenceHash = block.Evidence.Hash()
	bps, err := block.MakePartSet(testPartSize)
	require.NoError(t, err)

	blockID = types.BlockID{Hash: block.Hash(), PartSetHeader: bps.Header()}

	_, err = blockExec.ApplyBlock(ctx, state, blockID, block, nil)
	require.NoError(t, err)

	// TODO check state and mempool
	assert.Equal(t, abciMb, app.ByzantineValidators)
}

func TestProcessProposal(t *testing.T) {
	const height = 2
	txs := factory.MakeNTxs(height, 10)
	ctx := t.Context()

	app := abcimocks.NewApplication(t)
	logger := log.NewNopLogger()

	state, stateDB, privVals := makeState(t, 1, height)
	stateStore := sm.NewStore(stateDB)
	blockStore := store.NewBlockStore(dbm.NewMemDB())

	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	mp := &mpmocks.Mempool{}
	mp.On("TxStore").Return(nil)
	blockExec := sm.NewBlockExecutor(
		stateStore,
		logger,
		app,
		mp,
		sm.EmptyEvidencePool{},
		blockStore,
		eventBus,
		sm.NopMetrics(),
	)

	block0 := sf.MakeBlock(state, height-1, new(types.Commit))
	lastCommitSig := []types.CommitSig{}
	partSet, err := block0.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: block0.Hash(), PartSetHeader: partSet.Header()}
	voteInfos := []abci.VoteInfo{}
	for _, privVal := range privVals {
		vote, err := factory.MakeVote(ctx, privVal, block0.Header.ChainID, 0, 0, 0, 2, blockID, time.Now())
		require.NoError(t, err)
		pk, err := privVal.GetPubKey(ctx)
		require.NoError(t, err)
		addr := pk.Address()
		voteInfos = append(voteInfos,
			abci.VoteInfo{
				SignedLastBlock: true,
				Validator: abci.Validator{
					Address: addr,
					Power:   1000,
				},
			})
		lastCommitSig = append(lastCommitSig, vote.CommitSig())
	}

	block1 := sf.MakeBlock(state, height, &types.Commit{
		Height:     height - 1,
		Signatures: lastCommitSig,
	})
	block1.Txs = txs

	expectedRpp := &abci.RequestProcessProposal{
		Txs:                 block1.Txs.ToSliceOfBytes(),
		Hash:                block1.Hash(),
		Height:              block1.Header.Height,
		Time:                block1.Header.Time,
		ByzantineValidators: block1.Evidence.ToABCI(),
		ProposedLastCommit: abci.CommitInfo{
			Round: 0,
			Votes: voteInfos,
		},
		NextValidatorsHash:    block1.NextValidatorsHash,
		ProposerAddress:       block1.ProposerAddress,
		AppHash:               block1.AppHash,
		ValidatorsHash:        block1.ValidatorsHash,
		ConsensusHash:         block1.ConsensusHash,
		DataHash:              block1.DataHash,
		EvidenceHash:          block1.EvidenceHash,
		LastBlockHash:         block1.LastBlockID.Hash,
		LastBlockPartSetTotal: int64(block1.LastBlockID.PartSetHeader.Total),
		LastBlockPartSetHash:  block1.LastBlockID.Hash,
		LastCommitHash:        block1.LastCommitHash,
		LastResultsHash:       block1.LastResultsHash,
	}

	app.On("ProcessProposal", mock.Anything, mock.Anything).Return(&abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil)
	acceptBlock, err := blockExec.ProcessProposal(ctx, block1, state)
	require.NoError(t, err)
	require.True(t, acceptBlock)
	app.AssertExpectations(t)
	app.AssertCalled(t, "ProcessProposal", ctx, expectedRpp)
}

func TestValidateValidatorUpdates(t *testing.T) {
	pubkey1 := ed25519.GenerateSecretKey().Public()
	pubkey2 := ed25519.GenerateSecretKey().Public()
	pk1 := crypto.PubKeyToProto(pubkey1)
	pk2 := crypto.PubKeyToProto(pubkey2)

	defaultValidatorParams := types.ValidatorParams{PubKeyTypes: []string{types.ABCIPubKeyTypeEd25519}}

	testCases := []struct {
		name string

		abciUpdates     []abci.ValidatorUpdate
		validatorParams types.ValidatorParams

		shouldErr bool
	}{
		{
			"adding a validator is OK",
			[]abci.ValidatorUpdate{{PubKey: pk2, Power: 20}},
			defaultValidatorParams,
			false,
		},
		{
			"updating a validator is OK",
			[]abci.ValidatorUpdate{{PubKey: pk1, Power: 20}},
			defaultValidatorParams,
			false,
		},
		{
			"removing a validator is OK",
			[]abci.ValidatorUpdate{{PubKey: pk2, Power: 0}},
			defaultValidatorParams,
			false,
		},
		{
			"adding a validator with negative power results in error",
			[]abci.ValidatorUpdate{{PubKey: pk2, Power: -100}},
			defaultValidatorParams,
			true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := sm.ValidateValidatorUpdates(tc.abciUpdates, tc.validatorParams)
			if tc.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUpdateValidators(t *testing.T) {
	pubkey1 := ed25519.GenerateSecretKey().Public()
	val1 := types.NewValidator(pubkey1, 10)
	pubkey2 := ed25519.GenerateSecretKey().Public()
	val2 := types.NewValidator(pubkey2, 20)

	pk := crypto.PubKeyToProto(pubkey1)
	pk2 := crypto.PubKeyToProto(pubkey2)

	testCases := []struct {
		name string

		currentSet  *types.ValidatorSet
		abciUpdates []abci.ValidatorUpdate

		resultingSet *types.ValidatorSet
		shouldErr    bool
	}{
		{
			"adding a validator is OK",
			types.NewValidatorSet([]*types.Validator{val1}),
			[]abci.ValidatorUpdate{{PubKey: pk2, Power: 20}},
			types.NewValidatorSet([]*types.Validator{val1, val2}),
			false,
		},
		{
			"updating a validator is OK",
			types.NewValidatorSet([]*types.Validator{val1}),
			[]abci.ValidatorUpdate{{PubKey: pk, Power: 20}},
			types.NewValidatorSet([]*types.Validator{types.NewValidator(pubkey1, 20)}),
			false,
		},
		{
			"removing a validator is OK",
			types.NewValidatorSet([]*types.Validator{val1, val2}),
			[]abci.ValidatorUpdate{{PubKey: pk2, Power: 0}},
			types.NewValidatorSet([]*types.Validator{val1}),
			false,
		},
		{
			"removing a non-existing validator results in error",
			types.NewValidatorSet([]*types.Validator{val1}),
			[]abci.ValidatorUpdate{{PubKey: pk2, Power: 0}},
			types.NewValidatorSet([]*types.Validator{val1}),
			true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			updates, err := types.PB2TM.ValidatorUpdates(tc.abciUpdates)
			assert.NoError(t, err)
			err = tc.currentSet.UpdateWithChangeSet(updates)
			if tc.shouldErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				require.Equal(t, tc.resultingSet.Size(), tc.currentSet.Size())

				assert.Equal(t, tc.resultingSet.TotalVotingPower(), tc.currentSet.TotalVotingPower())

				assert.Equal(t, tc.resultingSet.Validators[0].Address, tc.currentSet.Validators[0].Address)
				if tc.resultingSet.Size() > 1 {
					assert.Equal(t, tc.resultingSet.Validators[1].Address, tc.currentSet.Validators[1].Address)
				}
			}
		})
	}
}

// TestFinalizeBlockValidatorUpdates ensures we update validator set and send an event.
func TestFinalizeBlockValidatorUpdates(t *testing.T) {
	ctx := t.Context()

	app := &testApp{}
	logger := log.NewNopLogger()

	state, stateDB, _ := makeState(t, 1, 1)
	stateStore := sm.NewStore(stateDB)
	blockStore := store.NewBlockStore(dbm.NewMemDB())
	mp := &mpmocks.Mempool{}
	mp.On("Lock").Return()
	mp.On("Unlock").Return()
	mp.On("Update",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything).Return(nil)
	mp.On("ReapMaxBytesMaxGas", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.Txs{})
	mp.On("TxStore").Return(nil)

	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	blockExec := sm.NewBlockExecutor(
		stateStore,
		logger,
		app,
		mp,
		sm.EmptyEvidencePool{},
		blockStore,
		eventBus,
		sm.NopMetrics(),
	)

	updatesSub, err := eventBus.SubscribeWithArgs(ctx, pubsub.SubscribeArgs{
		ClientID: "TestFinalizeBlockValidatorUpdates",
		Query:    types.EventQueryValidatorSetUpdates,
	})
	require.NoError(t, err)

	block := sf.MakeBlock(state, 1, new(types.Commit))
	bps, err := block.MakePartSet(testPartSize)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: block.Hash(), PartSetHeader: bps.Header()}

	pubkey := ed25519.GenerateSecretKey().Public()
	pk := crypto.PubKeyToProto(pubkey)
	app.ValidatorUpdates = []abci.ValidatorUpdate{
		{PubKey: pk, Power: 10},
	}

	state, err = blockExec.ApplyBlock(ctx, state, blockID, block, nil)
	require.NoError(t, err)
	// test new validator was added to NextValidators
	if assert.Equal(t, state.Validators.Size()+1, state.NextValidators.Size()) {
		idx, _ := state.NextValidators.GetByAddress(pubkey.Address())
		if idx < 0 {
			t.Fatalf("can't find address %v in the set %v", pubkey.Address(), state.NextValidators)
		}
	}

	// test we threw an event
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	msg, err := updatesSub.Next(ctx)
	require.NoError(t, err)
	event, ok := msg.Data().(types.EventDataValidatorSetUpdates)
	require.True(t, ok, "Expected event of type EventDataValidatorSetUpdates, got %T", msg.Data())
	if assert.NotEmpty(t, event.ValidatorUpdates) {
		assert.Equal(t, pubkey, event.ValidatorUpdates[0].PubKey)
		assert.EqualValues(t, 10, event.ValidatorUpdates[0].VotingPower)
	}
}

// TestFinalizeBlockValidatorUpdatesResultingInEmptySet checks that processing validator updates that
// would result in empty set causes no panic, an error is raised and NextValidators is not updated
func TestFinalizeBlockValidatorUpdatesResultingInEmptySet(t *testing.T) {
	ctx := t.Context()

	app := &testApp{}
	logger := log.NewNopLogger()

	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	state, stateDB, _ := makeState(t, 1, 1)
	stateStore := sm.NewStore(stateDB)
	blockStore := store.NewBlockStore(dbm.NewMemDB())
	mp := &mpmocks.Mempool{}
	mp.On("TxStore").Return(nil)
	blockExec := sm.NewBlockExecutor(
		stateStore,
		log.NewNopLogger(),
		app,
		mp,
		sm.EmptyEvidencePool{},
		blockStore,
		eventBus,
		sm.NopMetrics(),
	)

	block := sf.MakeBlock(state, 1, new(types.Commit))
	bps, err := block.MakePartSet(testPartSize)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: block.Hash(), PartSetHeader: bps.Header()}

	vp := crypto.PubKeyToProto(state.Validators.Validators[0].PubKey)
	// Remove the only validator
	app.ValidatorUpdates = []abci.ValidatorUpdate{
		{PubKey: vp, Power: 0},
	}

	assert.NotPanics(t, func() { state, err = blockExec.ApplyBlock(ctx, state, blockID, block, nil) })
	assert.Error(t, err)
	assert.NotEmpty(t, state.NextValidators.Validators)
}

func TestEmptyPrepareProposal(t *testing.T) {
	const height = 2
	ctx := t.Context()
	var err error

	logger := log.NewNopLogger()

	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	app := abcimocks.NewApplication(t)
	app.On("PrepareProposal", mock.Anything, mock.Anything).Return(&abci.ResponsePrepareProposal{}, nil)

	state, stateDB, privVals := makeState(t, 1, height)
	stateStore := sm.NewStore(stateDB)
	mp := &mpmocks.Mempool{}
	mp.On("Lock").Return()
	mp.On("Unlock").Return()
	mp.On("Update",
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything,
		mock.Anything).Return(nil)
	mp.On("ReapMaxBytesMaxGas", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.Txs{})
	mp.On("TxStore").Return(nil)

	blockExec := sm.NewBlockExecutor(
		stateStore,
		logger,
		app,
		mp,
		sm.EmptyEvidencePool{},
		nil,
		eventBus,
		sm.NopMetrics(),
	)
	pa, _ := state.Validators.GetByIndex(0)
	commit, _ := makeValidCommit(ctx, t, height, types.BlockID{}, state.Validators, privVals)
	_, err = blockExec.CreateProposalBlock(ctx, height, state, commit, pa)
	require.NoError(t, err)
}

// TestPrepareProposalErrorOnNonExistingRemoved tests that the block creation logic returns
// an error if the ResponsePrepareProposal returned from the application marks
//
//	a transaction as REMOVED that was not present in the original proposal.
func TestPrepareProposalErrorOnNonExistingRemoved(t *testing.T) {
	const height = 2
	ctx := t.Context()

	logger := log.NewNopLogger()
	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	state, stateDB, privVals := makeState(t, 1, height)
	stateStore := sm.NewStore(stateDB)

	evpool := &mocks.EvidencePool{}
	evpool.On("PendingEvidence", mock.Anything).Return([]types.Evidence{}, int64(0))

	mp := &mpmocks.Mempool{}
	mp.On("ReapMaxBytesMaxGas", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.Txs{})

	app := abcimocks.NewApplication(t)

	// create an invalid ResponsePrepareProposal
	rpp := &abci.ResponsePrepareProposal{
		TxRecords: []*abci.TxRecord{
			{
				Action: abci.TxRecord_UNMODIFIED,
				Tx:     []byte("new tx"),
			},
		},
	}
	app.On("PrepareProposal", mock.Anything, mock.Anything).Return(rpp, nil)

	blockExec := sm.NewBlockExecutor(
		stateStore,
		logger,
		app,
		mp,
		evpool,
		nil,
		eventBus,
		sm.NopMetrics(),
	)
	pa, _ := state.Validators.GetByIndex(0)
	commit, _ := makeValidCommit(ctx, t, height, types.BlockID{}, state.Validators, privVals)
	block, err := blockExec.CreateProposalBlock(ctx, height, state, commit, pa)
	require.ErrorContains(t, err, "new transaction incorrectly marked as removed")
	require.Nil(t, block)

	mp.AssertExpectations(t)
}

// TestPrepareProposalReorderTxs tests that CreateBlock produces a block with transactions
// in the order matching the order they are returned from PrepareProposal.
func TestPrepareProposalReorderTxs(t *testing.T) {
	const height = 2
	ctx := t.Context()
	var err error

	logger := log.NewNopLogger()
	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	state, stateDB, privVals := makeState(t, 1, height)
	stateStore := sm.NewStore(stateDB)

	evpool := &mocks.EvidencePool{}
	evpool.On("PendingEvidence", mock.Anything).Return([]types.Evidence{}, int64(0))

	txs := factory.MakeNTxs(height, 10)
	mp := &mpmocks.Mempool{}
	mp.On("ReapMaxBytesMaxGas", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.Txs(txs))

	trs := txsToTxRecords(types.Txs(txs))
	trs = trs[2:]
	trs = append(trs[len(trs)/2:], trs[:len(trs)/2]...)

	app := abcimocks.NewApplication(t)
	app.On("PrepareProposal", mock.Anything, mock.Anything).Return(&abci.ResponsePrepareProposal{
		TxRecords: trs,
	}, nil)

	blockExec := sm.NewBlockExecutor(
		stateStore,
		logger,
		app,
		mp,
		evpool,
		nil,
		eventBus,
		sm.NopMetrics(),
	)
	pa, _ := state.Validators.GetByIndex(0)
	commit, _ := makeValidCommit(ctx, t, height, types.BlockID{}, state.Validators, privVals)
	block, err := blockExec.CreateProposalBlock(ctx, height, state, commit, pa)
	require.NoError(t, err)
	for i, tx := range block.Data.Txs {
		require.Equal(t, types.Tx(trs[i].Tx), tx)
	}

	mp.AssertExpectations(t)

}

// TestPrepareProposalErrorOnTooManyTxs tests that the block creation logic returns
// an error if the ResponsePrepareProposal returned from the application is invalid.
func TestPrepareProposalErrorOnTooManyTxs(t *testing.T) {
	const height = 2
	ctx := t.Context()
	var err error

	logger := log.NewNopLogger()
	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	state, stateDB, privVals := makeState(t, 1, height)
	// limit max block size
	state.ConsensusParams.Block.MaxBytes = 60 * 1024
	stateStore := sm.NewStore(stateDB)

	evpool := &mocks.EvidencePool{}
	evpool.On("PendingEvidence", mock.Anything).Return([]types.Evidence{}, int64(0))

	const nValidators = 1
	var bytesPerTx int64 = 3
	maxDataBytes := types.MaxDataBytes(state.ConsensusParams.Block.MaxBytes, 0, nValidators)
	txs := factory.MakeNTxs(height, maxDataBytes/bytesPerTx+2) // +2 so that tx don't fit
	mp := &mpmocks.Mempool{}
	mp.On("ReapMaxBytesMaxGas", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.Txs(txs))

	trs := txsToTxRecords(types.Txs(txs))

	app := abcimocks.NewApplication(t)
	app.On("PrepareProposal", mock.Anything, mock.Anything).Return(&abci.ResponsePrepareProposal{
		TxRecords: trs,
	}, nil)

	blockExec := sm.NewBlockExecutor(
		stateStore,
		logger,
		app,
		mp,
		evpool,
		nil,
		eventBus,
		sm.NopMetrics(),
	)
	pa, _ := state.Validators.GetByIndex(0)
	commit, _ := makeValidCommit(ctx, t, height, types.BlockID{}, state.Validators, privVals)
	block, err := blockExec.CreateProposalBlock(ctx, height, state, commit, pa)
	require.ErrorContains(t, err, "transaction data size exceeds maximum")
	require.Nil(t, block, "")

	mp.AssertExpectations(t)
}

// TestPrepareProposalErrorOnPrepareProposalError tests when the client returns an error
// upon calling PrepareProposal on it.
func TestPrepareProposalErrorOnPrepareProposalError(t *testing.T) {
	const height = 2
	ctx := t.Context()
	var err error

	logger := log.NewNopLogger()
	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))

	state, stateDB, privVals := makeState(t, 1, height)
	stateStore := sm.NewStore(stateDB)

	evpool := &mocks.EvidencePool{}
	evpool.On("PendingEvidence", mock.Anything).Return([]types.Evidence{}, int64(0))

	txs := factory.MakeNTxs(height, 10)
	mp := &mpmocks.Mempool{}
	mp.On("ReapMaxBytesMaxGas", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(types.Txs(txs))

	app := &failingPrepareProposalApp{}

	blockExec := sm.NewBlockExecutor(
		stateStore,
		logger,
		app,
		mp,
		evpool,
		nil,
		eventBus,
		sm.NopMetrics(),
	)
	pa, _ := state.Validators.GetByIndex(0)
	commit, _ := makeValidCommit(ctx, t, height, types.BlockID{}, state.Validators, privVals)
	block, err := blockExec.CreateProposalBlock(ctx, height, state, commit, pa)
	require.Nil(t, block)
	require.ErrorContains(t, err, "an injected error")

	mp.AssertExpectations(t)
}

type failingPrepareProposalApp struct {
	abci.BaseApplication
}

func (f failingPrepareProposalApp) PrepareProposal(context.Context, *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	return nil, errors.New("an injected error")
}

func makeBlockID(hash []byte, partSetSize uint32, partSetHash []byte) types.BlockID {
	var (
		h   = make([]byte, crypto.HashSize)
		psH = make([]byte, crypto.HashSize)
	)
	copy(h, hash)
	copy(psH, partSetHash)
	return types.BlockID{
		Hash: h,
		PartSetHeader: types.PartSetHeader{
			Total: partSetSize,
			Hash:  psH,
		},
	}
}

func txsToTxRecords(txs []types.Tx) []*abci.TxRecord {
	trs := make([]*abci.TxRecord, len(txs))
	for i, tx := range txs {
		trs[i] = &abci.TxRecord{
			Action: abci.TxRecord_UNMODIFIED,
			Tx:     tx,
		}
	}
	return trs
}

// panicApp is a test app that panics during PrepareProposal to test panic recovery
type panicApp struct {
	abci.BaseApplication
}

func (app *panicApp) PrepareProposal(_ context.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	// This will trigger the panic recovery mechanism in CreateProposalBlock
	panic("test panic for coverage")
}

func (app *panicApp) Info(_ context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	return &abci.ResponseInfo{}, nil
}

func (app *panicApp) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return &abci.ResponseFinalizeBlock{}, nil
}

// TestCreateProposalBlockPanicRecovery tests that panics are recovered and converted to errors
func TestCreateProposalBlockPanicRecovery(t *testing.T) {
	ctx := context.Background()
	logger := log.NewNopLogger()

	// Create the panicking app
	app := &panicApp{}

	// Create test state and executor
	state, stateDB, _ := makeState(t, 1, 1)
	stateStore := sm.NewStore(stateDB)
	blockStore := store.NewBlockStore(dbm.NewMemDB())
	eventBus := eventbus.NewDefault(logger)
	require.NoError(t, eventBus.Start(ctx))
	defer eventBus.Stop()

	// Create mock mempool
	mp := &mpmocks.Mempool{}
	mp.On("ReapMaxBytesMaxGas", mock.Anything, mock.Anything, mock.Anything).Return(types.Txs{})

	blockExec := sm.NewBlockExecutor(
		stateStore,
		logger,
		app,
		mp,
		sm.EmptyEvidencePool{},
		blockStore,
		eventBus,
		sm.NopMetrics(),
	)

	// Get proposer address
	pa, _ := state.Validators.GetByIndex(0)

	// Create commit
	lastCommit := &types.Commit{}

	// This should trigger the panic recovery mechanism
	block, err := blockExec.CreateProposalBlock(ctx, 1, state, lastCommit, pa)

	// Verify that panic was caught and converted to error
	assert.Nil(t, block, "Block should be nil when panic is recovered")
	assert.Error(t, err, "Should return error when panic is recovered")
	assert.Contains(t, err.Error(), "CreateProposalBlock panic recovered", "Error should indicate panic recovery")
	assert.Contains(t, err.Error(), "test panic for coverage", "Error should contain original panic message")

	// Verify mock expectations
	mp.AssertExpectations(t)
}
