package keeper_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
)

func TestSetWithdrawAddr(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addr := seiapp.AddTestAddrs(app, ctx, 2, sdk.NewInt(1000000000))

	params := app.DistrKeeper.GetParams(ctx)
	params.WithdrawAddrEnabled = false
	app.DistrKeeper.SetParams(ctx, params)

	err := app.DistrKeeper.SetWithdrawAddr(ctx, addr[0], addr[1])
	require.NotNil(t, err)

	params.WithdrawAddrEnabled = true
	app.DistrKeeper.SetParams(ctx, params)

	err = app.DistrKeeper.SetWithdrawAddr(ctx, addr[0], addr[1])
	require.Nil(t, err)

	associatedAddr := seiapp.AddTestAddrs(app, ctx, 1, sdk.NewInt(1000000000))[0]
	evmAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	castAddr := sdk.AccAddress(evmAddr[:])
	app.EvmKeeper.SetAddressMapping(ctx, associatedAddr, evmAddr)
	require.Error(t, app.DistrKeeper.SetWithdrawAddr(ctx, addr[0], castAddr))

	require.Error(t, app.DistrKeeper.SetWithdrawAddr(ctx, addr[0], distrAcc.GetAddress()))
}

func TestAfterValidatorRemovedFallsBackForInvalidWithdrawAddress(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	params := app.DistrKeeper.GetParams(ctx)
	params.WithdrawAddrEnabled = true
	app.DistrKeeper.SetParams(ctx, params)

	addr := seiapp.AddTestAddrs(app, ctx, 2, sdk.NewInt(1000000000))
	valAddr := seiapp.ConvertAddrsToValAddrs(addr[:1])[0]
	valAccAddr := sdk.AccAddress(valAddr)
	associatedAddr := addr[1]
	evmAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")
	castAddr := sdk.AccAddress(evmAddr[:])

	require.True(t, app.BankKeeper.CanSendTo(ctx, castAddr))
	require.NoError(t, app.DistrKeeper.SetWithdrawAddr(ctx, valAccAddr, castAddr))
	require.Equal(t, castAddr.String(), app.DistrKeeper.GetDelegatorWithdrawAddr(ctx, valAccAddr).String())

	app.EvmKeeper.SetAddressMapping(ctx, associatedAddr, evmAddr)
	require.False(t, app.BankKeeper.CanSendTo(ctx, castAddr))
	require.Equal(t, valAccAddr.String(), app.DistrKeeper.GetDelegatorWithdrawAddr(ctx, valAccAddr).String())

	commission := sdk.DecCoins{sdk.NewDecCoin("usei", sdk.NewInt(10))}
	coins := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))
	distrAcc := app.DistrKeeper.GetDistributionAccount(ctx)
	require.NoError(t, apptesting.FundModuleAccount(app.BankKeeper, ctx, distrAcc.GetName(), coins))
	app.AccountKeeper.SetModuleAccount(ctx, distrAcc)

	app.DistrKeeper.SetValidatorOutstandingRewards(ctx, valAddr, types.ValidatorOutstandingRewards{Rewards: commission})
	app.DistrKeeper.SetValidatorAccumulatedCommission(ctx, valAddr, types.ValidatorAccumulatedCommission{Commission: commission})

	balanceBefore := app.BankKeeper.GetBalance(ctx, valAccAddr, "usei")
	require.NotPanics(t, func() {
		app.DistrKeeper.Hooks().AfterValidatorRemoved(ctx, sdk.ConsAddress{}, valAddr)
	})
	balanceAfter := app.BankKeeper.GetBalance(ctx, valAccAddr, "usei")
	require.Equal(t, balanceBefore.Amount.Add(sdk.NewInt(10)), balanceAfter.Amount)
}

