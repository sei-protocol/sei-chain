package keeper

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

var _ Keeper = (*BaseKeeper)(nil)

type Keeper interface {
	InitGenesis(sdk.Context, *types.GenesisState)
	ExportGenesis(sdk.Context) *types.GenesisState

	SetAccount(ctx sdk.Context, address sdk.AccAddress, denom string, account types.Account)

	GetParams(ctx sdk.Context) types.Params
	SetParams(ctx sdk.Context, params types.Params)

	CreateModuleAccount(ctx sdk.Context)

	types.QueryServer
}

type BaseKeeper struct {
	storeKey sdk.StoreKey

	cdc codec.Codec

	paramSpace    paramtypes.Subspace
	accountKeeper types.AccountKeeper
}

// NewKeeper returns a new instance of the x/confidentialtransfers keeper
func NewKeeper(
	codec codec.Codec,
	storeKey sdk.StoreKey,
	paramSpace paramtypes.Subspace,
	accountKeeper types.AccountKeeper,
) Keeper {

	if !paramSpace.HasKeyTable() {
		paramSpace = paramSpace.WithKeyTable(types.ParamKeyTable())
	}

	return BaseKeeper{
		cdc:           codec,
		storeKey:      storeKey,
		accountKeeper: accountKeeper,
		paramSpace:    paramSpace,
	}
}

func (k BaseKeeper) getAccount(ctx sdk.Context, address sdk.AccAddress, denom string) (types.Account, bool) {
	ctAccount, found := k.getCtAccount(ctx, address, denom)
	if !found {
		return types.Account{}, false
	}
	account, err := ctAccount.FromProto()
	if err != nil {
		return types.Account{}, false
	}
	return *account, true
}

func (k BaseKeeper) getCtAccount(ctx sdk.Context, address sdk.AccAddress, denom string) (types.CtAccount, bool) {
	store := k.getAccountStoreForAddress(ctx, address)
	key := []byte(denom)
	if !store.Has(key) {
		return types.CtAccount{}, false
	}

	var ctAccount types.CtAccount
	bz := store.Get(key)
	k.cdc.MustUnmarshal(bz, &ctAccount) // Unmarshal the bytes back into the CtAccount object
	return ctAccount, true
}

func (k BaseKeeper) SetAccount(ctx sdk.Context, address sdk.AccAddress, denom string, account types.Account) {
	store := k.getAccountStoreForAddress(ctx, address)
	ctAccount := types.NewCtAccount(&account)
	bz := k.cdc.MustMarshal(ctAccount) // Marshal the Account object into bytes
	store.Set([]byte(denom), bz)       // Store the serialized account under denom name as key
}

// Logger returns a logger for the x/confidentialtransfers module
func (k BaseKeeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GetAccountsForAddress iterates over all accounts associated with a given address
// and returns a mapping of denom:account
func (k BaseKeeper) GetAccountsForAddress(ctx sdk.Context, address sdk.AccAddress) (map[string]*types.Account, error) {
	// Create a prefix store scoped to the address
	store := k.getAccountStoreForAddress(ctx, address)

	// Iterate over all keys in the prefix store
	iterator := sdk.KVStorePrefixIterator(store, []byte{})
	defer func(iterator sdk.Iterator) {
		err := iterator.Close()
		if err != nil {
			k.Logger(ctx).Error("failed to close iterator", "error", err)
		}
	}(iterator)

	accounts := make(map[string]*types.Account)
	for ; iterator.Valid(); iterator.Next() {
		var ctAccount types.CtAccount
		k.cdc.MustUnmarshal(iterator.Value(), &ctAccount)
		account, err := ctAccount.FromProto()
		if err != nil {
			return nil, err
		}

		key := iterator.Key()

		accounts[string(key)] = account
	}

	return accounts, nil
}

func (k BaseKeeper) GetParams(ctx sdk.Context) (params types.Params) {
	k.paramSpace.GetParamSet(ctx, &params)
	return params
}

// SetParams sets the total set of bank parameters.
func (k BaseKeeper) SetParams(ctx sdk.Context, params types.Params) {
	k.paramSpace.SetParamSet(ctx, &params)
}

func (k BaseKeeper) CreateModuleAccount(ctx sdk.Context) {
	moduleAcc := authtypes.NewEmptyModuleAccount(types.ModuleName, authtypes.Minter, authtypes.Burner)
	k.accountKeeper.SetModuleAccount(ctx, moduleAcc)
}

func (k BaseKeeper) getAccountStore(ctx sdk.Context) prefix.Store {
	store := ctx.KVStore(k.storeKey)
	return prefix.NewStore(store, types.AccountsKey)
}

func (k BaseKeeper) getAccountStoreForAddress(ctx sdk.Context, addr sdk.AccAddress) prefix.Store {
	store := ctx.KVStore(k.storeKey)
	return prefix.NewStore(store, types.GetAddressPrefix(addr))
}
