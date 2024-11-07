package keeper

import (
	"bytes"
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

type Keeper interface {
	InitGenesis(sdk.Context, *types.GenesisState)
	ExportGenesis(sdk.Context) *types.GenesisState

	GetAccount(ctx sdk.Context, address sdk.AccAddress, denom string) (types.Account, bool)
	SetAccount(ctx sdk.Context, address sdk.AccAddress, denom string, account types.Account)

	GetParams(ctx sdk.Context) types.Params
	SetParams(ctx sdk.Context, params types.Params)

	CreateModuleAccount(ctx sdk.Context)
}

type BaseKeeper struct {
	storeKey sdk.StoreKey

	cdc codec.Codec

	paramSpace    paramtypes.Subspace
	accountKeeper types.AccountKeeper
}

func (k BaseKeeper) TestQuery(ctx context.Context, request *types.TestQueryRequest) (*types.TestQueryResponse, error) {
	//TODO: This is not a real gRPC query. This was added to the query.proto file as a placeholder. We should remove this and add the real queries once we better define query.proto.
	panic("implement me")
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

func (k BaseKeeper) GetAccount(ctx sdk.Context, address sdk.AccAddress, denom string) (types.Account, bool) {
	store := ctx.KVStore(k.storeKey)
	key := types.GetAccountKey(address, denom)
	if !store.Has(key) {
		return types.Account{}, false
	}

	var ctAccount types.CtAccount
	bz := store.Get(key)
	k.cdc.MustUnmarshal(bz, &ctAccount) // Unmarshal the bytes back into the CtAccount object
	account, err := ctAccount.FromProto()
	if err != nil {
		return types.Account{}, false
	}
	return *account, true
}

func (k BaseKeeper) SetAccount(ctx sdk.Context, address sdk.AccAddress, denom string, account types.Account) {
	store := ctx.KVStore(k.storeKey)
	key := types.GetAccountKey(address, denom)
	ctAccount := types.NewCtAccount(&account)
	bz := k.cdc.MustMarshal(ctAccount) // Marshal the Account object into bytes
	store.Set(key, bz)                 // Store the serialized account under the key
}

// Logger returns a logger for the x/confidentialtransfers module
func (k BaseKeeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GetAccountsForAddress iterates over all accounts associated with a given address
// and returns a mapping of denom:account
func (k BaseKeeper) GetAccountsForAddress(ctx sdk.Context, address sdk.AccAddress) (map[string]*types.Account, error) {
	// Create a prefix store scoped to the address
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.GetAddressPrefix(address))

	// Iterate over all keys in the prefix store
	iterator := sdk.KVStorePrefixIterator(store, []byte{})
	defer iterator.Close()

	accounts := make(map[string]*types.Account)
	for ; iterator.Valid(); iterator.Next() {
		var ctaccount types.CtAccount
		k.cdc.MustUnmarshal(iterator.Value(), &ctaccount)
		account, err := ctaccount.FromProto()
		if err != nil {
			return nil, err
		}

		// Extract the denom from the key
		key := iterator.Key()
		// Key format: account|<addr>|<denom>, so denom starts after "account|<addr>|"
		denom := string(bytes.TrimPrefix(key, types.GetAddressPrefix(address)))

		accounts[denom] = account
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
