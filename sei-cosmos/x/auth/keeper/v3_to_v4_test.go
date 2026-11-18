package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	authtestutil "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/testutil"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

func legacyVestingAccount(t *testing.T, typeURL string, accountNumber uint64) authtestutil.LegacyVestingAccount {
	t.Helper()
	pub := secp256k1.GenPrivKey().PubKey()
	account := authtestutil.LegacyVestingAccount{
		TypeURL:          typeURL,
		Base:             types.NewBaseAccount(sdk.AccAddress(pub.Address()), pub, accountNumber, 3),
		OriginalVesting:  sdk.NewCoins(sdk.NewInt64Coin("usei", 1_000), sdk.NewInt64Coin("uatom", 5)),
		DelegatedFree:    sdk.NewCoins(sdk.NewInt64Coin("usei", 10)),
		DelegatedVesting: sdk.NewCoins(sdk.NewInt64Coin("usei", 20)),
		EndTime:          32_503_680_000,
		Admin:            sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String(),
		StartTime:        1_700_000_000,
	}
	if typeURL == authtestutil.LegacyPeriodicVestingAccountTypeURL {
		account.Periods = []authtestutil.LegacyVestingPeriod{
			{Length: 60, Amount: sdk.NewCoins(sdk.NewInt64Coin("usei", 400))},
			{Length: 120, Amount: sdk.NewCoins(sdk.NewInt64Coin("usei", 600), sdk.NewInt64Coin("uatom", 5))},
		}
	}
	return account
}

// pacific1VestingAddresses are the accounts pacific-1 stores under a vesting
// type.
var pacific1VestingAddresses = []string{
	"sei1ga833mj6jc6d2lye6j84stug9h2mpxcxuyw5ak",
	"sei15wfxcaz3phxa7hyl9z9wr064nc7zt6y79ywkvw",
	"sei1570e0uvdywv0wp79mz235v6zzuu69268xhvk6x",
}

// legacyVestingAccountAt is a legacyVestingAccount of typeURL stored at address.
func legacyVestingAccountAt(t *testing.T, typeURL string, address sdk.AccAddress, accountNumber uint64) authtestutil.LegacyVestingAccount {
	t.Helper()
	account := legacyVestingAccount(t, typeURL, accountNumber)
	account.Base = types.NewBaseAccount(address, nil, accountNumber, 3)
	return account
}

func setLegacyVestingAccount(t *testing.T, store sdk.KVStore, account authtestutil.LegacyVestingAccount) {
	t.Helper()
	encoded, err := account.Encode()
	require.NoError(t, err)
	store.Set(types.AddressStoreKey(account.Base.GetAddress()), encoded)
}

func accountStoreEntries(store sdk.KVStore) map[string][]byte {
	iterator := sdk.KVStorePrefixIterator(store, types.AddressStoreKeyPrefix)
	defer func() { _ = iterator.Close() }()
	entries := map[string][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		entries[string(iterator.Key())] = append([]byte(nil), iterator.Value()...)
	}
	return entries
}

func TestMigrate3to4RewritesLegacyVestingAccountsAsBaseAccounts(t *testing.T) {
	app, ctx := createTestApp(false)
	store := ctx.KVStore(app.GetKey(types.StoreKey))

	vesting := make([]authtestutil.LegacyVestingAccount, 0, len(authtestutil.LegacyVestingAccountTypeURLs))
	for i, typeURL := range authtestutil.LegacyVestingAccountTypeURLs {
		account := legacyVestingAccount(t, typeURL, uint64(100+i)) //nolint:gosec // small test index
		setLegacyVestingAccount(t, store, account)
		vesting = append(vesting, account)
	}
	plain := app.AccountKeeper.NewAccountWithAddress(ctx, sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()))
	app.AccountKeeper.SetAccount(ctx, plain)
	app.AccountKeeper.GetModuleAccount(ctx, "migration-test-module")
	before := accountStoreEntries(store)

	m := keeper.NewMigrator(app.AccountKeeper, app.QueryServiceRouter())
	require.NoError(t, m.Migrate3to4(ctx))

	after := accountStoreEntries(store)
	require.Len(t, after, len(before), "the migration added or removed an account")
	for _, account := range vesting {
		key := string(types.AddressStoreKey(account.Base.GetAddress()))
		want, err := app.AccountKeeper.MarshalAccount(account.Base)
		require.NoError(t, err)
		require.Equal(t, want, after[key], "%s was not rewritten as its base account", account.TypeURL)
		delete(before, key)
		delete(after, key)

		got, ok := app.AccountKeeper.GetAccount(ctx, account.Base.GetAddress()).(*types.BaseAccount)
		require.True(t, ok, "%s does not decode as a base account", account.TypeURL)
		require.Equal(t, account.Base.GetAccountNumber(), got.GetAccountNumber())
		require.Equal(t, account.Base.GetSequence(), got.GetSequence())
		require.True(t, account.Base.GetPubKey().Equals(got.GetPubKey()))
	}
	require.Equal(t, before, after, "the migration changed an account that was not a vesting account")
}

func TestMigrate3to4IsANoOpOnceApplied(t *testing.T) {
	app, ctx := createTestApp(false)
	store := ctx.KVStore(app.GetKey(types.StoreKey))
	setLegacyVestingAccount(t, store, legacyVestingAccount(t, authtestutil.LegacyDelayedVestingAccountTypeURL, 7))

	m := keeper.NewMigrator(app.AccountKeeper, app.QueryServiceRouter())
	require.NoError(t, m.Migrate3to4(ctx))
	once := accountStoreEntries(store)
	require.NoError(t, m.Migrate3to4(ctx))
	require.Equal(t, once, accountStoreEntries(store))
}

