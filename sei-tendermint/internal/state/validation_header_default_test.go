//go:build !mock_chain_validation && !mock_block_validation

// TestValidateBlockHeader asserts that any header-field defect fails validation.
// Its table includes an AppHash mutation, and AppHash is swallowed by both
// mock_chain_validation and mock_block_validation, so this test is default-build
// only. (validationTestsStopHeight is defined in validation_test.go.)
package state_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	statefactory "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestValidateBlockHeader(t *testing.T) {
	ctx := t.Context()

	app := &testApp{}

	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(ctx))

	state, stateDB, privVals := makeState(t, 3, 1)
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

	// some bad values
	wrongHash := crypto.Checksum([]byte("this hash is wrong")).Bytes()
	wrongVersion1 := state.Version.Consensus
	wrongVersion1.Block += 2
	wrongVersion2 := state.Version.Consensus
	wrongVersion2.App += 2

	// Manipulation of any header field causes failure.
	testCases := []struct {
		name          string
		malleateBlock func(block *types.Block)
	}{
		{"Version wrong1", func(block *types.Block) { block.Version = wrongVersion1 }},
		{"Version wrong2", func(block *types.Block) { block.Version = wrongVersion2 }},
		{"ChainID wrong", func(block *types.Block) { block.ChainID = "not-the-real-one" }},
		{"Height wrong", func(block *types.Block) { block.Height += 10 }},
		{"Time wrong", func(block *types.Block) { block.Time = block.Time.Add(-time.Second * 1) }},

		{"LastBlockID wrong", func(block *types.Block) { block.LastBlockID.PartSetHeader.Total += 10 }},
		{"LastCommitHash wrong", func(block *types.Block) { block.LastCommitHash = wrongHash }},
		{"DataHash wrong", func(block *types.Block) { block.DataHash = wrongHash }},

		{"ValidatorsHash wrong", func(block *types.Block) { block.ValidatorsHash = wrongHash }},
		{"NextValidatorsHash wrong", func(block *types.Block) { block.NextValidatorsHash = wrongHash }},
		{"ConsensusHash wrong", func(block *types.Block) { block.ConsensusHash = wrongHash }},
		{"AppHash wrong", func(block *types.Block) { block.AppHash = wrongHash }},
		{"LastResultsHash wrong", func(block *types.Block) { block.LastResultsHash = wrongHash }},

		{"EvidenceHash wrong", func(block *types.Block) { block.EvidenceHash = wrongHash }},
		{"Proposer wrong", func(block *types.Block) {
			block.ProposerAddress = ed25519.GenerateSecretKey().Public().Address()
		}},
		{"Proposer invalid", func(block *types.Block) { block.ProposerAddress = []byte("wrong size") }},

		{"first LastCommit contains signatures", func(block *types.Block) {
			block.LastCommit = &types.Commit{Signatures: []types.CommitSig{types.NewCommitSigAbsent()}}
			block.LastCommitHash = block.LastCommit.Hash()
		}},
	}

	// Build up state for multiple heights
	for height := int64(1); height < validationTestsStopHeight; height++ {
		/*
			Invalid blocks don't pass
		*/
		for _, tc := range testCases {
			block := statefactory.MakeBlock(state, height, lastCommit)
			tc.malleateBlock(block)
			err := blockExec.ValidateBlock(ctx, state, block)
			t.Logf("%s: %v", tc.name, err)
			require.Error(t, err, tc.name)
		}

		/*
			A good block passes
		*/
		state, _, lastCommit = makeAndCommitGoodBlock(ctx, t,
			state, height, lastCommit, state.Validators.GetProposer().Address, blockExec, privVals, nil)
	}

	nextHeight := validationTestsStopHeight
	block := statefactory.MakeBlock(state, nextHeight, lastCommit)
	state.InitialHeight = nextHeight + 1
	err := blockExec.ValidateBlock(ctx, state, block)
	require.Error(t, err, "expected an error when state is ahead of block")
	assert.Contains(t, err.Error(), "lower than initial height")
}
