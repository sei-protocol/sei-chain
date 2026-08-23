package query_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/dbadapter"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func TestUntrustedQueryRejectsLargeLimitUpfront(t *testing.T) {
	ctx := sdk.Context{}.WithIsABCIQuery(true).WithPaginationLimits(sdk.UntrustedPaginationLimits(1000, 10_000, 10_000))
	store := newTestKVStore(t)

	_, err := query.Paginate(ctx, store, &query.PageRequest{Limit: 1001}, func(_, _ []byte) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "limit 1001 exceeds the maximum of 1000")
}

func TestUntrustedQueryRejectsLargeOffsetUpfront(t *testing.T) {
	ctx := sdk.Context{}.WithIsABCIQuery(true).WithPaginationLimits(sdk.UntrustedPaginationLimits(1000, 10_000, 10_000))
	store := newTestKVStore(t)

	_, err := query.Paginate(ctx, store, &query.PageRequest{Offset: 10_001, Limit: 1}, func(_, _ []byte) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "offset 10001 exceeds the maximum of 10000")
}

func TestUntrustedQueryFlatIterationReturnsPartialPage(t *testing.T) {
	ctx := sdk.Context{}.WithIsABCIQuery(true).WithPaginationLimits(sdk.UntrustedPaginationLimits(1000, 10_000, 50))
	store := prefix.NewStore(newTestKVStore(t), []byte("deleg/"))

	for i := 0; i < 1000; i++ {
		store.Set([]byte(fmt.Sprintf("%04d", i)), []byte("v"))
	}

	var count int
	res, err := query.FilteredPaginate(ctx, store, &query.PageRequest{Limit: 20}, func(key, _ []byte, accumulate bool) (bool, error) {
		n, err := strconv.Atoi(string(key))
		matched := err == nil && n%100 == 0
		if matched && accumulate {
			count++
		}
		return matched, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NotNil(t, res.NextKey)
}

func TestUntrustedQueryFlatIterationExactBudget(t *testing.T) {
	ctx := sdk.Context{}.WithIsABCIQuery(true).WithPaginationLimits(sdk.UntrustedPaginationLimits(1000, 10_000, 10))
	store := newTestKVStore(t)

	for i := 0; i < 20; i++ {
		store.Set([]byte(fmt.Sprintf("%02d", i)), []byte("v"))
	}

	var count int
	res, err := query.Paginate(ctx, store, &query.PageRequest{Limit: 100}, func(_, _ []byte) error {
		count++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 10, count)
	require.NotNil(t, res.NextKey)
}

func TestTrustedQueryOriginBypassesLimits(t *testing.T) {
	ctx := sdk.Context{}.WithIsABCIQuery(true).WithIsTrustedQueryOrigin(true).WithPaginationLimits(sdk.NoPaginationLimits())
	store := newTestKVStore(t)

	for i := 0; i < 20; i++ {
		store.Set([]byte(fmt.Sprintf("%02d", i)), []byte("v"))
	}

	var count int
	res, err := query.Paginate(ctx, store, &query.PageRequest{Limit: query.MaxLimit}, func(_, _ []byte) error {
		count++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 20, count)
	require.Nil(t, res.NextKey)
}

func TestFilteredPaginateV66StillErrorsOnScanLimit(t *testing.T) {
	store := newTestKVStore(t)
	for i := 0; i < 20_000; i++ {
		store.Set([]byte(fmt.Sprintf("%05d", i)), []byte("v"))
	}

	_, err := query.FilteredPaginateV66(store, &query.PageRequest{Limit: 100}, func(_, _ []byte, _ bool) (bool, error) {
		return false, nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "scanned more than 10000 entries")
}

func newTestKVStore(t *testing.T) sdk.KVStore {
	t.Helper()
	return dbadapter.Store{DB: dbm.NewMemDB()}
}
