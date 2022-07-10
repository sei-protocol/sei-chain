package keeper

import (
	"fmt"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	epochkeeper "github.com/sei-protocol/sei-chain/x/epoch/keeper"
)

type (
	Keeper struct {
		Cdc         codec.BinaryCodec
		storeKey    sdk.StoreKey
		memKey      sdk.StoreKey
		Paramstore  paramtypes.Subspace
		EpochKeeper epochkeeper.Keeper
		BankKeeper  bankkeeper.Keeper
		WasmKeeper  wasm.Keeper
		MemState    *dexcache.MemState
	}
)

func NewPlainKeeper(
	cdc codec.BinaryCodec,
	storeKey,
	memKey sdk.StoreKey,
	ps paramtypes.Subspace,
) *Keeper {
	// set KeyTable if it has not already been set
	if !ps.HasKeyTable() {
		ps = ps.WithKeyTable(types.ParamKeyTable())
	}
	return &Keeper{
		Cdc:        cdc,
		storeKey:   storeKey,
		memKey:     memKey,
		Paramstore: ps,
		MemState:   dexcache.NewMemState(),
	}
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey,
	memKey sdk.StoreKey,
	ps paramtypes.Subspace,
	epochKeeper epochkeeper.Keeper,
	bankKeeper bankkeeper.Keeper,
) *Keeper {
	// set KeyTable if it has not already been set
	if !ps.HasKeyTable() {
		ps = ps.WithKeyTable(types.ParamKeyTable())
	}
	return &Keeper{
		Cdc:         cdc,
		storeKey:    storeKey,
		memKey:      memKey,
		Paramstore:  ps,
		EpochKeeper: epochKeeper,
		BankKeeper:  bankKeeper,
		MemState:    dexcache.NewMemState(),
	}
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

func (k Keeper) GetStoreKey() sdk.StoreKey {
	return k.storeKey
}

func (k *Keeper) SetWasmKeeper(wasmKeeper *wasm.Keeper) {
	k.WasmKeeper = *wasmKeeper
}
