package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestClaimTransfersAllBalances(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	senderSei, _ := testkeeper.MockAddressPair()
	claimerSei, claimerEvm := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, claimerSei, claimerEvm)

	coins := sdk.NewCoins(
		sdk.NewCoin("usei", sdk.NewInt(10)),
		sdk.NewCoin("ufoo", sdk.NewInt(5)),
	)
	require.NoError(t, k.BankKeeper().AddCoins(ctx, senderSei, coins, true))

	err := k.Claim(ctx, types.NewMsgClaim(senderSei, claimerEvm))
	require.NoError(t, err)
	require.True(t, k.BankKeeper().GetAllBalances(ctx, senderSei).IsZero())
	require.True(t, coins.IsEqual(k.BankKeeper().GetAllBalances(ctx, claimerSei)))
}

func TestClaimWithNoBalances(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	senderSei, _ := testkeeper.MockAddressPair()
	claimerSei, claimerEvm := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, claimerSei, claimerEvm)

	err := k.Claim(ctx, types.NewMsgClaim(senderSei, claimerEvm))
	require.NoError(t, err)
	require.True(t, k.BankKeeper().GetAllBalances(ctx, claimerSei).IsZero())
}

func TestClaimInvalidSender(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	msg := &types.MsgClaim{Sender: "invalid", Claimer: common.Address{}.Hex()}
	require.Error(t, k.Claim(ctx, msg))
}
