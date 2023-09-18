package keeper

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

type Keeper struct {
	storeKey   sdk.StoreKey
	Paramstore paramtypes.Subspace

	evmChainID *big.Int

	bankKeeper    bankkeeper.Keeper
	accountKeeper *authkeeper.AccountKeeper
}

func (k *Keeper) ChainID() *big.Int {
	return k.evmChainID
}