// The migration refuses a vesting account it cannot convert rather than leave
// it behind, since no type registered after it can decode that account.
func TestMigrate3to4RejectsUnconvertibleAccounts(t *testing.T) {
	delayed := authtestutil.LegacyDelayedVestingAccountTypeURL

	for _, tc := range []struct {
		name    string
		account func(t *testing.T) (sdk.AccAddress, []byte)
		err     string
	}{
		{
			name: "vesting account without its base vesting account",
			account: func(t *testing.T) (sdk.AccAddress, []byte) {
				unrelatedField := protowire.AppendVarint(protowire.AppendTag(nil, 9, protowire.VarintType), 1)
				encoded, err := (&codectypes.Any{TypeUrl: delayed, Value: unrelatedField}).Marshal()
				require.NoError(t, err)
				return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()), encoded
			},
			err: "field 1 is missing",
		},
		{
			name: "vesting account stored under another address",
			account: func(t *testing.T) (sdk.AccAddress, []byte) {
				encoded, err := legacyVestingAccount(t, delayed, 1).Encode()
				require.NoError(t, err)
				return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()), encoded
			},
			err: "embeds the base account of",
		},
		{
			name: "truncated account encoding",
			account: func(t *testing.T) (sdk.AccAddress, []byte) {
				encoded, err := legacyVestingAccount(t, delayed, 1).Encode()
				require.NoError(t, err)
				return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()), encoded[:len(encoded)/2]
			},
			err: "unexpected EOF",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, ctx := createTestApp(false)
			store := ctx.KVStore(app.GetKey(types.StoreKey))
			address, encoded := tc.account(t)
			store.Set(types.AddressStoreKey(address), encoded)
			before := accountStoreEntries(store)

			m := keeper.NewMigrator(app.AccountKeeper, app.QueryServiceRouter())
			require.ErrorContains(t, m.Migrate3to4(ctx), tc.err)
			require.Equal(t, before, accountStoreEntries(store))
		})
	}
}

// On pacific-1 the migration reads only the accounts it lists, so it rewrites
// each of them and leaves an unlisted account of a vesting type as it was.
func TestMigrate3to4OnPacific1RewritesOnlyTheListedAccounts(t *testing.T) {
	app, ctx := createTestApp(false)
	ctx = ctx.WithChainID("pacific-1")
	store := ctx.KVStore(app.GetKey(types.StoreKey))

	listed := make([]authtestutil.LegacyVestingAccount, 0, len(pacific1VestingAddresses))
	for i, address := range pacific1VestingAddresses {
		account := legacyVestingAccountAt(t, authtestutil.LegacyDelayedVestingAccountTypeURL,
			sdk.MustAccAddressFromBech32(address), uint64(100+i)) //nolint:gosec // small test index
		setLegacyVestingAccount(t, store, account)
		listed = append(listed, account)
	}
	setLegacyVestingAccount(t, store, legacyVestingAccount(t, authtestutil.LegacyDelayedVestingAccountTypeURL, 200))
	before := accountStoreEntries(store)

	m := keeper.NewMigrator(app.AccountKeeper, app.QueryServiceRouter())
	require.NoError(t, m.Migrate3to4(ctx))

	after := accountStoreEntries(store)
	require.Len(t, after, len(before), "the migration added or removed an account")
	for _, account := range listed {
		key := string(types.AddressStoreKey(account.Base.GetAddress()))
		want, err := app.AccountKeeper.MarshalAccount(account.Base)
		require.NoError(t, err)
		require.Equal(t, want, after[key], "%s was not rewritten as its base account", account.Base.Address)
		delete(before, key)
		delete(after, key)
	}
	require.Equal(t, before, after, "the migration changed an account pacific-1 does not list")
}

// On pacific-1 a listed address that holds no account, or an account of another
// type, is skipped: the migration neither fails nor writes an account there.
func TestMigrate3to4OnPacific1SkipsListedAddressesWithoutVestingAccounts(t *testing.T) {
	app, ctx := createTestApp(false)
	ctx = ctx.WithChainID("pacific-1")
	store := ctx.KVStore(app.GetKey(types.StoreKey))
	plain := app.AccountKeeper.NewAccountWithAddress(ctx, sdk.MustAccAddressFromBech32(pacific1VestingAddresses[0]))
	app.AccountKeeper.SetAccount(ctx, plain)
	before := accountStoreEntries(store)

	m := keeper.NewMigrator(app.AccountKeeper, app.QueryServiceRouter())
	require.NoError(t, m.Migrate3to4(ctx))
	require.Equal(t, before, accountStoreEntries(store))
}

// On pacific-1 the migration refuses a listed account it cannot convert, as it
// does on a chain whose account store it scans.
func TestMigrate3to4OnPacific1RejectsAnUnconvertibleListedAccount(t *testing.T) {
	app, ctx := createTestApp(false)
	ctx = ctx.WithChainID("pacific-1")
	store := ctx.KVStore(app.GetKey(types.StoreKey))
	encoded, err := legacyVestingAccount(t, authtestutil.LegacyDelayedVestingAccountTypeURL, 1).Encode()
	require.NoError(t, err)
	store.Set(types.AddressStoreKey(sdk.MustAccAddressFromBech32(pacific1VestingAddresses[1])), encoded)
	before := accountStoreEntries(store)

	m := keeper.NewMigrator(app.AccountKeeper, app.QueryServiceRouter())
	require.ErrorContains(t, m.Migrate3to4(ctx), "embeds the base account of")
	require.Equal(t, before, accountStoreEntries(store))
}
