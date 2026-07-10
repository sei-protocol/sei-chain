package keeper_test

import (
	"testing"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/teststaking"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"

	types "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
)

// setupRewardsWithSlash creates a bonded validator with a self-delegation, slashes
// it, and allocates rewards, leaving accrued (un-incremented) rewards so both the
// simple and slash-event reward paths are exercised. It returns the context, the
// validator, the delegation, and the initial allocation.
func setupRewardsWithSlash(t *testing.T) (sdk.Context, *seiapp.App, stakingtypes.ValidatorI, stakingtypes.DelegationI, sdk.Int, sdk.ValAddress) {
	t.Helper()
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addr := seiapp.AddTestAddrs(app, ctx, 2, sdk.NewInt(100000000))
	valAddrs := seiapp.ConvertAddrsToValAddrs(addr)
	tstaking := teststaking.NewHelper(t, ctx, app.StakingKeeper)

	tstaking.Commission = stakingtypes.NewCommissionRates(sdk.NewDecWithPrec(5, 1), sdk.NewDecWithPrec(5, 1), sdk.NewDec(0))
	valPower := int64(100)
	tstaking.CreateValidatorWithValPower(valAddrs[0], valConsPk1, valPower, true)

	staking.EndBlocker(ctx, app.StakingKeeper)
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)

	// slash so the reward computation walks a slash event
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 3)
	app.StakingKeeper.Slash(ctx, valConsAddr1, ctx.BlockHeight(), valPower, sdk.NewDecWithPrec(5, 1))
	val := app.StakingKeeper.Validator(ctx, valAddrs[0])

	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 3)
	initial := app.StakingKeeper.TokensFromConsensusPower(ctx, 10)
	tokens := sdk.DecCoins{{Denom: sdk.DefaultBondDenom, Amount: initial.ToDec()}}
	app.DistrKeeper.AllocateTokensToValidator(ctx, val, tokens)

	del := app.StakingKeeper.Delegation(ctx, sdk.AccAddress(valAddrs[0]), valAddrs[0])
	return ctx, app, val, del, initial, valAddrs[0]
}

// TestCalculateDelegationRewardsReadOnlyMatchesWritePath asserts the read-only
// reward computation (1) returns exactly what the IncrementValidatorPeriod +
// CalculateDelegationRewards sequence returns, and (2) mutates no distribution
// state.
func TestCalculateDelegationRewardsReadOnlyMatchesWritePath(t *testing.T) {
	ctx, app, val, del, initial, valAddr := setupRewardsWithSlash(t)

	// snapshot the state IncrementValidatorPeriod would mutate
	periodBefore := app.DistrKeeper.GetValidatorCurrentRewards(ctx, valAddr).Period
	refCountBefore := app.DistrKeeper.GetValidatorHistoricalReferenceCount(ctx)

	readOnly := app.DistrKeeper.CalculateDelegationRewardsReadOnly(ctx, val, del)

	// (2) no state mutated by the read-only path
	require.Equal(t, periodBefore, app.DistrKeeper.GetValidatorCurrentRewards(ctx, valAddr).Period,
		"read-only reward calc must not increment the validator period")
	require.Equal(t, refCountBefore, app.DistrKeeper.GetValidatorHistoricalReferenceCount(ctx),
		"read-only reward calc must not change the historical reference count")

	// (1) equals the write path, computed on an isolated (cached) context
	cctx, _ := ctx.CacheContext()
	endingPeriod := app.DistrKeeper.IncrementValidatorPeriod(cctx, val)
	writePath := app.DistrKeeper.CalculateDelegationRewards(cctx, val, del, endingPeriod)
	require.Equal(t, writePath, readOnly, "read-only rewards must equal the write-path rewards")

	// sanity: the slashing scenario yields half the allocation (other half is commission)
	require.Equal(t, sdk.DecCoins{{Denom: sdk.DefaultBondDenom, Amount: initial.QuoRaw(2).ToDec()}}, readOnly)
}

// TestDelegationTotalRewardsQueryIsReadOnly asserts the gRPC query no longer
// mutates distribution state and is idempotent across repeated calls (before the
// fix, each call incremented the validator period).
func TestDelegationTotalRewardsQueryIsReadOnly(t *testing.T) {
	ctx, app, _, _, _, valAddr := setupRewardsWithSlash(t)

	periodBefore := app.DistrKeeper.GetValidatorCurrentRewards(ctx, valAddr).Period
	refCountBefore := app.DistrKeeper.GetValidatorHistoricalReferenceCount(ctx)

	req := &types.QueryDelegationTotalRewardsRequest{
		DelegatorAddress: sdk.AccAddress(valAddr).String(),
	}

	resp1, err := app.DistrKeeper.DelegationTotalRewards(sdk.WrapSDKContext(ctx), req)
	require.NoError(t, err)
	require.False(t, resp1.Total.IsZero(), "test should have non-zero rewards")

	require.Equal(t, periodBefore, app.DistrKeeper.GetValidatorCurrentRewards(ctx, valAddr).Period,
		"query must not increment the validator period")
	require.Equal(t, refCountBefore, app.DistrKeeper.GetValidatorHistoricalReferenceCount(ctx),
		"query must not change the historical reference count")

	// idempotent: a second query returns identical totals and still no mutation
	resp2, err := app.DistrKeeper.DelegationTotalRewards(sdk.WrapSDKContext(ctx), req)
	require.NoError(t, err)
	require.Equal(t, resp1.Total, resp2.Total)
	require.Equal(t, periodBefore, app.DistrKeeper.GetValidatorCurrentRewards(ctx, valAddr).Period)
}
