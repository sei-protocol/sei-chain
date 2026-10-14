package wasmbinding

import (
	"fmt"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

var legacyBurnContract = sdk.AccAddress("legacy-burn-contract")

// legacyBurnTokens returns the burn_tokens custom message a contract built on
// sei-cosmwasm sends for amount of denom.
func legacyBurnTokens(denom, amount string) wasmvmtypes.CosmosMsg {
	return wasmvmtypes.CosmosMsg{Custom: []byte(fmt.Sprintf(`{"burn_tokens":{"amount":{"denom":%q,"amount":%q}}}`, denom, amount))}
}

func TestLegacyBurnTokensBurnsTheContractsOwnBalance(t *testing.T) {
	testWrapper, _ := SetupWasmbindingTest(t)
	ctx, bank := testWrapper.Ctx, testWrapper.App.BankKeeper
	denom := "factory/" + legacyBurnContract.String() + "/stake"
	mintLegacyBurnBalance(t, bank, ctx, sdk.NewInt64Coin(denom, 100))

	_, _, err := wasmbinding.LegacyBurnTokensHandler(bank).
		DispatchMsg(ctx, legacyBurnContract, "", legacyBurnTokens(denom, "40"), wasmvmtypes.MessageInfo{}, wasmtypes.CodeInfo{})
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt64Coin(denom, 60), bank.GetBalance(ctx, legacyBurnContract, denom))
	require.Equal(t, sdk.NewInt64Coin(denom, 60), bank.GetSupply(ctx, denom))
}

func TestLegacyBurnTokensRefusesWhatItCannotBurn(t *testing.T) {
	testWrapper, _ := SetupWasmbindingTest(t)
	ctx, bank := testWrapper.Ctx, testWrapper.App.BankKeeper
	own := "factory/" + legacyBurnContract.String() + "/stake"
	foreign := "factory/" + sdk.AccAddress("another-denom-creatr").String() + "/stake"
	mintLegacyBurnBalance(t, bank, ctx, sdk.NewInt64Coin(own, 100))
	mintLegacyBurnBalance(t, bank, ctx, sdk.NewInt64Coin(foreign, 100))
	mintLegacyBurnBalance(t, bank, ctx, sdk.NewInt64Coin("usei", 100))

	for _, tc := range []struct {
		name  string
		msg   wasmvmtypes.CosmosMsg
		error *sdkerrors.Error
	}{
		{"another creator's denom", legacyBurnTokens(foreign, "1"), sdkerrors.ErrUnauthorized},
		{"a native denom", legacyBurnTokens("usei", "1"), sdkerrors.ErrUnauthorized},
		{"a zero amount", legacyBurnTokens(own, "0"), sdkerrors.ErrInvalidCoins},
		{"more than the balance", legacyBurnTokens(own, "101"), sdkerrors.ErrInsufficientFunds},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := wasmbinding.LegacyBurnTokensHandler(bank).
				DispatchMsg(ctx, legacyBurnContract, "", tc.msg, wasmvmtypes.MessageInfo{}, wasmtypes.CodeInfo{})
			require.ErrorIs(t, err, tc.error)
		})
	}
	for _, denom := range []string{own, foreign, "usei"} {
		require.Equal(t, sdk.NewInt64Coin(denom, 100), bank.GetBalance(ctx, legacyBurnContract, denom), denom)
	}
}

func TestLegacyBurnTokensLeavesOtherMessagesToTheNextHandler(t *testing.T) {
	testWrapper, _ := SetupWasmbindingTest(t)
	handler := wasmbinding.LegacyBurnTokensHandler(testWrapper.App.BankKeeper)
	for _, msg := range []wasmvmtypes.CosmosMsg{
		{},
		{Bank: &wasmvmtypes.BankMsg{Burn: &wasmvmtypes.BurnMsg{Amount: wasmvmtypes.Coins{{Denom: "usei", Amount: "1"}}}}},
		{Custom: []byte(`{"mint_tokens":{"amount":{"denom":"factory/creator/subdenom","amount":"1"}}}`)},
		{Custom: []byte(`{"call_evm":{"to":"0x0000000000000000000000000000000000000001","data":""}}`)},
		{Custom: []byte(`not json`)},
	} {
		_, _, err := handler.DispatchMsg(testWrapper.Ctx, legacyBurnContract, "", msg, wasmvmtypes.MessageInfo{}, wasmtypes.CodeInfo{})
		require.ErrorIs(t, err, wasmtypes.ErrUnknownMsg, string(msg.Custom))
	}
}

// TestCustomMessageHandlerServesLegacyBurnTokens requires the Sei Wasm message
// handler to burn for burn_tokens while still refusing the other retired
// tokenfactory messages.
func TestCustomMessageHandlerServesLegacyBurnTokens(t *testing.T) {
	testWrapper, _ := SetupWasmbindingTest(t)
	ctx, bank := testWrapper.Ctx, testWrapper.App.BankKeeper
	denom := "factory/" + legacyBurnContract.String() + "/stake"
	mintLegacyBurnBalance(t, bank, ctx, sdk.NewInt64Coin(denom, 100))
	messenger := wasmbinding.CustomMessageHandler(
		testWrapper.App.MsgServiceRouter(), bank, &testWrapper.App.EvmKeeper, testWrapper.App.AppCodec())

	_, _, err := messenger.DispatchMsg(ctx, legacyBurnContract, "", legacyBurnTokens(denom, "25"), wasmvmtypes.MessageInfo{}, wasmtypes.CodeInfo{})
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt64Coin(denom, 75), bank.GetSupply(ctx, denom))

	mint := wasmvmtypes.CosmosMsg{Custom: []byte(fmt.Sprintf(`{"mint_tokens":{"amount":{"denom":%q,"amount":"1"}}}`, denom))}
	_, _, err = messenger.DispatchMsg(ctx, legacyBurnContract, "", mint, wasmvmtypes.MessageInfo{}, wasmtypes.CodeInfo{})
	require.Equal(t, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Wasm Message"}, err)
	require.Equal(t, sdk.NewInt64Coin(denom, 75), bank.GetSupply(ctx, denom))
}

func mintLegacyBurnBalance(t *testing.T, bank interface {
	MintCoins(sdk.Context, string, sdk.Coins) error
	SendCoinsFromModuleToAccount(sdk.Context, string, sdk.AccAddress, sdk.Coins) error
}, ctx sdk.Context, coin sdk.Coin) {
	t.Helper()
	coins := sdk.NewCoins(coin)
	require.NoError(t, bank.MintCoins(ctx, minttypes.ModuleName, coins))
	require.NoError(t, bank.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, legacyBurnContract, coins))
}
