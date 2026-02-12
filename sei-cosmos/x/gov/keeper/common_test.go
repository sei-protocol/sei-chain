package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking"
	stakingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

var (
	TestProposal          = types.NewTextProposal("Test", "description", false)
	TestExpeditedProposal = types.NewTextProposal("Test", "description", true)
)

func createValidators(t *testing.T, ctx sdk.Context, app *seiapp.App, powers []int64) ([]sdk.AccAddress, []sdk.ValAddress) {
	addrs := seiapp.AddTestAddrsIncremental(app, ctx, 5, sdk.NewInt(30000000))
	valAddrs := seiapp.ConvertAddrsToValAddrs(addrs)
	pks := seiapp.CreateTestPubKeys(5)
	cdc := seiapp.MakeEncodingConfig().Marshaler

	app.StakingKeeper = stakingkeeper.NewKeeper(
		cdc,
		app.GetKey(stakingtypes.StoreKey),
		app.AccountKeeper,
		app.BankKeeper,
		app.GetSubspace(stakingtypes.ModuleName),
	)

	val1, err := stakingtypes.NewValidator(valAddrs[0], pks[0], stakingtypes.Description{})
	require.NoError(t, err)
	val2, err := stakingtypes.NewValidator(valAddrs[1], pks[1], stakingtypes.Description{})
	require.NoError(t, err)
	val3, err := stakingtypes.NewValidator(valAddrs[2], pks[2], stakingtypes.Description{})
	require.NoError(t, err)

	app.StakingKeeper.SetValidator(ctx, val1)
	app.StakingKeeper.SetValidator(ctx, val2)
	app.StakingKeeper.SetValidator(ctx, val3)
	app.StakingKeeper.SetValidatorByConsAddr(ctx, val1)
	app.StakingKeeper.SetValidatorByConsAddr(ctx, val2)
	app.StakingKeeper.SetValidatorByConsAddr(ctx, val3)
	app.StakingKeeper.SetNewValidatorByPowerIndex(ctx, val1)
	app.StakingKeeper.SetNewValidatorByPowerIndex(ctx, val2)
	app.StakingKeeper.SetNewValidatorByPowerIndex(ctx, val3)

	_, _ = app.StakingKeeper.Delegate(ctx, addrs[0], app.StakingKeeper.TokensFromConsensusPower(ctx, powers[0]), stakingtypes.Unbonded, val1, true)
	_, _ = app.StakingKeeper.Delegate(ctx, addrs[1], app.StakingKeeper.TokensFromConsensusPower(ctx, powers[1]), stakingtypes.Unbonded, val2, true)
	_, _ = app.StakingKeeper.Delegate(ctx, addrs[2], app.StakingKeeper.TokensFromConsensusPower(ctx, powers[2]), stakingtypes.Unbonded, val3, true)

	_ = staking.EndBlocker(ctx, app.StakingKeeper)

	return addrs, valAddrs
}
