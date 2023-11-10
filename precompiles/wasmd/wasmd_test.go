package wasmd_test

import (
	"testing"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/precompiles/wasmd"
	"github.com/stretchr/testify/require"
)

func TestRequiredGas(t *testing.T) {
	testApp := app.Setup(false)
	p, err := wasmd.NewPrecompile(&testApp.EvmKeeper, wasmkeeper.NewDefaultPermissionKeeper(testApp.WasmKeeper))
	require.Nil(t, err)
	require.Equal(t, uint64(2000), p.RequiredGas(p.ExecuteID))
}
