package query_test

import (
	gocontext "context"
	"fmt"
	"math"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/suite"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-cosmos/baseapp"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
)

const (
	holder          = "holder"
	multiPerm       = "multiple permissions account"
	randomPerm      = "random permission"
	numBalances     = 235
	defaultLimit    = 100
	overLimit       = 101
	underLimit      = 10
	lastPageRecords = 35
)

type paginationTestSuite struct {
	suite.Suite
}

func TestPaginationTestSuite(t *testing.T) {
	suite.Run(t, new(paginationTestSuite))
}

func (s *paginationTestSuite) TestParsePagination() {
	s.T().Log("verify default values for empty page request")
	pageReq := &query.PageRequest{}
	page, limit, err := query.ParsePagination(pageReq)
	s.Require().NoError(err)
	s.Require().Equal(limit, query.DefaultLimit)
	s.Require().Equal(page, 1)

	s.T().Log("verify with custom values")
	pageReq = &query.PageRequest{
		Offset: 0,
		Limit:  10,
	}
	page, limit, err = query.ParsePagination(pageReq)
	s.Require().NoError(err)
	s.Require().Equal(page, 1)
	s.Require().Equal(limit, 10)

	s.T().Log("verify limits above MaxInt64 are rejected: page math is int-based")
	pageReq = &query.PageRequest{Limit: uint64(math.MaxInt64) + 1}
	_, _, err = query.ParsePagination(pageReq)
	s.Require().Error(err)

	s.T().Log("verify offsets above MaxInt64 are rejected: page math is int-based")
	pageReq = &query.PageRequest{Offset: uint64(math.MaxInt64) + 1, Limit: 1}
	_, _, err = query.ParsePagination(pageReq)
	s.Require().Error(err)
}

func (s *paginationTestSuite) TestPagination() {
	app, ctx, _ := setupTest(s.T())
	queryHelper := baseapp.NewQueryServerTestHelper(ctx, app.InterfaceRegistry())
	types.RegisterQueryServer(queryHelper, app.BankKeeper)
	queryClient := types.NewQueryClient(queryHelper)

	var balances sdk.Coins

	for i := 0; i < numBalances; i++ {
		denom := fmt.Sprintf("foo%ddenom", i)
		balances = append(balances, sdk.NewInt64Coin(denom, 100))
	}

	balances = balances.Sort()
	addr1 := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	acc1 := app.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	app.AccountKeeper.SetAccount(ctx, acc1)
	s.Require().NoError(apptesting.FundAccount(app.BankKeeper, ctx, addr1, balances))

	s.T().Log("verify empty page request results a max of defaultLimit records without total count")
	pageReq := &query.PageRequest{}
	request := types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err := queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Pagination.Total, uint64(0))
	s.Require().NotNil(res.Pagination.NextKey)
	s.Require().LessOrEqual(res.Balances.Len(), defaultLimit)

	s.T().Log("verify page request with limit > defaultLimit, returns less or equal to `limit` records")
	pageReq = &query.PageRequest{Limit: overLimit}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Pagination.Total, uint64(0))
	s.Require().NotNil(res.Pagination.NextKey)
	s.Require().LessOrEqual(res.Balances.Len(), overLimit)

	s.T().Log("verify paginate with custom limit and countTotal true")
	pageReq = &query.PageRequest{Limit: underLimit, CountTotal: true}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Balances.Len(), underLimit)
	s.Require().NotNil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(numBalances))

	s.T().Log("verify paginate with custom limit and countTotal false")
	pageReq = &query.PageRequest{Limit: defaultLimit, CountTotal: false}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Balances.Len(), defaultLimit)
	s.Require().NotNil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(0))

	s.T().Log("verify paginate with custom limit, key and countTotal false")
	pageReq = &query.PageRequest{Key: res.Pagination.NextKey, Limit: defaultLimit, CountTotal: false}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Balances.Len(), defaultLimit)
	s.Require().NotNil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(0))

	s.T().Log("verify paginate for last page, results in records less than max limit")
	pageReq = &query.PageRequest{Key: res.Pagination.NextKey, Limit: defaultLimit, CountTotal: false}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().LessOrEqual(res.Balances.Len(), defaultLimit)
	s.Require().Equal(res.Balances.Len(), lastPageRecords)
	s.Require().Nil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(0))

	s.T().Log("verify paginate with offset and limit")
	pageReq = &query.PageRequest{Offset: 200, Limit: defaultLimit, CountTotal: false}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().LessOrEqual(res.Balances.Len(), defaultLimit)
	s.Require().Equal(res.Balances.Len(), lastPageRecords)
	s.Require().Nil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(0))

	s.T().Log("verify paginate with offset and limit")
	pageReq = &query.PageRequest{Offset: 100, Limit: defaultLimit, CountTotal: false}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().LessOrEqual(res.Balances.Len(), defaultLimit)
	s.Require().NotNil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(0))

	s.T().Log("verify paginate with offset and key - error")
	pageReq = &query.PageRequest{Key: res.Pagination.NextKey, Offset: 100, Limit: defaultLimit, CountTotal: false}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	_, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().Error(err)
	s.Require().Equal("rpc error: code = InvalidArgument desc = paginate: invalid request, either offset or key is expected, got both", err.Error())

	s.T().Log("verify paginate with offset greater than total results")
	pageReq = &query.PageRequest{Offset: 300, Limit: defaultLimit, CountTotal: false}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().LessOrEqual(res.Balances.Len(), 0)
	s.Require().Nil(res.Pagination.NextKey)
}

