//go:build !mock_chain_validation

// These tests assert that block validation halts on a bad commit or excess
// evidence. mock_chain_validation swallows those sentinels (ErrLastCommitVerify,
// ErrTooMuchEvidence) -- mock_block_validation does not -- so they are excluded
// only under that build. TestValidateBlockHeader exercises a sentinel both mock
// builds swallow and lives in validation_header_default_test.go.
package state_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/mocks"
	statefactory "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	testfactory "github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const validationTestsStopHeight int64 = 10

func TestValidateBlockCommit(t *testing.T) {
	ctx := t.Context()

	app := &testApp{}

	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(ctx))

	state, stateDB, privVals := makeState(t, 1, 1)
	stateStore := sm.NewStore(stateDB)
	proxyApp := proxy.New(app, proxy.NopMetrics())
	mp := makeTxMempool(t, proxyApp)

	blockStore := store.NewBlockStore(dbm.NewMemDB())
	blockExec := sm.NewBlockExecutor(
		stateStore,
		proxyApp,
		mp,
		sm.EmptyEvidencePool{},
		blockStore,
		eventBus,
		sm.NopMetrics(),
		types.DefaultConsensusPolicy(),
	)
	lastCommit := &types.Commit{}
	wrongSigsCommit := &types.Commit{Height: 1}
	badPrivVal := types.NewMockPV()

	for height := int64(1); height < validationTestsStopHeight; height++ {
		proposerAddr := state.Validators.GetProposer().Address
		if height > 1 {
			/*
				#2589: ensure state.LastValidators.VerifyCommit fails here
			*/
			// should be height-1 instead of height
			wrongHeightVote, err := testfactory.MakeVote(
				ctx,
				privVals[proposerAddr.String()],
				chainID,
				1,
				height,
				0,
				2,
				state.LastBlockID,
				time.Now(),
			)
			require.NoError(t, err)
			wrongHeightCommit := &types.Commit{
				Height:     wrongHeightVote.Height,
				Round:      wrongHeightVote.Round,
				BlockID:    state.LastBlockID,
				Signatures: []types.CommitSig{wrongHeightVote.CommitSig()},
			}
			block := statefactory.MakeBlock(state, height, wrongHeightCommit)
			if err := blockExec.ValidateBlock(ctx, state, block); !errors.As(err, &types.ErrInvalidCommitHeight{}) {
				t.Fatalf("expected ErrInvalidCommitHeight at height %d but got: %v", height, err)
			}
			/*
				#2589: test len(block.LastCommit.Signatures) == state.LastValidators.Size()
			*/
			block = statefactory.MakeBlock(state, height, wrongSigsCommit)
			if err := blockExec.ValidateBlock(ctx, state, block); !errors.As(err, &types.ErrInvalidCommitSignatures{}) {
				t.Fatalf("expected ErrInvalidCommitSignatures at height %d but got: %v", height, err)
			}
		}

		/*
			A good block passes
		*/
		var blockID types.BlockID
		state, blockID, lastCommit = makeAndCommitGoodBlock(
			ctx,
			t,
			state,
			height,
			lastCommit,
			proposerAddr,
			blockExec,
			privVals,
			nil,
		)

		/*
			wrongSigsCommit is fine except for the extra bad precommit
		*/
		goodVote, err := testfactory.MakeVote(
			ctx,
			privVals[proposerAddr.String()],
			chainID,
			1,
			height,
			0,
			2,
			blockID,
			time.Now(),
		)
		require.NoError(t, err)
		bpvPubKey, err := badPrivVal.GetPubKey(ctx)
		require.NoError(t, err)

		badVote := &types.Vote{
			ValidatorAddress: bpvPubKey.Address(),
			ValidatorIndex:   0,
			Height:           height,
			Round:            0,
			Timestamp:        tmtime.Now(),
			Type:             tmproto.PrecommitType,
			BlockID:          blockID,
		}

		g := goodVote.ToProto()
		b := badVote.ToProto()

		err = badPrivVal.SignVote(ctx, chainID, g)
		require.NoError(t, err, "height %d", height)
		err = badPrivVal.SignVote(ctx, chainID, b)
		require.NoError(t, err, "height %d", height)

		goodVote.Signature = utils.Some(utils.OrPanic1(crypto.SigFromBytes(g.Signature)))
		badVote.Signature = utils.Some(utils.OrPanic1(crypto.SigFromBytes(b.Signature)))

		wrongSigsCommit = &types.Commit{
			Height:     goodVote.Height,
			Round:      goodVote.Round,
			BlockID:    blockID,
			Signatures: []types.CommitSig{goodVote.CommitSig(), badVote.CommitSig()},
		}
	}
}

