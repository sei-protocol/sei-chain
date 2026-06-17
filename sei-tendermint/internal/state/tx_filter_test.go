package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestTxFilter(t *testing.T) {
	genDoc := randomGenesisDoc()
	genDoc.ConsensusParams.Block.MaxBytes = 3000
	genDoc.ConsensusParams.Evidence.MaxBytes = 1500

	// Max size of Txs is much smaller than size of block,
	// since we need to account for commits and evidence.
	testCases := []struct {
		tx    types.Tx
		isErr bool
	}{
		{types.Tx(tmrand.Bytes(2155)), false},
		{types.Tx(tmrand.Bytes(2156)), true},
		{types.Tx(tmrand.Bytes(3000)), true},
	}

	for i, tc := range testCases {
		state, err := sm.MakeGenesisState(genDoc)
		require.NoError(t, err)

		constraints := sm.TxConstraintsForState(state)
		require.NoError(t, err)
		txSize := types.ComputeProtoSizeForTxs([]types.Tx{tc.tx})
		if tc.isErr {
			assert.Greater(t, txSize, constraints.MaxDataBytes, "#%v", i)
		} else {
			assert.LessOrEqual(t, txSize, constraints.MaxDataBytes, "#%v", i)
		}
	}
}
