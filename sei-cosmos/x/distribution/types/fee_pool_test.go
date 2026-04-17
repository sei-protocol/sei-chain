package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
)

func TestValidateGenesis(t *testing.T) {

	fp := types.InitialFeePool()
	require.Nil(t, fp.ValidateGenesis())

	fp2 := types.FeePool{CommunityPool: sdk.DecCoins{{Denom: "usei", Amount: sdk.NewDec(-1)}}}
	require.NotNil(t, fp2.ValidateGenesis())
}
