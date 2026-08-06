package query_test

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/app/apptesting"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/address"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
)

var addr1 = sdk.AccAddress([]byte("addr1"))

func (s *paginationTestSuite) TestFilteredPaginations() {
	app, ctx, appCodec := setupTest(s.T())

	var balances sdk.Coins
	for i := 0; i < numBalances; i++ {
		denom := fmt.Sprintf("foo%ddenom", i)
		balances = append(balances, sdk.NewInt64Coin(denom, 100))
	}

	for i := 0; i < 4; i++ {
		denom := fmt.Sprintf("test%ddenom", i)
		balances = append(balances, sdk.NewInt64Coin(denom, 250))
	}

	balances = balances.Sort()
	addr1 := sdk.AccAddress([]byte("addr1"))
	acc1 := app.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	app.AccountKeeper.SetAccount(ctx, acc1)
	s.Require().NoError(apptesting.FundAccount(app.BankKeeper, ctx, addr1, balances))
	store := ctx.KVStore(app.GetKey(types.StoreKey))

	// verify pagination with limit > total values
	pageReq := &query.PageRequest{Key: nil, Limit: 5, CountTotal: true}
	balances, res, err := execFilterPaginate(store, pageReq, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(4, len(balances))

	s.T().Log("verify empty request")
	balances, res, err = execFilterPaginate(store, nil, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(4, len(balances))
	s.Require().Equal(uint64(0), res.Total)
	s.Require().Nil(res.NextKey)

	s.T().Log("verify nextKey is returned if there are more results")
	pageReq = &query.PageRequest{Key: nil, Limit: 2, CountTotal: true}
	balances, res, err = execFilterPaginate(store, pageReq, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(2, len(balances))
	s.Require().NotNil(res.NextKey)
	s.Require().Equal(string(res.NextKey), "test2denom")
	s.Require().Equal(uint64(4), res.Total)

	s.T().Log("verify both key and offset can't be given")
	pageReq = &query.PageRequest{Key: res.NextKey, Limit: 1, Offset: 2, CountTotal: true}
	_, _, err = execFilterPaginate(store, pageReq, appCodec)
	s.Require().Error(err)

	s.T().Log("use nextKey for query")
	pageReq = &query.PageRequest{Key: res.NextKey, Limit: 2, CountTotal: true}
	balances, res, err = execFilterPaginate(store, pageReq, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(2, len(balances))
	s.Require().Nil(res.NextKey)

	s.T().Log("verify default limit")
	pageReq = &query.PageRequest{Key: nil, Limit: 0}
	balances, res, err = execFilterPaginate(store, pageReq, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(4, len(balances))
	s.Require().Equal(uint64(0), res.Total)

	s.T().Log("verify with offset")
	pageReq = &query.PageRequest{Offset: 2, Limit: 2}
	balances, res, err = execFilterPaginate(store, pageReq, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().LessOrEqual(len(balances), 2)
}

func (s *paginationTestSuite) TestReverseFilteredPaginations() {
	app, ctx, appCodec := setupTest(s.T())

	var balances sdk.Coins
	for i := 0; i < numBalances; i++ {
		denom := fmt.Sprintf("foo%ddenom", i)
		balances = append(balances, sdk.NewInt64Coin(denom, 100))
	}

	for i := 0; i < 10; i++ {
		denom := fmt.Sprintf("test%ddenom", i)
		balances = append(balances, sdk.NewInt64Coin(denom, 250))
	}

	balances = balances.Sort()
	addr1 := sdk.AccAddress([]byte("addr1"))
	acc1 := app.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	app.AccountKeeper.SetAccount(ctx, acc1)
	s.Require().NoError(apptesting.FundAccount(app.BankKeeper, ctx, addr1, balances))
	store := ctx.KVStore(app.GetKey(types.StoreKey))

	// verify pagination with limit > total values
	pageReq := &query.PageRequest{Key: nil, Limit: 5, CountTotal: true, Reverse: true}
	balns, res, err := execFilterPaginate(store, pageReq, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(5, len(balns))

	s.T().Log("verify empty request")
	balns, res, err = execFilterPaginate(store, nil, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(10, len(balns))
	s.Require().Equal(uint64(0), res.Total)
	s.Require().Nil(res.NextKey)

	s.T().Log("verify default limit")
	pageReq = &query.PageRequest{Reverse: true}
	balns, res, err = execFilterPaginate(store, pageReq, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(10, len(balns))
	s.Require().Equal(uint64(0), res.Total)

	s.T().Log("verify nextKey is returned if there are more results")
	pageReq = &query.PageRequest{Limit: 2, CountTotal: true, Reverse: true}
	balns, res, err = execFilterPaginate(store, pageReq, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(2, len(balns))
	s.Require().NotNil(res.NextKey)
	s.Require().Equal(string(res.NextKey), "test7denom")
	s.Require().Equal(uint64(10), res.Total)

	s.T().Log("verify both key and offset can't be given")
	pageReq = &query.PageRequest{Key: res.NextKey, Limit: 1, Offset: 2, Reverse: true}
	_, _, err = execFilterPaginate(store, pageReq, appCodec)
	s.Require().Error(err)

	s.T().Log("use nextKey for query and reverse true")
	pageReq = &query.PageRequest{Key: res.NextKey, Limit: 2, Reverse: true}
	balns, res, err = execFilterPaginate(store, pageReq, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(2, len(balns))
	s.Require().NotNil(res.NextKey)
	s.Require().Equal(string(res.NextKey), "test5denom")

	s.T().Log("verify last page records, nextKey for query and reverse true")
	pageReq = &query.PageRequest{Key: res.NextKey, Reverse: true}
	balns, res, err = execFilterPaginate(store, pageReq, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(6, len(balns))
	s.Require().Nil(res.NextKey)

	s.T().Log("verify Reverse pagination returns valid result")
	s.Require().Equal(balances[235:241].String(), balns.Sort().String())

}

func (s *paginationTestSuite) TestFilteredPaginateMaxLimitExceeded() {
	app, ctx, _ := setupTest(s.T())
	store := ctx.KVStore(app.GetKey(types.StoreKey))

	_, err := query.FilteredPaginate(store, &query.PageRequest{Limit: query.MaxLimit + 1}, func(_ []byte, _ []byte, _ bool) (bool, error) {
		return false, nil
	})
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "exceeds maximum allowed limit")
}

func (s *paginationTestSuite) TestFilteredPaginateOffsetExceedsMax() {
	app, ctx, _ := setupTest(s.T())
	kvStore := ctx.KVStore(app.GetKey(types.StoreKey))

	_, err := query.FilteredPaginate(kvStore, &query.PageRequest{Offset: query.MaxOffset + 1}, func(_ []byte, _ []byte, _ bool) (bool, error) {
		return false, nil
	})
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "exceeds maximum allowed offset")

	_, err = query.FilteredPaginate(kvStore, &query.PageRequest{Offset: query.MaxOffset}, func(_ []byte, _ []byte, _ bool) (bool, error) {
		return false, nil
	})
	s.Require().NoError(err)
}

func (s *paginationTestSuite) TestFilteredPaginateCountTotalScanLimitExceeded() {
	app, ctx, _ := setupTest(s.T())
	kvStore := prefix.NewStore(ctx.KVStore(app.GetKey(types.StoreKey)), []byte("filteredscanlimit/"))

	numItems := int(query.MaxScanLimit) + 2
	for i := 0; i < numItems; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	_, err := query.FilteredPaginate(kvStore, &query.PageRequest{Limit: 1, CountTotal: true}, func(_ []byte, _ []byte, _ bool) (bool, error) {
		return true, nil
	})
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "scanned more than")
}

func (s *paginationTestSuite) TestFilteredPaginateCountTotalScanLimitExceededNoHits() {
	app, ctx, _ := setupTest(s.T())
	kvStore := prefix.NewStore(ctx.KVStore(app.GetKey(types.StoreKey)), []byte("filteredscanlimitnohits/"))

	// Phase 1 fires when totalIter > offset + MaxScanLimit = 10001
	pageReq := &query.PageRequest{Offset: 1, CountTotal: true}
	numItems := int(query.MaxScanLimit) + 2
	for i := 0; i < numItems; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	// filter returns no hits — numHits never reaches end, Phase 1 guard must fire
	_, err := query.FilteredPaginate(kvStore, pageReq, func(_ []byte, _ []byte, _ bool) (bool, error) {
		return false, nil
	})
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "scanned more than")
}

func (s *paginationTestSuite) TestFilteredPaginateSparseFilterFillsPageWithinScanLimit() {
	app, ctx, _ := setupTest(s.T())
	kvStore := prefix.NewStore(ctx.KVStore(app.GetKey(types.StoreKey)), []byte("filteredsparse/"))

	numItems := int(query.MaxScanLimit)
	for i := 0; i < numItems; i++ {
		value := "miss"
		if i%1000 == 0 {
			value = "hit"
		}
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte(value))
	}

	var hits [][]byte
	onResult := func(key []byte, value []byte, accumulate bool) (bool, error) {
		if string(value) != "hit" {
			return false, nil
		}
		if accumulate {
			hits = append(hits, key)
		}
		return true, nil
	}

	res, err := query.FilteredPaginate(kvStore, &query.PageRequest{Limit: 5}, onResult)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(5, len(hits))
	s.Require().Equal("00000000", string(hits[0]))
	s.Require().Equal("00004000", string(hits[4]))
	s.Require().Equal("00005000", string(res.NextKey))

	s.T().Log("count_total scans the rest of the store, still within the Phase 2 cap")
	hits = nil
	res, err = query.FilteredPaginate(kvStore, &query.PageRequest{Limit: 5, CountTotal: true}, onResult)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(5, len(hits))
	s.Require().Equal(uint64(10), res.Total)
	s.Require().NotNil(res.NextKey)
}

func (s *paginationTestSuite) TestFilteredPaginateSparseFilterExceedsScanLimitReturnsPartialPage() {
	app, ctx, _ := setupTest(s.T())
	kvStore := prefix.NewStore(ctx.KVStore(app.GetKey(types.StoreKey)), []byte("filteredsparsepartial/"))

	numItems := int(query.MaxScanLimit)*2 + 500
	numHits := 0
	for i := 0; i < numItems; i++ {
		value := "miss"
		if i%3000 == 0 {
			value = "hit"
			numHits++
		}
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte(value))
	}
	s.Require().Less(numHits, 250, "the filter must be too sparse to ever fill a 250-entry page")

	onResult := func(hits *[][]byte) func(key []byte, value []byte, accumulate bool) (bool, error) {
		return func(key []byte, value []byte, accumulate bool) (bool, error) {
			if string(value) != "hit" {
				return false, nil
			}
			if accumulate {
				*hits = append(*hits, append([]byte{}, key...))
			}
			return true, nil
		}
	}

	var hits [][]byte
	res, err := query.FilteredPaginate(kvStore, &query.PageRequest{Limit: 250}, onResult(&hits))
	s.Require().NoError(err, "hitting the scan cap before the page fills must not error when count_total is unset")
	s.Require().NotNil(res)
	s.Require().NotEmpty(hits, "hits found before the scan cap must be returned, not discarded")
	s.Require().Less(len(hits), 250, "fewer real hits exist within one scan window than the requested limit")
	s.Require().NotNil(res.NextKey, "a resumable key must be returned rather than erroring the whole page away")

	s.T().Log("following NextKey makes bounded progress and eventually surfaces every real hit")
	allHits := append([][]byte{}, hits...)
	key := res.NextKey
	for i := 0; i < 5 && key != nil; i++ {
		hits = nil
		res, err = query.FilteredPaginate(kvStore, &query.PageRequest{Key: key, Limit: 250}, onResult(&hits))
		s.Require().NoError(err)
		allHits = append(allHits, hits...)
		key = res.NextKey
	}
	s.Require().Len(allHits, numHits, "resuming from NextKey must eventually surface every real match")
}

// TestFilteredPaginatePastPageHitBeyondScanWindowReturnsResumableKey covers the
// "silent data loss" bug: once the page fills, the old Phase 2 logic gave up after
// MaxScanLimit further entries and returned NextKey: nil whenever it hadn't found
// the next hit yet — which a caller reads as "this was the last page" even though a
// real match exists beyond the scan window. It must instead return a non-nil,
// resumable NextKey whenever the scan was cut short rather than the store being
// genuinely exhausted.
func (s *paginationTestSuite) TestFilteredPaginatePastPageHitBeyondScanWindowReturnsResumableKey() {
	app, ctx, _ := setupTest(s.T())
	kvStore := prefix.NewStore(ctx.KVStore(app.GetKey(types.StoreKey)), []byte("filteredpastpage/"))

	const farHitIndex = 15000
	numItems := farHitIndex + 1
	for i := 0; i < numItems; i++ {
		value := "miss"
		if i == 0 || i == 1 || i == farHitIndex {
			value = "hit"
		}
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte(value))
	}

	var hits [][]byte
	onResult := func(key []byte, value []byte, accumulate bool) (bool, error) {
		if string(value) != "hit" {
			return false, nil
		}
		if accumulate {
			hits = append(hits, append([]byte{}, key...))
		}
		return true, nil
	}

	res, err := query.FilteredPaginate(kvStore, &query.PageRequest{Limit: 2}, onResult)
	s.Require().NoError(err)
	s.Require().Equal(2, len(hits), "the requested page fills from the first two hits")
	s.Require().NotNil(res.NextKey,
		"a third hit exists beyond the scan window; NextKey must point past the page rather than "+
			"nil, which would claim this was the last page and silently drop it")

	s.T().Log("resuming from NextKey eventually reaches the far hit")
	hits = nil
	key := res.NextKey
	for i := 0; i < 5 && key != nil; i++ {
		res, err = query.FilteredPaginate(kvStore, &query.PageRequest{Key: key, Limit: 1}, onResult)
		s.Require().NoError(err)
		key = res.NextKey
	}
	s.Require().Len(hits, 1)
	s.Require().Equal(fmt.Sprintf("%08d", farHitIndex), string(hits[0]))
}

// TestFilteredPaginateSparseFilterWithCountTotalStillErrors pins the one
// deliberate exception to the partial-page fix above: count_total=true asks for an
// exact Total, which cannot be honored once the scan is cut short before the page
// even fills, so that combination must still fail loud rather than return a Total
// that looks exact but is actually a lower bound.
func (s *paginationTestSuite) TestFilteredPaginateSparseFilterWithCountTotalStillErrors() {
	app, ctx, _ := setupTest(s.T())
	kvStore := prefix.NewStore(ctx.KVStore(app.GetKey(types.StoreKey)), []byte("filteredsparsecounttotal/"))

	numItems := int(query.MaxScanLimit) + 2
	for i := 0; i < numItems; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("miss"))
	}

	_, err := query.FilteredPaginate(kvStore, &query.PageRequest{Limit: 250, CountTotal: true},
		func(_ []byte, value []byte, _ bool) (bool, error) {
			return string(value) == "hit", nil
		})
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "scanned more than")
	s.Require().Contains(err.Error(), "count_total")
}

// TestGenericFilteredPaginateSparseFilterReturnsPartialResults is the
// GenericFilteredPaginate counterpart of the FilteredPaginate fix above — the code
// path x/authz's GranteeGrants and x/feegrant's granter-allowances queries use.
// codectypes.Any is used as both T and F because its zero value marshals to zero
// bytes, which is exactly the "no hit" signal GenericFilteredPaginate reads via
// val.Size() != 0.
func (s *paginationTestSuite) TestGenericFilteredPaginateSparseFilterReturnsPartialResults() {
	app, ctx, appCodec := setupTest(s.T())
	kvStore := prefix.NewStore(ctx.KVStore(app.GetKey(types.StoreKey)), []byte("genericsparse/"))

	numItems := int(query.MaxScanLimit) + 2000
	numHits := 0
	for i := 0; i < numItems; i++ {
		typeURL := "miss"
		if i%4000 == 0 {
			typeURL = "hit"
			numHits++
		}
		bz, err := appCodec.Marshal(&codectypes.Any{TypeUrl: typeURL})
		s.Require().NoError(err)
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), bz)
	}
	s.Require().Less(numHits, 50, "the filter must be too sparse to ever fill a 50-entry page")

	results, res, err := query.GenericFilteredPaginate(appCodec, kvStore, &query.PageRequest{Limit: 50},
		func(_ []byte, value *codectypes.Any) (*codectypes.Any, error) {
			if value.TypeUrl != "hit" {
				return &codectypes.Any{}, nil
			}
			return value, nil
		},
		func() *codectypes.Any { return &codectypes.Any{} },
	)
	s.Require().NoError(err, "hitting the scan cap before the page fills must not error when count_total is unset")
	s.Require().NotEmpty(results, "hits found before the scan cap must be returned, not discarded")
	s.Require().Less(len(results), 50, "fewer real hits exist within one scan window than the requested limit")
	s.Require().NotNil(res.NextKey, "a resumable key must be returned rather than erroring the whole page away")
}

func execFilterPaginate(store sdk.KVStore, pageReq *query.PageRequest, appCodec codec.Codec) (balances sdk.Coins, res *query.PageResponse, err error) {
	balancesStore := prefix.NewStore(store, types.BalancesPrefix)
	accountStore := prefix.NewStore(balancesStore, address.MustLengthPrefix(addr1))

	var balResult sdk.Coins
	res, err = query.FilteredPaginate(accountStore, pageReq, func(key []byte, value []byte, accumulate bool) (bool, error) {
		var bal sdk.Coin
		err := appCodec.Unmarshal(value, &bal)
		if err != nil {
			return false, err
		}

		// filter balances with amount greater than 100
		if bal.Amount.Int64() > int64(100) {
			if accumulate {
				balResult = append(balResult, bal)
			}

			return true, nil
		}

		return false, nil
	})

	return balResult, res, err
}
