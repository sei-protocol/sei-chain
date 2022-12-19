package abci_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const (
	SupportedFeatures = "iterator,staking,stargate"
	TestContract      = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
)

func TestHandleBBNewBlock(t *testing.T) {
	// this test only ensures that HandleBBNewBlock doesn't crash. The actual logic
	// is tested in module_test.go where an actual wasm file is deployed and invoked.
	keeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeter())
	keeper.SetContract(ctx, &types.ContractInfoV2{
		ContractAddr: TestContract,
		RentBalance:  100000000,
	})
	wrapper := abci.KeeperWrapper{Keeper: keeper}
	wrapper.HandleBBNewBlock(ctx, TestContract, 1)
}
