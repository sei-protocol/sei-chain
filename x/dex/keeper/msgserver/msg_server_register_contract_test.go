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

const (
	TestContractA = "sei1hrpna9v7vs3stzyd4z3xf00676kf78zpe2u5ksvljswn2vnjp3yslucc3n"
)

func TestRegisterContract(t *testing.T) {
	// Instantiate and get contract address
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	wctx := sdk.WrapSDKContext(ctx)
	keeper := testApp.DexKeeper

	server := msgserver.NewMsgServerImpl(keeper)
	_, err := server.RegisterContract(wctx, &types.MsgRegisterContract{})
	require.EqualError(t, err, sdkerrors.Wrapf(sdkerrors.ErrNotSupported, "deprecated").Error())
}
