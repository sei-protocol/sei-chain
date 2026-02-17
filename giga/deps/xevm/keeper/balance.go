package keeper

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
)

func (k *Keeper) GetBalance(ctx sdk.Context, addr sdk.AccAddress) *big.Int {
	denom := k.GetBaseDenom(ctx)
	usei := k.BankKeeper().GetBalance(ctx, addr, denom).Amount
	// Skip LockedCoins: it calls AccountKeeper.GetAccount solely to check for
	// vesting accounts, but standard accounts always return zero locked coins.
	// In the giga executor path, vesting accounts are not used, so we skip the
	// expensive protobuf deserialization (~15GB allocs / 30s).
	wei := k.BankKeeper().GetWeiBalance(ctx, addr)
	return usei.Mul(state.SdkUseiToSweiMultiplier).Add(wei).BigInt()
}
