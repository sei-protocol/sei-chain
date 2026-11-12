package keeper_test

import (
	"maps"
	"slices"
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

// rewriteLegacyVestingAccounts runs the rewrite Migrate3to4 starts in batches of batchSize until it
// is done, and returns the number of batches it took.
func rewriteLegacyVestingAccounts(t *testing.T, ak keeper.AccountKeeper, ctx sdk.Context, batchSize int) int {
	t.Helper()
	for batches := 1; ; batches++ {
		require.Less(t, batches, 1_000, "the rewrite did not finish")
		if ak.RewriteLegacyVestingAccounts(ctx, batchSize) {
			return batches
		}
	}
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
	require.Equal(t, before, accountStoreEntries(store), "the migration rewrote an account before the first batch")
	batches := rewriteLegacyVestingAccounts(t, app.AccountKeeper, ctx, 2)
	require.Equal(t, (len(before)+1)/2, batches, "the rewrite did not scan two accounts per batch")
	require.Nil(t, store.Get(types.LegacyVestingRewriteCursorKey), "the finished rewrite left its cursor behind")

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

// Each batch rewrites only the accounts it scans, and the next batch resumes at the first account
// left unscanned.
func TestRewriteLegacyVestingAccountsResumesWhereTheLastBatchStopped(t *testing.T) {
	app, ctx := createTestApp(false)
	store := ctx.KVStore(app.GetKey(types.StoreKey))
	vesting := map[string]bool{}
	for i := range 5 {
		account := legacyVestingAccount(t, authtestutil.LegacyDelayedVestingAccountTypeURL, uint64(100+i)) //nolint:gosec // small test index
		setLegacyVestingAccount(t, store, account)
		vesting[string(types.AddressStoreKey(account.Base.GetAddress()))] = true
	}
	keys := slices.Sorted(maps.Keys(accountStoreEntries(store)))
	const batchSize = 3

	m := keeper.NewMigrator(app.AccountKeeper, app.QueryServiceRouter())
	require.NoError(t, m.Migrate3to4(ctx))
	require.False(t, app.AccountKeeper.RewriteLegacyVestingAccounts(ctx, batchSize))
	require.Equal(t, []byte(keys[batchSize]), store.Get(types.LegacyVestingRewriteCursorKey),
		"the cursor is not the first account the batch left unscanned")
	for i, key := range keys {
		if !vesting[key] {
			continue
		}
		typeURL := string(legacyTypeURL(t, store.Get([]byte(key))))
		if i < batchSize {
			require.Equal(t, baseAccountTypeURL, typeURL, "account %d was scanned but not rewritten", i)
		} else {
			require.Equal(t, authtestutil.LegacyDelayedVestingAccountTypeURL, typeURL, "account %d was rewritten before it was scanned", i)
		}
	}

	rewriteLegacyVestingAccounts(t, app.AccountKeeper, ctx, batchSize)
	for key := range vesting {
		require.Equal(t, baseAccountTypeURL, string(legacyTypeURL(t, store.Get([]byte(key)))))
	}
}

const baseAccountTypeURL = "/cosmos.auth.v1beta1.BaseAccount"

func legacyTypeURL(t *testing.T, encoded []byte) []byte {
	t.Helper()
	var any codectypes.Any
	require.NoError(t, any.Unmarshal(encoded))
	return []byte(any.TypeUrl)
}

func TestRewriteLegacyVestingAccountsIsANoOpUntilStartedAndOnceDone(t *testing.T) {
	app, ctx := createTestApp(false)
	store := ctx.KVStore(app.GetKey(types.StoreKey))
	setLegacyVestingAccount(t, store, legacyVestingAccount(t, authtestutil.LegacyDelayedVestingAccountTypeURL, 7))
	unstarted := accountStoreEntries(store)

	require.True(t, app.AccountKeeper.RewriteLegacyVestingAccounts(ctx, 1_000))
	require.Equal(t, unstarted, accountStoreEntries(store))

	m := keeper.NewMigrator(app.AccountKeeper, app.QueryServiceRouter())
	require.NoError(t, m.Migrate3to4(ctx))
	rewriteLegacyVestingAccounts(t, app.AccountKeeper, ctx, 1_000)
	once := accountStoreEntries(store)
	require.True(t, app.AccountKeeper.RewriteLegacyVestingAccounts(ctx, 1_000))
	require.Equal(t, once, accountStoreEntries(store))
}

// An account not yet rewritten reads, iterates and pages as the base account it embeds.
func TestLegacyVestingAccountsDecodeAsBaseAccountsBeforeTheRewrite(t *testing.T) {
	app, ctx := createTestApp(false)
	store := ctx.KVStore(app.GetKey(types.StoreKey))
	accounts := map[string]authtestutil.LegacyVestingAccount{}
	for i, typeURL := range authtestutil.LegacyVestingAccountTypeURLs {
		account := legacyVestingAccount(t, typeURL, uint64(100+i)) //nolint:gosec // small test index
		setLegacyVestingAccount(t, store, account)
		accounts[account.Base.GetAddress().String()] = account
	}

	for _, account := range accounts {
		got, ok := app.AccountKeeper.GetAccount(ctx, account.Base.GetAddress()).(*types.BaseAccount)
		require.True(t, ok, "%s does not decode as a base account", account.TypeURL)
		require.Equal(t, account.Base, got)
	}
	iterated := 0
	app.AccountKeeper.IterateAccounts(ctx, func(account types.AccountI) bool {
		if legacy, ok := accounts[account.GetAddress().String()]; ok {
			require.Equal(t, legacy.Base, account)
			iterated++
		}
		return false
	})
	require.Equal(t, len(accounts), iterated)
	res, err := app.AccountKeeper.Accounts(sdk.WrapSDKContext(ctx), &types.QueryAccountsRequest{})
	require.NoError(t, err)
	paged := 0
	for _, encoded := range res.Accounts {
		var account types.AccountI
		require.NoError(t, app.InterfaceRegistry().UnpackAny(encoded, &account))
		if legacy, ok := accounts[account.GetAddress().String()]; ok {
			require.Equal(t, legacy.Base, account)
			paged++
		}
	}
	require.Equal(t, len(accounts), paged)
}

// The rewrite leaves a vesting account it cannot convert as it is, rather than halt the chain in the
// block that reaches it, and still finishes.
func TestRewriteLegacyVestingAccountsSkipsUnconvertibleAccounts(t *testing.T) {
	delayed := authtestutil.LegacyDelayedVestingAccountTypeURL

	for _, tc := range []struct {
		name    string
		account func(t *testing.T) (sdk.AccAddress, []byte)
	}{
		{
			name: "vesting account without its base vesting account",
			account: func(t *testing.T) (sdk.AccAddress, []byte) {
				unrelatedField := protowire.AppendVarint(protowire.AppendTag(nil, 9, protowire.VarintType), 1)
				encoded, err := (&codectypes.Any{TypeUrl: delayed, Value: unrelatedField}).Marshal()
				require.NoError(t, err)
				return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()), encoded
			},
		},
		{
			name: "vesting account stored under another address",
			account: func(t *testing.T) (sdk.AccAddress, []byte) {
				encoded, err := legacyVestingAccount(t, delayed, 1).Encode()
				require.NoError(t, err)
				return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()), encoded
			},
		},
		{
			name: "truncated account encoding",
			account: func(t *testing.T) (sdk.AccAddress, []byte) {
				encoded, err := legacyVestingAccount(t, delayed, 1).Encode()
				require.NoError(t, err)
				return sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()), encoded[:len(encoded)/2]
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, ctx := createTestApp(false)
			store := ctx.KVStore(app.GetKey(types.StoreKey))
			address, encoded := tc.account(t)
			store.Set(types.AddressStoreKey(address), encoded)
			convertible := legacyVestingAccount(t, delayed, 2)
			setLegacyVestingAccount(t, store, convertible)
			unconvertibleKey := string(types.AddressStoreKey(address))
			before := accountStoreEntries(store)

			m := keeper.NewMigrator(app.AccountKeeper, app.QueryServiceRouter())
			require.NoError(t, m.Migrate3to4(ctx))
			rewriteLegacyVestingAccounts(t, app.AccountKeeper, ctx, 1_000)

			after := accountStoreEntries(store)
			require.Equal(t, before[unconvertibleKey], after[unconvertibleKey])
			_, ok := app.AccountKeeper.GetAccount(ctx, convertible.Base.GetAddress()).(*types.BaseAccount)
			require.True(t, ok, "the unconvertible account stopped the rewrite of another account")
			require.Equal(t, baseAccountTypeURL,
				string(legacyTypeURL(t, after[string(types.AddressStoreKey(convertible.Base.GetAddress()))])))
		})
	}
}
