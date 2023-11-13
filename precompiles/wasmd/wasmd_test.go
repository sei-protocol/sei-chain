package wasmd_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestRequiredGas(t *testing.T) {
	testApp := app.Setup(false)
	p, err := wasmd.NewPrecompile(&testApp.EvmKeeper, wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper))
	require.Nil(t, err)
	require.Equal(t, uint64(2000), p.RequiredGas(p.ExecuteID))
}

func TestExecute(t *testing.T) {
	testApp := app.Setup(false)
	mockAddr, _ := testkeeper.MockAddressPair()
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	wasmKeeper := wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper)
	p, err := wasmd.NewPrecompile(&testApp.EvmKeeper, wasmKeeper)
	require.Nil(t, err)
	code, err := os.ReadFile("../../example/cosmwasm/echo/artifacts/echo.wasm")
	require.Nil(t, err)
	codeID, err := wasmKeeper.Create(ctx, mockAddr, code, nil)
	require.Nil(t, err)
	contractAddr, _, err := wasmKeeper.Instantiate(ctx, codeID, mockAddr, mockAddr, []byte("{}"), "test", sdk.NewCoins())
	require.Nil(t, err)

	amts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000)))
	testApp.BankKeeper.MintCoins(ctx, "evm", amts)
	testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", mockAddr, amts)
	amtsbz, err := amts.MarshalJSON()
	require.Nil(t, err)
	executeMethod, err := p.ABI.MethodById(p.ExecuteID)
	require.Nil(t, err)
	args, err := executeMethod.Inputs.Pack(contractAddr.String(), mockAddr.String(), []byte("{\"echo\":{\"message\":\"test msg\"}}"), amtsbz)
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, &testApp.EvmKeeper)
	evm := vm.EVM{
		StateDB: statedb,
	}
	res, err := p.Run(&evm, append(p.ExecuteID, args...))
	require.Nil(t, err)
	require.Equal(t, fmt.Sprintf("received test msg from %s with 1000usei", mockAddr.String()), string(res))
}