func (s *paginationTestSuite) TestReversePagination() {
	app, ctx, _ := setupTest(s.T())
	queryHelper := baseapp.NewQueryServerTestHelper(ctx, app.InterfaceRegistry())
	types.RegisterQueryServer(queryHelper, app.BankKeeper)
	queryClient := types.NewQueryClient(queryHelper)

	var balances sdk.Coins

	for i := 0; i < numBalances; i++ {
		denom := fmt.Sprintf("foo%ddenom", i)
		balances = append(balances, sdk.NewInt64Coin(denom, 100))
	}

	balances = balances.Sort()
	addr1 := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	acc1 := app.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	app.AccountKeeper.SetAccount(ctx, acc1)
	s.Require().NoError(apptesting.FundAccount(app.BankKeeper, ctx, addr1, balances))

	s.T().Log("verify paginate with custom limit and countTotal, Reverse false")
	pageReq := &query.PageRequest{Limit: 2, CountTotal: true, Reverse: true, Key: nil}
	request := types.NewQueryAllBalancesRequest(addr1, pageReq)
	res1, err := queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res1.Balances.Len(), 2)
	s.Require().NotNil(res1.Pagination.NextKey)

	s.T().Log("verify paginate with custom limit and countTotal, Reverse false")
	pageReq = &query.PageRequest{Limit: 150}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res1, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res1.Balances.Len(), 150)
	s.Require().NotNil(res1.Pagination.NextKey)
	s.Require().Equal(res1.Pagination.Total, uint64(0))

	s.T().Log("verify paginate with custom limit, key and Reverse true")
	pageReq = &query.PageRequest{Limit: defaultLimit, Reverse: true}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err := queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Balances.Len(), defaultLimit)
	s.Require().NotNil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(0))

	s.T().Log("verify paginate with custom limit, key and Reverse true")
	pageReq = &query.PageRequest{Offset: 100, Limit: defaultLimit, Reverse: true}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Balances.Len(), defaultLimit)
	s.Require().NotNil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(0))

	s.T().Log("verify paginate for last page, Reverse true")
	pageReq = &query.PageRequest{Offset: 200, Limit: defaultLimit, Reverse: true}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Balances.Len(), lastPageRecords)
	s.Require().Nil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(0))

	s.T().Log("verify page request with limit > defaultLimit, returns less or equal to `limit` records")
	pageReq = &query.PageRequest{Limit: overLimit, Reverse: true}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Pagination.Total, uint64(0))
	s.Require().NotNil(res.Pagination.NextKey)
	s.Require().LessOrEqual(res.Balances.Len(), overLimit)

	s.T().Log("verify paginate with custom limit, key, countTotal false and Reverse true")
	pageReq = &query.PageRequest{Key: res1.Pagination.NextKey, Limit: 50, Reverse: true}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Balances.Len(), 50)
	s.Require().NotNil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(0))

	s.T().Log("verify Reverse pagination returns valid result")
	s.Require().Equal(balances[101:151].String(), res.Balances.Sort().String())

	s.T().Log("verify paginate with custom limit, key, countTotal false and Reverse true")
	pageReq = &query.PageRequest{Key: res.Pagination.NextKey, Limit: 50, Reverse: true}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Balances.Len(), 50)
	s.Require().NotNil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(0))

	s.T().Log("verify Reverse pagination returns valid result")
	s.Require().Equal(balances[51:101].String(), res.Balances.Sort().String())

	s.T().Log("verify paginate for last page Reverse true")
	pageReq = &query.PageRequest{Key: res.Pagination.NextKey, Limit: defaultLimit, Reverse: true}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().Equal(res.Balances.Len(), 51)
	s.Require().Nil(res.Pagination.NextKey)
	s.Require().Equal(res.Pagination.Total, uint64(0))

	s.T().Log("verify Reverse pagination returns valid result")
	s.Require().Equal(balances[0:51].String(), res.Balances.Sort().String())

	s.T().Log("verify paginate with offset and key - error")
	pageReq = &query.PageRequest{Key: res1.Pagination.NextKey, Offset: 100, Limit: defaultLimit, CountTotal: false}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	_, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().Error(err)
	s.Require().Equal("rpc error: code = InvalidArgument desc = paginate: invalid request, either offset or key is expected, got both", err.Error())

	s.T().Log("verify paginate with offset greater than total results")
	pageReq = &query.PageRequest{Offset: 300, Limit: defaultLimit, CountTotal: false, Reverse: true}
	request = types.NewQueryAllBalancesRequest(addr1, pageReq)
	res, err = queryClient.AllBalances(gocontext.Background(), request)
	s.Require().NoError(err)
	s.Require().LessOrEqual(res.Balances.Len(), 0)
	s.Require().Nil(res.Pagination.NextKey)
}

