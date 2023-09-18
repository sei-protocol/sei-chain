package keeper

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type Keeper struct {
	storeKey   sdk.StoreKey
	Paramstore paramtypes.Subspace

	evmChainID *big.Int

	bankKeeper    bankkeeper.Keeper
	accountKeeper *authkeeper.AccountKeeper
}

func NewKeeper(
	storeKey sdk.StoreKey, paramstore paramtypes.Subspace, evmChainID *big.Int,
	bankKeeper bankkeeper.Keeper, accountKeeper *authkeeper.AccountKeeper) *Keeper {
	if !paramstore.HasKeyTable() {
		paramstore = paramstore.WithKeyTable(types.ParamKeyTable())
	}
	return &Keeper{
		storeKey:      storeKey,
		Paramstore:    paramstore,
		evmChainID:    evmChainID,
		bankKeeper:    bankKeeper,
		accountKeeper: accountKeeper,
	}
}

func (k *Keeper) ChainID() *big.Int {
	return k.evmChainID
}
