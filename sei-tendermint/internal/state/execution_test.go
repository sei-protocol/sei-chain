package state_test

import (
	"context"
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

	ctx := t.Context()

	eventBus := eventbus.NewDefault()
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
	blockExec := sm.NewBlockExecutor(stateStore, app, mp, sm.EmptyEvidencePool{}, blockStore, eventBus, sm.NopMetrics())

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

			eventBus := eventbus.NewDefault()
			require.NoError(t, eventBus.Start(ctx))

			blockExec := sm.NewBlockExecutor(stateStore, app, mp, evpool, blockStore, eventBus, sm.NopMetrics())
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

	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(ctx))

	blockStore := store.NewBlockStore(dbm.NewMemDB())

	blockExec := sm.NewBlockExecutor(stateStore, app, mp, evpool, blockStore, eventBus, sm.NopMetrics())

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

	state, stateDB, privVals := makeState(t, 1, height)
	stateStore := sm.NewStore(stateDB)
	blockStore := store.NewBlockStore(dbm.NewMemDB())

	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(ctx))

	mp := &mpmocks.Mempool{}
	mp.On("TxStore").Return(nil)
	blockExec := sm.NewBlockExecutor(
		stateStore,
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
		ByzantineValidators: block1.Evidence.ToABCI(),
		ProposedLastCommit: abci.CommitInfo{
			Round: 0,
			Votes: voteInfos,
		},
		Header: block1.Header.ToProto(),
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

	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(ctx))

	blockExec := sm.NewBlockExecutor(
		stateStore,
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
		if _, _, ok := state.NextValidators.GetByAddress(pubkey.Address()); !ok {
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

	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(ctx))

	state, stateDB, _ := makeState(t, 1, 1)
	stateStore := sm.NewStore(stateDB)
	blockStore := store.NewBlockStore(dbm.NewMemDB())
	mp := &mpmocks.Mempool{}
	mp.On("TxStore").Return(nil)
	blockExec := sm.NewBlockExecutor(
		stateStore,
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
