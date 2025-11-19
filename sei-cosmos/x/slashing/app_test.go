package slashing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	seiapp "github.com/sei-protocol/sei-chain/app"
)

var (
	priv1 = secp256k1.GenPrivKey()
	addr1 = seitypes.AccAddress(priv1.PubKey().Address())

	valKey  = ed25519.GenPrivKey()
	valAddr = seitypes.AccAddress(valKey.PubKey().Address())
)

func checkValidator(t *testing.T, app *seiapp.App, _ seitypes.AccAddress, expFound bool) stakingtypes.Validator {
	ctxCheck := app.BaseApp.NewContext(true, tmproto.Header{})
	validator, found := app.StakingKeeper.GetValidator(ctxCheck, seitypes.ValAddress(addr1))
	require.Equal(t, expFound, found)
	return validator
}

func checkValidatorSigningInfo(t *testing.T, app *seiapp.App, addr seitypes.ConsAddress, expFound bool) types.ValidatorSigningInfo {
	ctxCheck := app.BaseApp.NewContext(true, tmproto.Header{})
	signingInfo, found := app.SlashingKeeper.GetValidatorSigningInfo(ctxCheck, addr)
	require.Equal(t, expFound, found)
	return signingInfo
}

func TestSlashingMsgs(t *testing.T) {
	genTokens := sdk.TokensFromConsensusPower(42, sdk.DefaultPowerReduction)
	bondTokens := sdk.TokensFromConsensusPower(10, sdk.DefaultPowerReduction)
	genCoin := sdk.NewCoin(sdk.DefaultBondDenom, genTokens)
	bondCoin := sdk.NewCoin(sdk.DefaultBondDenom, bondTokens)

	acc1 := &authtypes.BaseAccount{
		Address: addr1.String(),
	}
	accs := authtypes.GenesisAccounts{acc1}
	balances := []banktypes.Balance{
		{
			Address: addr1.String(),
			Coins:   sdk.Coins{genCoin},
		},
	}

	app := seiapp.SetupWithGenesisAccounts(t, accs, balances...)
	seiapp.CheckBalance(t, app, addr1, sdk.Coins{genCoin})

	description := stakingtypes.NewDescription("foo_moniker", "", "", "", "")
	commission := stakingtypes.NewCommissionRates(sdk.NewDecWithPrec(5, 2), sdk.NewDecWithPrec(5, 2), sdk.ZeroDec())

	createValidatorMsg, err := stakingtypes.NewMsgCreateValidator(
		seitypes.ValAddress(addr1), valKey.PubKey(), bondCoin, description, commission, sdk.OneInt(),
	)
	require.NoError(t, err)

	header := tmproto.Header{Height: app.LastBlockHeight() + 1}
	txGen := seiapp.MakeEncodingConfig().TxConfig
	_, _, err = seiapp.SignCheckDeliver(t, txGen, app.BaseApp, header, []seitypes.Msg{createValidatorMsg}, "", []uint64{0}, []uint64{0}, true, true, priv1)
	require.NoError(t, err)
	seiapp.CheckBalance(t, app, addr1, sdk.Coins{genCoin.Sub(bondCoin)})

	header = tmproto.Header{Height: app.LastBlockHeight() + 1}
	app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: app.LastBlockHeight() + 1})

	validator := checkValidator(t, app, addr1, true)
	require.Equal(t, seitypes.ValAddress(addr1).String(), validator.OperatorAddress)
	require.Equal(t, stakingtypes.Bonded, validator.Status)
	require.True(sdk.IntEq(t, bondTokens, validator.BondedTokens()))
	unjailMsg := &types.MsgUnjail{ValidatorAddr: seitypes.ValAddress(addr1).String()}

	checkValidatorSigningInfo(t, app, seitypes.ConsAddress(valAddr), true)

	// unjail should fail with unknown validator
	header = tmproto.Header{Height: app.LastBlockHeight() + 1}
	_, res, err := seiapp.SignCheckDeliver(t, txGen, app.BaseApp, header, []seitypes.Msg{unjailMsg}, "", []uint64{0}, []uint64{1}, false, false, priv1)
	require.Error(t, err)
	require.Nil(t, res)
	require.True(t, errors.Is(types.ErrValidatorNotJailed, err))
}