// TestAfterValidatorRemovedRoutesToCommunityPoolForUnreceivableValidator covers the
// case where the validator operator address itself cannot receive funds — its EVM
// address was re-associated (e.g. via associatePubKey) away from the direct-cast Sei
// address it was created under. The withdraw-address fallback in GetDelegatorWithdrawAddr
// resolves back to that same unreceivable operator address, so the commission
// force-withdraw fails. AfterValidatorRemoved runs during EndBlock, so it must not panic:
// the commission is routed to the community pool instead, which conserves value because
// the coins already back the distribution module account.
func TestAfterValidatorRemovedRoutesToCommunityPoolForUnreceivableValidator(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	// The validator operator is the direct-cast Sei address of an EVM address.
	evmAddr := common.HexToAddress("0x3333333333333333333333333333333333333333")
	castAddr := sdk.AccAddress(evmAddr[:])
	valAddr := sdk.ValAddress(castAddr)
	valAccAddr := sdk.AccAddress(valAddr) // == castAddr

	require.True(t, app.BankKeeper.CanSendTo(ctx, castAddr))

	// Re-associate the EVM address to a different true Sei address, mirroring
	// associatePubKey after a validator was created under the direct-cast address.
	associatedAddr := seiapp.AddTestAddrs(app, ctx, 1, sdk.NewInt(1000000000))[0]
	app.EvmKeeper.SetAddressMapping(ctx, associatedAddr, evmAddr)

	// The operator/delegator address can no longer receive funds, and the
	// withdraw-address fallback resolves back to that same unreceivable address.
	require.False(t, app.BankKeeper.CanSendTo(ctx, castAddr))
	require.Equal(t, valAccAddr.String(), app.DistrKeeper.GetDelegatorWithdrawAddr(ctx, valAccAddr).String())

	commission := sdk.DecCoins{sdk.NewDecCoin("usei", sdk.NewInt(10))}
	coins := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))
	distrAcc := app.DistrKeeper.GetDistributionAccount(ctx)
	require.NoError(t, apptesting.FundModuleAccount(app.BankKeeper, ctx, distrAcc.GetName(), coins))
	app.AccountKeeper.SetModuleAccount(ctx, distrAcc)

	app.DistrKeeper.SetValidatorOutstandingRewards(ctx, valAddr, types.ValidatorOutstandingRewards{Rewards: commission})
	app.DistrKeeper.SetValidatorAccumulatedCommission(ctx, valAddr, types.ValidatorAccumulatedCommission{Commission: commission})

	communityBefore := app.DistrKeeper.GetFeePool(ctx).CommunityPool.AmountOf("usei")
	moduleBalanceBefore := app.BankKeeper.GetBalance(ctx, distrAcc.GetAddress(), "usei")

	require.NotPanics(t, func() {
		app.DistrKeeper.Hooks().AfterValidatorRemoved(ctx, sdk.ConsAddress{}, valAddr)
	})

	// The commission could not be paid out, so it stays in the distribution module
	// account and is accounted to the community pool. No value leaves the module and
	// the unreceivable operator address receives nothing.
	communityAfter := app.DistrKeeper.GetFeePool(ctx).CommunityPool.AmountOf("usei")
	require.Equal(t, communityBefore.Add(sdk.NewDec(10)), communityAfter)
	require.True(t, app.BankKeeper.GetBalance(ctx, castAddr, "usei").IsZero())
	require.Equal(t, moduleBalanceBefore, app.BankKeeper.GetBalance(ctx, distrAcc.GetAddress(), "usei"))
}

func TestWithdrawValidatorCommission(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	valCommission := sdk.DecCoins{
		sdk.NewDecCoinFromDec("mytoken", sdk.NewDec(5).Quo(sdk.NewDec(4))),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(3).Quo(sdk.NewDec(2))),
	}

	addr := seiapp.AddTestAddrs(app, ctx, 1, sdk.NewInt(1000000000))
	valAddrs := seiapp.ConvertAddrsToValAddrs(addr)

	// set module account coins
	distrAcc := app.DistrKeeper.GetDistributionAccount(ctx)
	coins := sdk.NewCoins(sdk.NewCoin("mytoken", sdk.NewInt(2)), sdk.NewCoin("usei", sdk.NewInt(2)))
	require.NoError(t, apptesting.FundModuleAccount(app.BankKeeper, ctx, distrAcc.GetName(), coins))

	app.AccountKeeper.SetModuleAccount(ctx, distrAcc)

	// check initial balance
	balance := app.BankKeeper.GetAllBalances(ctx, sdk.AccAddress(valAddrs[0]))
	expTokens := app.StakingKeeper.TokensFromConsensusPower(ctx, 1000)
	expCoins := sdk.NewCoins(sdk.NewCoin("usei", expTokens))
	require.Equal(t, expCoins, balance)

	// set outstanding rewards
	app.DistrKeeper.SetValidatorOutstandingRewards(ctx, valAddrs[0], types.ValidatorOutstandingRewards{Rewards: valCommission})

	// set commission
	app.DistrKeeper.SetValidatorAccumulatedCommission(ctx, valAddrs[0], types.ValidatorAccumulatedCommission{Commission: valCommission})

	// withdraw commission
	_, err := app.DistrKeeper.WithdrawValidatorCommission(ctx, valAddrs[0])
	require.NoError(t, err)

	// check balance increase
	balance = app.BankKeeper.GetAllBalances(ctx, sdk.AccAddress(valAddrs[0]))
	require.Equal(t, sdk.NewCoins(
		sdk.NewCoin("mytoken", sdk.NewInt(1)),
		sdk.NewCoin("usei", expTokens.AddRaw(1)),
	), balance)

	// check remainder
	remainder := app.DistrKeeper.GetValidatorAccumulatedCommission(ctx, valAddrs[0]).Commission
	require.Equal(t, sdk.DecCoins{
		sdk.NewDecCoinFromDec("mytoken", sdk.NewDec(1).Quo(sdk.NewDec(4))),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(1).Quo(sdk.NewDec(2))),
	}, remainder)

	require.True(t, true)
}

