package query

import (
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store"
)

func newTestKVStore(t *testing.T) sdk.KVStore {
	t.Helper()

	db := dbm.NewMemDB()
	key := storetypes.NewKVStoreKey("test")
	ms := store.NewCommitMultiStore(db)
	ms.MountStoreWithDB(key, storetypes.StoreTypeIAVL, db)
	require.NoError(t, ms.LoadLatestVersion())
	return prefix.NewStore(ms.GetKVStore(key), []byte("scanlimit/"))
}

func TestPaginateEnforcesUntrustedScanLimit(t *testing.T) {
	kvStore := newTestKVStore(t)
	ctx := sdk.Context{}.WithIsABCIQuery(true).WithQueryScanLimit(true, MaxScanLimit)

	for i := 0; i < int(MaxScanLimit+50); i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	_, err := Paginate(ctx, kvStore, &PageRequest{Offset: MaxScanLimit + 1, Limit: 10}, func(_, _ []byte) error {
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "scanned more than 10000 entries")
}

func TestPaginateTrustedOriginUsesHigherLimit(t *testing.T) {
	kvStore := newTestKVStore(t)
	trustedLimit := uint64(500)
	ctx := sdk.Context{}.
		WithIsABCIQuery(true).
		WithIsTrustedQueryOrigin(true).
		WithQueryScanLimit(true, trustedLimit)

	for i := 0; i < 600; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	var count int
	_, err := Paginate(ctx, kvStore, &PageRequest{Offset: 450, Limit: 10}, func(_, _ []byte) error {
		count++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 10, count)
}

func TestFilteredPaginateEnforcesUntrustedScanLimit(t *testing.T) {
	kvStore := newTestKVStore(t)
	ctx := sdk.Context{}.WithIsABCIQuery(true).WithQueryScanLimit(true, MaxScanLimit)

	for i := 0; i < 20_000; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("miss"))
	}

	_, err := FilteredPaginate(ctx, kvStore, &PageRequest{Limit: 5}, func(_ []byte, value []byte, _ bool) (bool, error) {
		return string(value) == "hit", nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "scanned more than 10000 entries")
}
