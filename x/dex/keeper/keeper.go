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
		cdc                 codec.BinaryCodec
		storeKey            sdk.StoreKey
		memKey              sdk.StoreKey
		paramstore          paramtypes.Subspace
		Orders              map[string]map[string]*dexcache.Orders
		EpochKeeper         epochkeeper.Keeper
		OrderPlacements     map[string]map[string]*dexcache.OrderPlacements
		DepositInfo         map[string]*dexcache.DepositInfo
		BankKeeper          bankkeeper.Keeper
		OrderCancellations  map[string]map[string]*dexcache.OrderCancellations
		LiquidationRequests map[string]map[string]string
		WasmKeeper          wasm.Keeper
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
		cdc:                 cdc,
		storeKey:            storeKey,
		memKey:              memKey,
		paramstore:          ps,
		Orders:              map[string]map[string]*dexcache.Orders{},
		OrderPlacements:     map[string]map[string]*dexcache.OrderPlacements{},
		DepositInfo:         map[string]*dexcache.DepositInfo{},
		OrderCancellations:  map[string]map[string]*dexcache.OrderCancellations{},
		LiquidationRequests: map[string]map[string]string{},
	}
}

func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey,
	memKey sdk.StoreKey,
	ps paramtypes.Subspace,
	epochKeeper epochkeeper.Keeper,
	bankKeeper bankkeeper.Keeper,
	wasmKeeper wasm.Keeper,
) *Keeper {
	// set KeyTable if it has not already been set
	if !ps.HasKeyTable() {
		ps = ps.WithKeyTable(types.ParamKeyTable())
	}
	return &Keeper{
		cdc:                 cdc,
		storeKey:            storeKey,
		memKey:              memKey,
		paramstore:          ps,
		Orders:              map[string]map[string]*dexcache.Orders{},
		EpochKeeper:         epochKeeper,
		OrderPlacements:     map[string]map[string]*dexcache.OrderPlacements{},
		DepositInfo:         map[string]*dexcache.DepositInfo{},
		BankKeeper:          bankKeeper,
		OrderCancellations:  map[string]map[string]*dexcache.OrderCancellations{},
		LiquidationRequests: map[string]map[string]string{},
		WasmKeeper:          wasmKeeper,
	}
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}
