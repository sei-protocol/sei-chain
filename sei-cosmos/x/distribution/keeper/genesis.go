package keeper

import (
	"fmt"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
)

// InitGenesis sets distribution information for genesis
func (k Keeper) InitGenesis(ctx sdk.Context, data types.GenesisState) {
	var moduleHoldings sdk.DecCoins

	k.SetFeePool(ctx, data.FeePool)
	k.SetParams(ctx, data.Params)

	for _, dwi := range data.DelegatorWithdrawInfos {
		delegatorAddress := sdk.MustAccAddressFromBech32(dwi.DelegatorAddress)
		withdrawAddress := sdk.MustAccAddressFromBech32(dwi.WithdrawAddress)
		k.SetDelegatorWithdrawAddr(ctx, delegatorAddress, withdrawAddress)
	}

	var previousProposer sdk.ConsAddress
	if data.PreviousProposer != "" {
		var err error
		previousProposer, err = sdk.ConsAddressFromBech32(data.PreviousProposer)
		if err != nil {
			panic(err)
		}
	}

	k.SetPreviousProposerConsAddr(ctx, previousProposer)

	for _, rew := range data.OutstandingRewards {
		valAddr, err := sdk.ValAddressFromBech32(rew.ValidatorAddress)
		if err != nil {
			panic(err)
		}
		k.SetValidatorOutstandingRewards(ctx, valAddr, types.ValidatorOutstandingRewards{Rewards: rew.OutstandingRewards})
		moduleHoldings = moduleHoldings.Add(rew.OutstandingRewards...)
	}
	for _, acc := range data.ValidatorAccumulatedCommissions {
		valAddr, err := sdk.ValAddressFromBech32(acc.ValidatorAddress)
		if err != nil {
			panic(err)
		}
		k.SetValidatorAccumulatedCommission(ctx, valAddr, acc.Accumulated)
	}
	for _, his := range data.ValidatorHistoricalRewards {
		valAddr, err := sdk.ValAddressFromBech32(his.ValidatorAddress)
		if err != nil {
			panic(err)
		}
		k.SetValidatorHistoricalRewards(ctx, valAddr, his.Period, his.Rewards)
	}
	for _, cur := range data.ValidatorCurrentRewards {
		valAddr, err := sdk.ValAddressFromBech32(cur.ValidatorAddress)
		if err != nil {
			panic(err)
		}
		k.SetValidatorCurrentRewards(ctx, valAddr, cur.Rewards)
	}
	for _, del := range data.DelegatorStartingInfos {
		valAddr, err := sdk.ValAddressFromBech32(del.ValidatorAddress)
		if err != nil {
			panic(err)
		}
		delegatorAddress := sdk.MustAccAddressFromBech32(del.DelegatorAddress)

		k.SetDelegatorStartingInfo(ctx, valAddr, delegatorAddress, del.StartingInfo)
	}
	for _, evt := range data.ValidatorSlashEvents {
		valAddr, err := sdk.ValAddressFromBech32(evt.ValidatorAddress)
		if err != nil {
			panic(err)
		}
		k.SetValidatorSlashEvent(ctx, valAddr, evt.Height, evt.Period, evt.ValidatorSlashEvent)
	}

	moduleHoldings = moduleHoldings.Add(data.FeePool.CommunityPool...)
	moduleHoldingsInt, _ := moduleHoldings.TruncateDecimal()

	// check if the module account exists
	moduleAcc := k.GetDistributionAccount(ctx)
	if moduleAcc == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.ModuleName))
	}

	balances := k.bankKeeper.GetAllBalances(ctx, moduleAcc.GetAddress())
	if balances.IsZero() {
		k.authKeeper.SetModuleAccount(ctx, moduleAcc)
	}
	if !balances.IsEqual(moduleHoldingsInt) {
		panic(fmt.Sprintf("distribution module balance does not match the module holdings: %s <-> %s", balances, moduleHoldingsInt))
	}
}
