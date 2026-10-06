package core

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

func TestValidatorsOmittedHeightBeforeFirstBlockUsesInitialHeight(t *testing.T) {
	env := newAutobahnBroadcastEnv(t)

	got, err := env.Validators(t.Context(), &coretypes.RequestValidators{})
	require.NoError(t, err)
	require.Equal(t, env.GenDoc.InitialHeight, got.BlockHeight)
	require.Greater(t, got.Total, 0)
	require.Equal(t, got.Total, len(got.Validators))
}

func TestValidatorsExplicitZeroIsRejected(t *testing.T) {
	env := newAutobahnBroadcastEnv(t)
	zero := coretypes.Int64(0)

	_, err := env.Validators(t.Context(), &coretypes.RequestValidators{Height: &zero})
	require.ErrorIs(t, err, coretypes.ErrZeroOrNegativeHeight)
}
