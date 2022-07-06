package keeper_test

import (
	"testing"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
)

const SupportedFeatures = "iterator,staking,stargate"
const TestContract = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"

func TestHandleBBNewBlock(t *testing.T) {
	ctx, wasmkeepers := wasmkeeper.CreateTestInput(t, false, SupportedFeatures)
	dexKeeper := keeper.Keeper{WasmKeeper: *wasmkeepers.WasmKeeper}
	dexKeeper.HandleBBNewBlock(ctx, TestContract, 1)
	// right now just to make sure this call doesn't crash.
	// TODO: add a test wasm file to actually invoke the sudo call
}