func TestGetTotalRewards(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	valCommission := sdk.DecCoins{
		sdk.NewDecCoinFromDec("mytoken", sdk.NewDec(5).Quo(sdk.NewDec(4))),
		sdk.NewDecCoinFromDec("usei", sdk.NewDec(3).Quo(sdk.NewDec(2))),
	}

	addr := seiapp.AddTestAddrs(app, ctx, 2, sdk.NewInt(1000000000))
	valAddrs := seiapp.ConvertAddrsToValAddrs(addr)

	app.DistrKeeper.SetValidatorOutstandingRewards(ctx, valAddrs[0], types.ValidatorOutstandingRewards{Rewards: valCommission})
	app.DistrKeeper.SetValidatorOutstandingRewards(ctx, valAddrs[1], types.ValidatorOutstandingRewards{Rewards: valCommission})

	expectedRewards := valCommission.MulDec(sdk.NewDec(2))
	totalRewards := app.DistrKeeper.GetTotalRewards(ctx)

	require.Equal(t, expectedRewards, totalRewards)
}

func TestFundCommunityPool(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addr := seiapp.AddTestAddrs(app, ctx, 2, sdk.ZeroInt())

	amount := sdk.NewCoins(sdk.NewInt64Coin("usei", 100))
	require.NoError(t, apptesting.FundAccount(app.BankKeeper, ctx, addr[0], amount))

	initPool := app.DistrKeeper.GetFeePool(ctx)
	assert.Empty(t, initPool.CommunityPool)

	err := app.DistrKeeper.FundCommunityPool(ctx, amount, addr[0])
	assert.Nil(t, err)

	assert.Equal(t, initPool.CommunityPool.Add(sdk.NewDecCoinsFromCoins(amount...)...), app.DistrKeeper.GetFeePool(ctx).CommunityPool)
	assert.Empty(t, app.BankKeeper.GetAllBalances(ctx, addr[0]))
}

// TestFundCommunityPoolRejectsOutOfRangeAmount verifies that funding the
// community pool with an amount too large to convert to a whole-coin Dec is
// rejected at the conversion boundary and leaves the stored fee pool
// unchanged.
func TestFundCommunityPoolRejectsOutOfRangeAmount(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addr := seiapp.AddTestAddrs(app, ctx, 1, sdk.ZeroInt())

	maxAmt := sdk.NewIntFromBigInt(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)))
	coins := sdk.NewCoins(sdk.NewCoin("bigcoin", maxAmt))
	require.NoError(t, apptesting.FundAccount(app.BankKeeper, ctx, addr[0], coins))

	require.Panics(t, func() {
		_ = app.DistrKeeper.FundCommunityPool(ctx, coins, addr[0])
	}, "funding the community pool with an out-of-range amount must be rejected")

	// The stored fee pool must be unchanged after the rejected attempt.
	require.NotPanics(t, func() { app.DistrKeeper.GetFeePool(ctx) })
	require.True(t, app.DistrKeeper.GetFeePool(ctx).CommunityPool.IsZero())
}
