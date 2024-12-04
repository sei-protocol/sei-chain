package keeper

import (
	"fmt"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

type (
	Keeper struct {
		storeKey sdk.StoreKey

		paramSpace paramtypes.Subspace

		accountKeeper types.AccountKeeper
		bankKeeper    types.BankKeeper
		distrKeeper   types.DistrKeeper
	}
)

// NewKeeper returns a new instance of the x/tokenfactory keeper
func NewKeeper(
	_ codec.Codec,
	storeKey sdk.StoreKey,
	paramSpace paramtypes.Subspace,
	accountKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper,
	distrKeeper types.DistrKeeper,
) Keeper {
	if !paramSpace.HasKeyTable() {
		paramSpace = paramSpace.WithKeyTable(types.ParamKeyTable())
	}

	return Keeper{
		storeKey:   storeKey,
		paramSpace: paramSpace,

		accountKeeper: accountKeeper,
		bankKeeper:    bankKeeper,
		distrKeeper:   distrKeeper,
	}
}

// Logger returns a logger for the x/tokenfactory module
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GetDenomPrefixStore returns the substore for a specific denom
func (k Keeper) GetDenomPrefixStore(ctx sdk.Context, denom string) sdk.KVStore {
	store := ctx.KVStore(k.storeKey)
	return prefix.NewStore(store, types.GetDenomPrefixStore(denom))
}

// GetCreatorPrefixStore returns the substore for a specific creator address
func (k Keeper) GetCreatorPrefixStore(ctx sdk.Context, creator string) sdk.KVStore {
	store := ctx.KVStore(k.storeKey)
	return prefix.NewStore(store, types.GetCreatorPrefix(creator))
}

// GetCreatorsPrefixStore returns the substore that contains a list of creators
func (k Keeper) GetCreatorsPrefixStore(ctx sdk.Context) sdk.KVStore {
	store := ctx.KVStore(k.storeKey)
	return prefix.NewStore(store, types.GetCreatorsPrefix())
}

// CreateModuleAccount creates a module account with minting and burning capabilities
// This account isn't intended to store any coins,
// it purely mints and burns them on behalf of the admin of respective denoms,
// and sends to the relevant address.
func (k Keeper) CreateModuleAccount(ctx sdk.Context) {
	moduleAcc := authtypes.NewEmptyModuleAccount(types.ModuleName, authtypes.Minter, authtypes.Burner)
	k.accountKeeper.SetModuleAccount(ctx, moduleAcc)
}

func (k Keeper) GetDenomAllowListMaxSize(ctx sdk.Context) int32 {
	var demomAllowListMaxSize int32
	k.paramSpace.Get(ctx, types.DenomAllowListMaxSizeKey, &demomAllowListMaxSize)
	return demomAllowListMaxSize
}