func TestValidateBlockEvidence(t *testing.T) {
	ctx := t.Context()

	app := &testApp{}

	state, stateDB, privVals := makeState(t, 4, 1)
	stateStore := sm.NewStore(stateDB)
	blockStore := store.NewBlockStore(dbm.NewMemDB())
	defaultEvidenceTime := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)

	evpool := &mocks.EvidencePool{}
	evpool.On("CheckEvidence", ctx, mock.AnythingOfType("types.EvidenceList")).Return(nil)
	evpool.On("Update", ctx, mock.AnythingOfType("state.State"), mock.AnythingOfType("types.EvidenceList")).Return()
	evpool.On("ABCIEvidence", mock.AnythingOfType("int64"), mock.AnythingOfType("[]types.Evidence")).Return(
		[]abci.Misbehavior{})

	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(ctx))
	proxyApp := proxy.New(app, proxy.NopMetrics())
	mp := makeTxMempool(t, proxyApp)

	state.ConsensusParams.Evidence.MaxBytes = 1000
	blockExec := sm.NewBlockExecutor(
		stateStore,
		proxyApp,
		mp,
		evpool,
		blockStore,
		eventBus,
		sm.NopMetrics(),
		types.DefaultConsensusPolicy(),
	)
	lastCommit := &types.Commit{}

	for height := int64(1); height < validationTestsStopHeight; height++ {
		proposerAddr := state.Validators.GetProposer().Address
		maxBytesEvidence := state.ConsensusParams.Evidence.MaxBytes
		if height > 1 {
			/*
				A block with too much evidence fails
			*/
			evidence := make([]types.Evidence, 0)
			var currentBytes int64
			// more bytes than the maximum allowed for evidence
			for currentBytes <= maxBytesEvidence {
				newEv, err := types.NewMockDuplicateVoteEvidenceWithValidator(ctx, height, time.Now(),
					privVals[proposerAddr.String()], chainID)
				require.NoError(t, err)
				evidence = append(evidence, newEv)
				currentBytes += int64(len(newEv.Bytes()))
			}
			block := state.MakeBlock(height, testfactory.MakeNTxs(height, 10), lastCommit, evidence, proposerAddr)

			if err := blockExec.ValidateBlock(ctx, state, block); !errors.As(err, &types.ErrEvidenceOverflow{}) {
				t.Fatalf("expected error to be of type ErrEvidenceOverflow at height %d but got %v", height, err)
			}
		}

		/*
			A good block with several pieces of good evidence passes
		*/
		evidence := make([]types.Evidence, 0)
		var currentBytes int64
		// precisely the amount of allowed evidence
		for {
			newEv, err := types.NewMockDuplicateVoteEvidenceWithValidator(ctx, height, defaultEvidenceTime,
				privVals[proposerAddr.String()], chainID)
			require.NoError(t, err)
			currentBytes += int64(len(newEv.Bytes()))
			if currentBytes >= maxBytesEvidence {
				break
			}
			evidence = append(evidence, newEv)
		}

		state, _, lastCommit = makeAndCommitGoodBlock(
			ctx,
			t,
			state,
			height,
			lastCommit,
			proposerAddr,
			blockExec,
			privVals,
			evidence,
		)

	}
}
