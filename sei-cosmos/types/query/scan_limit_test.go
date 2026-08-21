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

func enforcingABCIContext(t *testing.T) sdk.Context {
	t.Helper()
	return sdk.Context{}.WithIsABCIQuery(true).WithQueryScanLimit(true, MaxScanLimit)
}

func TestPaginateEnforcesUntrustedScanLimitOnOffsetPath(t *testing.T) {
	kvStore := newTestKVStore(t)
	ctx := enforcingABCIContext(t)

	for i := 0; i < int(MaxScanLimit+50); i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	_, err := Paginate(ctx, kvStore, &PageRequest{Limit: 1, CountTotal: true}, func(_, _ []byte) error {
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "scanned more than 10000 entries")
}

func TestPaginateRejectsUntrustedLimitAboveScanCap(t *testing.T) {
	kvStore := newTestKVStore(t)
	ctx := enforcingABCIContext(t)
	kvStore.Set([]byte("a"), []byte("v"))

	called := false
	_, err := Paginate(ctx, kvStore, &PageRequest{Limit: MaxScanLimit + 1}, func(_, _ []byte) error {
		called = true
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "limit 10001 exceeds the maximum of 10000")
	require.False(t, called)
}

func TestPaginateRejectsUntrustedOffsetAboveScanCap(t *testing.T) {
	kvStore := newTestKVStore(t)
	ctx := enforcingABCIContext(t)
	kvStore.Set([]byte("a"), []byte("v"))

	called := false
	_, err := Paginate(ctx, kvStore, &PageRequest{Offset: MaxScanLimit + 1, Limit: 1}, func(_, _ []byte) error {
		called = true
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "offset 10001 exceeds the maximum of 10000")
	require.False(t, called)
}

func TestFilteredPaginateEnforcesUntrustedScanLimitOnKeyPath(t *testing.T) {
	kvStore := newTestKVStore(t)
	ctx := enforcingABCIContext(t)

	for i := 0; i < int(MaxScanLimit+50); i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	_, err := FilteredPaginate(ctx, kvStore, &PageRequest{Key: []byte("00000000"), Limit: 5}, func(_ []byte, value []byte, _ bool) (bool, error) {
		return string(value) == "hit", nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "scanned more than 10000 entries")
}

func TestPaginateTrustedOriginUsesHigherLimit(t *testing.T) {
	const (
		storeSize = 25_000
		pageLimit = 20_000
	)
	trustedLimit := uint64(storeSize)

	kvStore := newTestKVStore(t)
	for i := 0; i < storeSize; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	pageReq := &PageRequest{Limit: pageLimit, CountTotal: false}

	_, err := Paginate(enforcingABCIContext(t), kvStore, pageReq, func(_, _ []byte) error {
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "limit 20000 exceeds the maximum of 10000")

	trustedCtx := sdk.Context{}.
		WithIsABCIQuery(true).
		WithIsTrustedQueryOrigin(true).
		WithQueryScanLimit(true, trustedLimit)

	var count int
	_, err = Paginate(trustedCtx, kvStore, pageReq, func(_, _ []byte) error {
		count++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, pageLimit, count)
}

func TestPaginateTrustedOriginUnlimitedScan(t *testing.T) {
	const (
		storeSize = int(MaxScanLimit) + 50
		pageLimit = uint64(storeSize)
	)

	kvStore := newTestKVStore(t)
	for i := 0; i < storeSize; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	pageReq := &PageRequest{Limit: pageLimit, CountTotal: false}
	unlimitedCtx := sdk.Context{}.
		WithIsABCIQuery(true).
		WithIsTrustedQueryOrigin(true).
		WithQueryScanLimit(false, 0)

	var count int
	_, err := Paginate(unlimitedCtx, kvStore, pageReq, func(_, _ []byte) error {
		count++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, storeSize, count)
}

func TestPaginateTrustedOriginAllowsLimitAndOffsetWithinTrustedCap(t *testing.T) {
	const (
		storeSize    = 25_000
		trustedLimit = uint64(storeSize)
		offset       = uint64(20_000)
		pageLimit    = uint64(1_000)
	)

	kvStore := newTestKVStore(t)
	for i := 0; i < storeSize; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	untrusted := enforcingABCIContext(t)
	_, err := Paginate(untrusted, kvStore, &PageRequest{Offset: offset, Limit: pageLimit}, func(_, _ []byte) error {
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "offset 20000 exceeds the maximum of 10000")

	trustedCtx := sdk.Context{}.
		WithIsABCIQuery(true).
		WithIsTrustedQueryOrigin(true).
		WithQueryScanLimit(true, trustedLimit)

	var count int
	_, err = Paginate(trustedCtx, kvStore, &PageRequest{Offset: offset, Limit: pageLimit}, func(_, _ []byte) error {
		count++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, int(pageLimit), count)
}

func TestFilteredPaginateV66DoesNotRejectOversizedLimit(t *testing.T) {
	kvStore := newTestKVStore(t)
	for i := 0; i < 5; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	var count int
	_, err := FilteredPaginateV66(kvStore, &PageRequest{Limit: MaxScanLimit + 1}, func(_, _ []byte, accumulate bool) (bool, error) {
		if accumulate {
			count++
		}
		return true, nil
	})
	require.NoError(t, err)
	require.Equal(t, 5, count)

	_, err = FilteredPaginateV66(kvStore, &PageRequest{Offset: MaxScanLimit + 1, Limit: 1}, func(_, _ []byte, accumulate bool) (bool, error) {
		if accumulate {
			t.Fatal("offset past the store must not accumulate")
		}
		return true, nil
	})
	require.NoError(t, err)
}

func TestFilteredPaginateEnforcesUntrustedScanLimit(t *testing.T) {
	kvStore := newTestKVStore(t)
	ctx := enforcingABCIContext(t)

	for i := 0; i < 20_000; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("miss"))
	}

	_, err := FilteredPaginate(ctx, kvStore, &PageRequest{Limit: 5}, func(_ []byte, value []byte, _ bool) (bool, error) {
		return string(value) == "hit", nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "scanned more than 10000 entries")
}

func TestFilteredPaginateStopsPostPageScanWhenCountTotalFalse(t *testing.T) {
	kvStore := newTestKVStore(t)
	ctx := enforcingABCIContext(t)

	for i := 0; i < 20_000; i++ {
		value := []byte("miss")
		if i < 5 {
			value = []byte("hit")
		}
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), value)
	}

	res, err := FilteredPaginate(ctx, kvStore, &PageRequest{Limit: 5, CountTotal: false}, func(_ []byte, value []byte, _ bool) (bool, error) {
		return string(value) == "hit", nil
	})
	require.NoError(t, err)
	require.Nil(t, res.NextKey)
}

func TestFilteredPaginateV66StopsPostPageScanWhenCountTotalFalse(t *testing.T) {
	kvStore := newTestKVStore(t)

	for i := 0; i < 20_000; i++ {
		value := []byte("miss")
		if i < 5 {
			value = []byte("hit")
		}
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), value)
	}

	res, err := FilteredPaginateV66(kvStore, &PageRequest{Limit: 5, CountTotal: false}, func(_ []byte, value []byte, _ bool) (bool, error) {
		return string(value) == "hit", nil
	})
	require.NoError(t, err)
	require.Nil(t, res.NextKey)
}
