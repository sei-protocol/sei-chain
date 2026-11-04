//go:build cgo

package cosmwasm

import (
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/require"
)

const IBC_TEST_CONTRACT = "./testdata/ibc_reflect.wasm"

func TestIBC(t *testing.T) {
	vm := withVM(t)

	wasm, err := ioutil.ReadFile(IBC_TEST_CONTRACT)
	require.NoError(t, err)

	checksum, err := vm.StoreCode(wasm)
	require.NoError(t, err)

	code, err := vm.GetCode(checksum)
	require.NoError(t, err)
	require.Equal(t, WasmCode(wasm), code)
}

func TestAnalyzeCode(t *testing.T) {
	vm := withVM(t)

	// Store non-IBC contract
	wasm, err := ioutil.ReadFile(HACKATOM_TEST_CONTRACT)
	require.NoError(t, err)
	checksum, err := vm.StoreCode(wasm)
	require.NoError(t, err)
	// and analyze
	report, err := vm.AnalyzeCode(checksum)
	require.NoError(t, err)
	require.False(t, report.HasIBCEntryPoints)
	require.Equal(t, "", report.RequiredFeatures)
	require.Equal(t, "", report.RequiredCapabilities)

	// Store IBC contract
	wasm2, err := ioutil.ReadFile(IBC_TEST_CONTRACT)
	require.NoError(t, err)
	checksum2, err := vm.StoreCode(wasm2)
	require.NoError(t, err)
	// and analyze
	report2, err := vm.AnalyzeCode(checksum2)
	require.NoError(t, err)
	require.True(t, report2.HasIBCEntryPoints)
	require.Equal(t, "iterator,stargate", report2.RequiredFeatures)
	require.Equal(t, "iterator,stargate", report2.RequiredCapabilities)
}