func (s *paginationTestSuite) TestPaginateCountTotalLargeStore() {
	app, ctx, _ := setupTest(s.T())
	// Use a dedicated prefix to isolate test data from other store entries.
	kvStore := prefix.NewStore(ctx.KVStore(app.GetKey(types.StoreKey)), []byte("largetotal/"))

	const numItems = 10_002
	for i := 0; i < numItems; i++ {
		kvStore.Set([]byte(fmt.Sprintf("%08d", i)), []byte("v"))
	}

	s.T().Log("count_total scans the whole store and returns an accurate total")
	res, err := query.Paginate(ctx, kvStore, &query.PageRequest{Limit: 1, CountTotal: true}, func(_, _ []byte) error { return nil })
	s.Require().NoError(err)
	s.Require().Equal(uint64(numItems), res.Total)

	s.T().Log("a large offset skips entries instead of being rejected")
	var count int
	res, err = query.Paginate(ctx, kvStore, &query.PageRequest{Offset: 10_001, Limit: 5}, func(_, _ []byte) error {
		count++
		return nil
	})
	s.Require().NoError(err)
	s.Require().Equal(1, count)
	s.Require().Nil(res.NextKey)

	s.T().Log("a maximum uint64 limit returns everything without overflowing")
	count = 0
	res, err = query.Paginate(ctx, kvStore, &query.PageRequest{Offset: 1, Limit: query.MaxLimit}, func(_, _ []byte) error {
		count++
		return nil
	})
	s.Require().NoError(err)
	s.Require().Equal(numItems-1, count)
	s.Require().Nil(res.NextKey)
}

func setupTest(t *testing.T) (*app.App, sdk.Context, codec.Codec) {
	a := app.Setup(t, false, false, false)
	ctx := a.BaseApp.NewContext(false, tmproto.Header{Height: 1})
	appCodec := a.AppCodec()

	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db)

	ms.LoadLatestVersion()

	return a, ctx, appCodec
}
