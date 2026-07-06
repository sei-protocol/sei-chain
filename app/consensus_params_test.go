package app

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

func TestGetFinalizeBlockResponsePropagatesFullConsensusParams(t *testing.T) {
	propose := 300 * time.Millisecond
	proposeDelta := 50 * time.Millisecond
	vote := 50 * time.Millisecond
	voteDelta := 50 * time.Millisecond
	commit := 200 * time.Millisecond
	precision := 505 * time.Millisecond
	messageDelay := 12 * time.Second

	consensusParamUpdates := &tmproto.ConsensusParams{
		Block: &tmproto.BlockParams{
			MaxBytes:      22020096,
			MaxGas:        12500000,
			MinTxsInBlock: 0,
			MaxGasWanted:  50000000,
		},
		Evidence: &tmproto.EvidenceParams{
			MaxAgeNumBlocks: 100000,
			MaxAgeDuration:  48 * time.Hour,
			MaxBytes:        1048576,
		},
		Validator: &tmproto.ValidatorParams{
			PubKeyTypes: []string{"ed25519"},
		},
		Version: &tmproto.VersionParams{
			AppVersion: 1,
		},
		Synchrony: &tmproto.SynchronyParams{
			Precision:    &precision,
			MessageDelay: &messageDelay,
		},
		Timeout: &tmproto.TimeoutParams{
			Propose:             &propose,
			ProposeDelta:        &proposeDelta,
			Vote:                &vote,
			VoteDelta:           &voteDelta,
			Commit:              &commit,
			BypassCommitTimeout: false,
		},
		Abci: &tmproto.ABCIParams{
			VoteExtensionsEnableHeight: 123,
			RecheckTx:                  true,
		},
	}

	app := &App{}
	resp := app.getFinalizeBlockResponse(
		[]byte("hash"),
		nil,
		nil,
		types.ResponseEndBlock{},
		consensusParamUpdates,
	)

	require.Equal(t, consensusParamUpdates.Block, resp.ConsensusParamUpdates.Block)
	require.Equal(t, consensusParamUpdates.Evidence, resp.ConsensusParamUpdates.Evidence)
	require.Equal(t, consensusParamUpdates.Validator, resp.ConsensusParamUpdates.Validator)
	require.Equal(t, consensusParamUpdates.Version, resp.ConsensusParamUpdates.Version)
	require.Equal(t, consensusParamUpdates.Synchrony, resp.ConsensusParamUpdates.Synchrony)
	require.Equal(t, consensusParamUpdates.Timeout, resp.ConsensusParamUpdates.Timeout)
	require.Equal(t, consensusParamUpdates.Abci, resp.ConsensusParamUpdates.Abci)
}
