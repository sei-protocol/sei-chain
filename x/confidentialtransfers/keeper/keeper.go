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
)

// TODO: This is just scaffolding. To be implemented.
type (
	Keeper struct {
		storeKey sdk.StoreKey

		cdc codec.Codec

		accountKeeper types.AccountKeeper
		bankKeeper    types.BankKeeper
	}
)

func (k Keeper) TestQuery(ctx context.Context, request *types.TestQueryRequest) (*types.TestQueryResponse, error) {
	//TODO: This is not a real gRPC query. This was added to the query.proto file as a placeholder. We should remove this and add the real queries once we better define query.proto.
	panic("implement me")
}

// NewKeeper returns a new instance of the x/confidentialtransfers keeper
func NewKeeper(
	codec codec.Codec,
	storeKey sdk.StoreKey,
	ak types.AccountKeeper,
	bk types.BankKeeper,
) Keeper {
	return Keeper{
		cdc:           codec,
		storeKey:      storeKey,
		accountKeeper: ak,
		bankKeeper:    bk,
	}
}

func (k Keeper) GetAccount(ctx sdk.Context, address sdk.AccAddress, denom string) (types.Account, bool) {
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

func (k Keeper) SetAccount(ctx sdk.Context, address sdk.AccAddress, denom string, account *types.Account) {
	store := ctx.KVStore(k.storeKey)
	key := types.GetAccountKey(address, denom)
	ctAccount := types.NewCtAccount(account)
	bz := k.cdc.MustMarshal(ctAccount) // Marshal the Account object into bytes
	store.Set(key, bz)                 // Store the serialized account under the key
}

func (k Keeper) DeleteAccount(ctx sdk.Context, address sdk.AccAddress, denom string) {
	store := ctx.KVStore(k.storeKey)
	key := types.GetAccountKey(address, denom)
	store.Delete(key) // Store the serialized account under the key
}

// Logger returns a logger for the x/confidentialtransfers module
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GetAccountsForAddress iterates over all accounts associated with a given address
// and returns a mapping of denom:account
func (k Keeper) GetAccountsForAddress(ctx sdk.Context, address sdk.AccAddress) (map[string]*types.Account, error) {
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

// CreateModuleAccount creates the module account for confidentialtransfers
func (k Keeper) CreateModuleAccount(ctx sdk.Context) {
	account := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	if account == nil {
		moduleAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
		k.accountKeeper.SetModuleAccount(ctx, moduleAcc)
	}
}
