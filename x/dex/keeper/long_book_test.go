package keeper_test

import (
	"encoding/hex"
	"fmt"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/stretchr/testify/require"
)

func TestLongBookGet(t *testing.T) {
	//keeper, ctx := keepertest.DexKeeper(t)
	//items := keepertest.CreateNLongBook(keeper, ctx, 10)
	//for i, item := range items {
	//	got, found := keeper.GetLongBookByPrice(ctx, keepertest.TestContract, sdk.NewDec(int64(i)), keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	//	require.True(t, found)
	//	require.Equal(t,
	//		nullify.Fill(&item),
	//		nullify.Fill(&got),
	//	)
	//}
	// Decode
	str := "08B7A8F304123E736569313436366E66337A7578707961387139656D78756B643776667461663668347073723061303773726C357A7737347A683834796A717065686579631AAE0108BE82121A2A736569313937616568793779766C356B3632747374367A75766766356A357A687879366E613061376870223E736569313436366E66337A7578707961387139656D78756B643776667461663668347073723061303773726C357A7737347A683834796A717065686579632A0231303201313A0455535432420441544F4D5A297B22706F736974696F6E5F656666656374223A224F70656E222C226C65766572616765223A2231227D1AD20108BE82121A2A736569316B396766736A677A7839686563306D643775686A617176767732786E646E71633232796E3872223E736569313436366E66337A7578707961387139656D78756B643776667461663668347073723061303773726C357A7737347A683834796A717065686579632A1431323030303030303030303030303030303030303213333030303030303030303030303030303030303A0455535432420441544F4D5A297B22706F736974696F6E5F656666656374223A224F70656E222C226C65766572616765223A2231227D"
	if bytes, err := hex.DecodeString(str); err != nil {
		panic(err)
	} else {
		result := types.MatchResult{}
		if err := result.Unmarshal(bytes); err != nil {
			panic(err)
		}
		ret := &types.QueryGetMatchResultResponse{Result: &result}
		encodingConfig := app.MakeEncodingConfig()
		ctx := client.Context{}.WithCodec(encodingConfig.Marshaler).WithOutputFormat("text")
		fmt.Printf("%s\n", ctx.PrintProto(ret))
	}

}

func TestLongBookRemove(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	items := keepertest.CreateNLongBook(keeper, ctx, 10)
	for i := range items {
		keeper.RemoveLongBookByPrice(ctx, keepertest.TestContract, sdk.NewDec(int64(i)), keepertest.TestPriceDenom, keepertest.TestAssetDenom)
		_, found := keeper.GetLongBookByPrice(ctx, keepertest.TestContract, sdk.NewDec(int64(i)), keepertest.TestPriceDenom, keepertest.TestAssetDenom)
		require.False(t, found)
	}
}

func TestLongBookGetAll(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	items := keepertest.CreateNLongBook(keeper, ctx, 10)
	require.ElementsMatch(t,
		nullify.Fill(items),
		nullify.Fill(keeper.GetAllLongBook(ctx, keepertest.TestContract)),
	)
}
