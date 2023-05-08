package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestNewSettlementEntry(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(100)
	sudoFinalizeBlockMsg := types.NewSettlementEntry(
		ctx,
		1,
		"TEST_ACCOUNT",
		types.PositionDirection_LONG,
		"USDC",
		"ATOM",
		sdk.MustNewDecFromStr("1"),
		sdk.MustNewDecFromStr("2"),
		sdk.MustNewDecFromStr("3"),
		types.OrderType_MARKET,
	)

	require.Equal(t, "Long", sudoFinalizeBlockMsg.PositionDirection)
	require.Equal(t, "Market", sudoFinalizeBlockMsg.OrderType)
	require.Equal(t, uint64(100), sudoFinalizeBlockMsg.Height)
}
