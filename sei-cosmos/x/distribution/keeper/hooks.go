package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// Wrapper struct
type Hooks struct {
	k Keeper
}

var _ stakingtypes.StakingHooks = Hooks{}

// Create new distribution hooks
func (k Keeper) Hooks() Hooks { return Hooks{k} }

// initialize validator distribution record
func (h Hooks) AfterValidatorCreated(ctx sdk.Context, valAddr sdk.ValAddress) {
	val := h.k.stakingKeeper.Validator(ctx, valAddr)
	h.k.initializeValidator(ctx, val)
}

// AfterValidatorRemoved performs clean up after a validator is removed
func (h Hooks) AfterValidatorRemoved(ctx sdk.Context, _ sdk.ConsAddress, valAddr sdk.ValAddress) {
	// fetch outstanding
	outstanding := h.k.GetValidatorOutstandingRewardsCoins(ctx, valAddr)

	// force-withdraw commission
	commission := h.k.GetValidatorAccumulatedCommission(ctx, valAddr).Commission
	if !commission.IsZero() {
		// subtract from outstanding
		outstanding = outstanding.Sub(commission)

		// split into integral & remainder
		coins, remainder := commission.TruncateDecimal()

		// remainder to community pool
		feePool := h.k.GetFeePool(ctx)
		feePool.CommunityPool = feePool.CommunityPool.Add(remainder...)
		h.k.SetFeePool(ctx, feePool)

		// add to validator account
		if !coins.IsZero() {
			accAddr := sdk.AccAddress(valAddr)
			withdrawAddr := h.k.GetDelegatorWithdrawAddr(ctx, accAddr)

			// GetDelegatorWithdrawAddr falls back to the delegator (accAddr) when the
			// configured withdraw address cannot receive funds, but that fallback can
			// itself be unable to receive — e.g. accAddr is an EVM address whose Sei
			// mapping was re-associated to a different address, so CanAddressReceive
			// rejects it. This hook runs in EndBlock, so attempting the send and
			// panicking on the resulting bank error would halt the chain. Check
			// receivability first: when the recipient cannot receive, route the
			// commission to the community pool instead. The coins already back the
			// distribution module account (where community pool funds are held), so this
			// conserves value and avoids the partial module-account debit that a failed
			// SendCoins leaves behind.
			if h.k.canReceiveWithdrawAddr(ctx, withdrawAddr) {
				if err := h.k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, withdrawAddr, coins); err != nil {
					panic(err)
				}
			} else {
				feePool := h.k.GetFeePool(ctx)
				feePool.CommunityPool = feePool.CommunityPool.Add(sdk.NewDecCoinsFromCoins(coins...)...)
				h.k.SetFeePool(ctx, feePool)
			}
		}
	}

	// Add outstanding to community pool
	// The validator is removed only after it has no more delegations.
	// This operation sends only the remaining dust to the community pool.
	feePool := h.k.GetFeePool(ctx)
	feePool.CommunityPool = feePool.CommunityPool.Add(outstanding...)
	h.k.SetFeePool(ctx, feePool)

	// delete outstanding
	h.k.DeleteValidatorOutstandingRewards(ctx, valAddr)

	// remove commission record
	h.k.DeleteValidatorAccumulatedCommission(ctx, valAddr)

	// clear slashes
	h.k.DeleteValidatorSlashEvents(ctx, valAddr)

	// clear historical rewards
	h.k.DeleteValidatorHistoricalRewards(ctx, valAddr)

	// clear current rewards
	h.k.DeleteValidatorCurrentRewards(ctx, valAddr)
}

// increment period
func (h Hooks) BeforeDelegationCreated(ctx sdk.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) {
	val := h.k.stakingKeeper.Validator(ctx, valAddr)
	h.k.IncrementValidatorPeriod(ctx, val)
}

// withdraw delegation rewards (which also increments period)
func (h Hooks) BeforeDelegationSharesModified(ctx sdk.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) {
	val := h.k.stakingKeeper.Validator(ctx, valAddr)
	del := h.k.stakingKeeper.Delegation(ctx, delAddr, valAddr)

	if _, err := h.k.withdrawDelegationRewards(ctx, val, del); err != nil {
		panic(err)
	}
}

// create new delegation period record
func (h Hooks) AfterDelegationModified(ctx sdk.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) {
	h.k.initializeDelegation(ctx, valAddr, delAddr)
}

// record the slash event
func (h Hooks) BeforeValidatorSlashed(ctx sdk.Context, valAddr sdk.ValAddress, fraction sdk.Dec) {
	h.k.updateValidatorSlashFraction(ctx, valAddr, fraction)
}

func (h Hooks) BeforeValidatorModified(_ sdk.Context, _ sdk.ValAddress)                         {}
func (h Hooks) AfterValidatorBonded(_ sdk.Context, _ sdk.ConsAddress, _ sdk.ValAddress)         {}
func (h Hooks) AfterValidatorBeginUnbonding(_ sdk.Context, _ sdk.ConsAddress, _ sdk.ValAddress) {}
func (h Hooks) BeforeDelegationRemoved(_ sdk.Context, _ sdk.AccAddress, _ sdk.ValAddress)       {}
