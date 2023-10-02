package state

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type StateDBImpl struct {
	ctx sdk.Context
	// If err is not nil at the end of the execution, the transaction will be rolled
	// back.
	err error

	// changes to EVM module balance because of balance movements. If this value
	// does not equal to the change in EVM module account balance minus the minted
	// amount at the end of the execution, the transaction should fail.
	deficit *big.Int
	// the number of base tokens minted to temporarily facilitate balance movements.
	// At the end of execution, `minted` number of base tokens will be burnt.
	minted *big.Int

	initialModuleBalance *big.Int

	k *keeper.Keeper
}

func NewStateDBImpl(ctx sdk.Context, k *keeper.Keeper) *StateDBImpl {
	return &StateDBImpl{
		ctx:                  ctx,
		k:                    k,
		deficit:              big.NewInt(0),
		minted:               big.NewInt(0),
		initialModuleBalance: k.GetModuleBalance(ctx),
	}
}
