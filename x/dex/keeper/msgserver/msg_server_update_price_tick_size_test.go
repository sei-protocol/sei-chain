package msgserver_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestUpdatePriceTickSize(t *testing.T) {
	// Instantiate and get contract address
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	wctx := sdk.WrapSDKContext(ctx)
	keeper := testApp.DexKeeper
	server := msgserver.NewMsgServerImpl(keeper)

	// Test updated tick size
	tickUpdates := []types.TickSize{}
	tickUpdates = append(tickUpdates, types.TickSize{
		ContractAddr: TestContractA,
		Pair:         &keepertest.TestPair,
		Ticksize:     sdk.MustNewDecFromStr("0.1"),
	})
	_, err := server.UpdatePriceTickSize(wctx, &types.MsgUpdatePriceTickSize{
		Creator:      keepertest.TestAccount,
		TickSizeList: tickUpdates,
	})
	require.EqualError(t, err, sdkerrors.Wrapf(sdkerrors.ErrNotSupported, "deprecated").Error())
}
