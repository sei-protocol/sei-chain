package staking_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
	seiapp "github.com/sei-protocol/sei-chain/app"
)

func checkValidator(t *testing.T, app *seiapp.App, addr seitypes.ValAddress, expFound bool) types.Validator {
	ctxCheck := app.BaseApp.NewContext(true, tmproto.Header{})
	validator, found := app.StakingKeeper.GetValidator(ctxCheck, addr)

	require.Equal(t, expFound, found)
	return validator
}

func checkDelegation(
	t *testing.T, app *seiapp.App, delegatorAddr seitypes.AccAddress,
	validatorAddr seitypes.ValAddress, expFound bool, expShares sdk.Dec,
) {

	ctxCheck := app.BaseApp.NewContext(true, tmproto.Header{})
	delegation, found := app.StakingKeeper.GetDelegation(ctxCheck, delegatorAddr, validatorAddr)
	if expFound {
		require.True(t, found)
		require.True(sdk.DecEq(t, expShares, delegation.Shares))

		return
	}

	require.False(t, found)
}

func TestStakingMsgs(t *testing.T) {
	genTokens := sdk.TokensFromConsensusPower(42, sdk.DefaultPowerReduction)
	bondTokens := sdk.TokensFromConsensusPower(10, sdk.DefaultPowerReduction)
	genCoin := sdk.NewCoin(sdk.DefaultBondDenom, genTokens)
	bondCoin := sdk.NewCoin(sdk.DefaultBondDenom, bondTokens)

	acc1 := &authtypes.BaseAccount{Address: addr1.String()}
	acc2 := &authtypes.BaseAccount{Address: addr2.String()}
	accs := authtypes.GenesisAccounts{acc1, acc2}
	balances := []banktypes.Balance{
		{
			Address: addr1.String(),
			Coins:   sdk.Coins{genCoin},
		},
		{
			Address: addr2.String(),
			Coins:   sdk.Coins{genCoin},
		},
	}

	app := seiapp.SetupWithGenesisAccounts(t, accs, balances...)
	seiapp.CheckBalance(t, app, addr1, sdk.Coins{genCoin})
	seiapp.CheckBalance(t, app, addr2, sdk.Coins{genCoin})

	// create validator
	description := types.NewDescription("foo_moniker", "", "", "", "")
	createValidatorMsg, err := types.NewMsgCreateValidator(
		seitypes.ValAddress(addr1), valKey.PubKey(), bondCoin, description, commissionRates, sdk.OneInt(),
	)
	require.NoError(t, err)

	header := tmproto.Header{Height: app.LastBlockHeight() + 1}
	txGen := seiapp.MakeEncodingConfig().TxConfig
	_, _, err = seiapp.SignCheckDeliver(t, txGen, app.BaseApp, header, []seitypes.Msg{createValidatorMsg}, "", []uint64{0}, []uint64{0}, true, true, priv1)
	require.NoError(t, err)
	seiapp.CheckBalance(t, app, addr1, sdk.Coins{genCoin.Sub(bondCoin)})

	header = tmproto.Header{Height: app.LastBlockHeight() + 1}
	app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: app.LastBlockHeight() + 1})

	validator := checkValidator(t, app, seitypes.ValAddress(addr1), true)
	require.Equal(t, seitypes.ValAddress(addr1).String(), validator.OperatorAddress)
	require.Equal(t, types.Bonded, validator.Status)
	require.True(sdk.IntEq(t, bondTokens, validator.BondedTokens()))

	header = tmproto.Header{Height: app.LastBlockHeight() + 1}
	app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: app.LastBlockHeight() + 1})

	// edit the validator
	description = types.NewDescription("bar_moniker", "", "", "", "")
	editValidatorMsg := types.NewMsgEditValidator(seitypes.ValAddress(addr1), description, nil, nil)

	header = tmproto.Header{Height: app.LastBlockHeight() + 1}
	_, _, err = seiapp.SignCheckDeliver(t, txGen, app.BaseApp, header, []seitypes.Msg{editValidatorMsg}, "", []uint64{0}, []uint64{1}, true, true, priv1)
	require.NoError(t, err)

	validator = checkValidator(t, app, seitypes.ValAddress(addr1), true)
	require.Equal(t, description, validator.Description)

	// delegate
	seiapp.CheckBalance(t, app, addr2, sdk.Coins{genCoin})
	delegateMsg := types.NewMsgDelegate(addr2, seitypes.ValAddress(addr1), bondCoin)

	header = tmproto.Header{Height: app.LastBlockHeight() + 1}
	_, _, err = seiapp.SignCheckDeliver(t, txGen, app.BaseApp, header, []seitypes.Msg{delegateMsg}, "", []uint64{1}, []uint64{0}, true, true, priv2)
	require.NoError(t, err)

	seiapp.CheckBalance(t, app, addr2, sdk.Coins{genCoin.Sub(bondCoin)})
	checkDelegation(t, app, addr2, seitypes.ValAddress(addr1), true, bondTokens.ToDec())

	// begin unbonding
	beginUnbondingMsg := types.NewMsgUndelegate(addr2, seitypes.ValAddress(addr1), bondCoin)
	header = tmproto.Header{Height: app.LastBlockHeight() + 1}
	_, _, err = seiapp.SignCheckDeliver(t, txGen, app.BaseApp, header, []seitypes.Msg{beginUnbondingMsg}, "", []uint64{1}, []uint64{1}, true, true, priv2)
	require.NoError(t, err)

	// delegation should exist anymore
	checkDelegation(t, app, addr2, seitypes.ValAddress(addr1), false, sdk.Dec{})

	// balance should be the same because bonding not yet complete
	seiapp.CheckBalance(t, app, addr2, sdk.Coins{genCoin.Sub(bondCoin)})
}
