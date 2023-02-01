package abci_test

import (
	"context"
	"testing"
	"time"

	sdkstoretypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

const (
	SupportedFeatures = "iterator,staking,stargate"
	TestContract      = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
)

func TestHandleBBNewBlock(t *testing.T) {
	// this test only ensures that HandleBBNewBlock doesn't crash. The actual logic
	// is tested in module_test.go where an actual wasm file is deployed and invoked.
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetKey(types.StoreKey))))
	keeper := testApp.DexKeeper
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithLogger(&sdkstoretypes.NoOpLogger{}, "test"))
	keeper.SetContract(ctx, &types.ContractInfoV2{
		ContractAddr: TestContract,
		RentBalance:  100000000,
	})
	wrapper := abci.KeeperWrapper{Keeper: &keeper}
	wrapper.HandleBBNewBlock(ctx, TestContract, 1)
}
