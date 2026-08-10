package query_test

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/app/apptesting"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
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

	s.T().Log("verify maximum uint64 limit returns all filtered values")
	pageReq = &query.PageRequest{Limit: query.MaxLimit}
	balances, res, err = execFilterPaginate(store, pageReq, appCodec)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(4, len(balances))
	s.Require().Nil(res.NextKey)

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

func (s *paginationTestSuite) TestFilteredPaginateCountTotalLargeSparseStore() {
	app, ctx, _ := setupTest(s.T())
	kvStore := prefix.NewStore(ctx.KVStore(app.GetKey(types.StoreKey)), []byte("filteredlargetotal/"))

	const numItems = 10_002
	for i := 0; i < numItems; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	res, err := query.FilteredPaginate(kvStore, &query.PageRequest{Limit: 3, CountTotal: true}, func(key []byte, _ []byte, _ bool) (bool, error) {
		return string(key) == "00000000" || string(key) == "00005000" || string(key) == "00010000", nil
	})
	s.Require().NoError(err)
	s.Require().Equal(uint64(3), res.Total)
	s.Require().Nil(res.NextKey)
}

func (s *paginationTestSuite) TestFilteredPaginateLimitExceedsHitsInLargeStore() {
	app, ctx, _ := setupTest(s.T())
	kvStore := prefix.NewStore(ctx.KVStore(app.GetKey(types.StoreKey)), []byte("filteredlargelimit/"))

	const numItems = 10_002
	for i := 0; i < numItems; i++ {
		value := []byte("miss")
		if i%100 == 0 {
			value = []byte("hit")
		}
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), value)
	}

	var hits int
	res, err := query.FilteredPaginate(kvStore, &query.PageRequest{Limit: 250}, func(_ []byte, value []byte, accumulate bool) (bool, error) {
		hit := string(value) == "hit"
		if hit && accumulate {
			hits++
		}
		return hit, nil
	})
	s.Require().NoError(err)
	s.Require().Equal(101, hits)
	s.Require().Nil(res.NextKey)
}

func (s *paginationTestSuite) TestFilteredPaginateSparseFilter() {
	app, ctx, _ := setupTest(s.T())
	kvStore := prefix.NewStore(ctx.KVStore(app.GetKey(types.StoreKey)), []byte("filteredsparse/"))

	const numItems = 10_000
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

	s.T().Log("count_total scans the rest of the store")
	hits = nil
	res, err = query.FilteredPaginate(kvStore, &query.PageRequest{Limit: 5, CountTotal: true}, onResult)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(5, len(hits))
	s.Require().Equal(uint64(10), res.Total)
	s.Require().NotNil(res.NextKey)
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
