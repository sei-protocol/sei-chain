//go:build cgo

package cosmwasm

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/CosmWasm/wasmvm/internal/api"
	"github.com/CosmWasm/wasmvm/types"
)

const (
	TESTING_CAPABILITIES = "staking,stargate,iterator"
	TESTING_PRINT_DEBUG  = false
	TESTING_GAS_LIMIT    = uint64(500_000_000_000) // ~0.5ms
	TESTING_MEMORY_LIMIT = 32                      // MiB
	TESTING_CACHE_SIZE   = 100                     // MiB
)

const (
	CYBERPUNK_TEST_CONTRACT = "./testdata/cyberpunk.wasm"
	HACKATOM_TEST_CONTRACT  = "./testdata/hackatom.wasm"
)

func withVM(t *testing.T) *VM {
	tmpdir, err := ioutil.TempDir("", "wasmvm-testing")
	require.NoError(t, err)
	vm, err := NewVM(tmpdir, TESTING_CAPABILITIES, TESTING_MEMORY_LIMIT, TESTING_PRINT_DEBUG, TESTING_CACHE_SIZE)
	require.NoError(t, err)

	t.Cleanup(func() {
		vm.Cleanup()
		os.RemoveAll(tmpdir)
	})
	return vm
}

func createTestContract(t *testing.T, vm *VM, path string) Checksum {
	wasm, err := ioutil.ReadFile(path)
	require.NoError(t, err)
	checksum, err := vm.StoreCode(wasm)
	require.NoError(t, err)
	return checksum
}

func TestStoreCode(t *testing.T) {
	vm := withVM(t)

	// Valid hackatom contract
	{
		wasm, err := ioutil.ReadFile(HACKATOM_TEST_CONTRACT)
		require.NoError(t, err)
		_, err = vm.StoreCode(wasm)
		require.NoError(t, err)
	}

	// Valid cyberpunk contract
	{
		wasm, err := ioutil.ReadFile(CYBERPUNK_TEST_CONTRACT)
		require.NoError(t, err)
		_, err = vm.StoreCode(wasm)
		require.NoError(t, err)
	}

	// Valid Wasm with no exports
	{
		// echo '(module)' | wat2wasm - -o empty.wasm
		// hexdump -C < empty.wasm

		wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
		_, err := vm.StoreCode(wasm)
		require.ErrorContains(t, err, "Error during static Wasm validation: Wasm contract must contain exactly one memory")
	}

	// No Wasm
	{
		wasm := []byte("foobar")
		_, err := vm.StoreCode(wasm)
		require.ErrorContains(t, err, "Wasm bytecode could not be deserialized")
	}

	// Empty
	{
		wasm := []byte("")
		_, err := vm.StoreCode(wasm)
		require.ErrorContains(t, err, "Wasm bytecode could not be deserialized")
	}

	// Nil
	{
		var wasm []byte = nil
		_, err := vm.StoreCode(wasm)
		require.ErrorContains(t, err, "Null/Nil argument: wasm")
	}
}

func TestStoreCodeAndGet(t *testing.T) {
	vm := withVM(t)

	wasm, err := ioutil.ReadFile(HACKATOM_TEST_CONTRACT)
	require.NoError(t, err)

	checksum, err := vm.StoreCode(wasm)
	require.NoError(t, err)

	code, err := vm.GetCode(checksum)
	require.NoError(t, err)
	require.Equal(t, WasmCode(wasm), code)
}

func TestRemoveCode(t *testing.T) {
	vm := withVM(t)

	wasm, err := ioutil.ReadFile(HACKATOM_TEST_CONTRACT)
	require.NoError(t, err)

	checksum, err := vm.StoreCode(wasm)
	require.NoError(t, err)

	err = vm.RemoveCode(checksum)
	require.NoError(t, err)

	err = vm.RemoveCode(checksum)
	require.ErrorContains(t, err, "Wasm file does not exist")
}

func TestHappyPath(t *testing.T) {
	vm := withVM(t)
	checksum := createTestContract(t, vm, HACKATOM_TEST_CONTRACT)

	deserCost := types.UFraction{Numerator: 1, Denominator: 1}
	gasMeter1 := api.NewMockGasMeter(TESTING_GAS_LIMIT)
	// instantiate it with this store
	store := api.NewLookup(gasMeter1)
	goapi := api.NewMockAPI()
	balance := types.Coins{types.NewCoin(250, "ATOM")}
	querier := api.DefaultQuerier(api.MOCK_CONTRACT_ADDR, balance)

	// instantiate
	env := api.MockEnv()
	info := api.MockInfo("creator", nil)
	msg := []byte(`{"verifier": "fred", "beneficiary": "bob"}`)
	ires, _, err := vm.Instantiate(checksum, env, info, msg, store, *goapi, querier, gasMeter1, TESTING_GAS_LIMIT, deserCost)
	require.NoError(t, err)
	require.Equal(t, 0, len(ires.Messages))

	// execute
	gasMeter2 := api.NewMockGasMeter(TESTING_GAS_LIMIT)
	store.SetGasMeter(gasMeter2)
	env = api.MockEnv()
	info = api.MockInfo("fred", nil)
	hres, _, err := vm.Execute(checksum, env, info, []byte(`{"release":{}}`), store, *goapi, querier, gasMeter2, TESTING_GAS_LIMIT, deserCost)
	require.NoError(t, err)
	require.Equal(t, 1, len(hres.Messages))

	// make sure it read the balance properly and we got 250 atoms
	dispatch := hres.Messages[0].Msg
	require.NotNil(t, dispatch.Bank, "%#v", dispatch)
	require.NotNil(t, dispatch.Bank.Send, "%#v", dispatch)
	send := dispatch.Bank.Send
	assert.Equal(t, "bob", send.ToAddress)
	assert.Equal(t, balance, send.Amount)
	// check the data is properly formatted
	expectedData := []byte{0xF0, 0x0B, 0xAA}
	assert.Equal(t, expectedData, hres.Data)
}

func TestEnv(t *testing.T) {
	vm := withVM(t)
	checksum := createTestContract(t, vm, CYBERPUNK_TEST_CONTRACT)

	deserCost := types.UFraction{Numerator: 1, Denominator: 1}
	gasMeter1 := api.NewMockGasMeter(TESTING_GAS_LIMIT)
	// instantiate it with this store
	store := api.NewLookup(gasMeter1)
	goapi := api.NewMockAPI()
	balance := types.Coins{types.NewCoin(250, "ATOM")}
	querier := api.DefaultQuerier(api.MOCK_CONTRACT_ADDR, balance)

	// instantiate
	env := api.MockEnv()
	info := api.MockInfo("creator", nil)
	ires, _, err := vm.Instantiate(checksum, env, info, []byte(`{}`), store, *goapi, querier, gasMeter1, TESTING_GAS_LIMIT, deserCost)
	require.NoError(t, err)
	require.Equal(t, 0, len(ires.Messages))

	// Execute mirror env without Transaction
	env = types.Env{
		Block: types.BlockInfo{
			Height:  444,
			Time:    1955939743_123456789,
			ChainID: "nice-chain",
		},
		Contract: types.ContractInfo{
			Address: "wasm10dyr9899g6t0pelew4nvf4j5c3jcgv0r5d3a5l",
		},
		Transaction: nil,
	}
	info = api.MockInfo("creator", nil)
	msg := []byte(`{"mirror_env": {}}`)
	ires, _, err = vm.Execute(checksum, env, info, msg, store, *goapi, querier, gasMeter1, TESTING_GAS_LIMIT, deserCost)
	require.NoError(t, err)
	expected, _ := json.Marshal(env)
	require.Equal(t, expected, ires.Data)

	// Execute mirror env with Transaction
	env = types.Env{
		Block: types.BlockInfo{
			Height:  444,
			Time:    1955939743_123456789,
			ChainID: "nice-chain",
		},
		Contract: types.ContractInfo{
			Address: "wasm10dyr9899g6t0pelew4nvf4j5c3jcgv0r5d3a5l",
		},
		Transaction: &types.TransactionInfo{
			Index: 18,
		},
	}
	info = api.MockInfo("creator", nil)
	msg = []byte(`{"mirror_env": {}}`)
	ires, _, err = vm.Execute(checksum, env, info, msg, store, *goapi, querier, gasMeter1, TESTING_GAS_LIMIT, deserCost)
	require.NoError(t, err)
	expected, _ = json.Marshal(env)
	require.Equal(t, expected, ires.Data)
}

func TestGetMetrics(t *testing.T) {
	vm := withVM(t)

	// GetMetrics 1
	metrics, err := vm.GetMetrics()
	require.NoError(t, err)
	assert.Equal(t, &types.Metrics{}, metrics)

	// Create contract
	checksum := createTestContract(t, vm, HACKATOM_TEST_CONTRACT)

	deserCost := types.UFraction{Numerator: 1, Denominator: 1}

	// GetMetrics 2
	metrics, err = vm.GetMetrics()
	require.NoError(t, err)
	assert.Equal(t, &types.Metrics{}, metrics)

	// Instantiate 1
	gasMeter1 := api.NewMockGasMeter(TESTING_GAS_LIMIT)
	// instantiate it with this store
	store := api.NewLookup(gasMeter1)
	goapi := api.NewMockAPI()
	balance := types.Coins{types.NewCoin(250, "ATOM")}
	querier := api.DefaultQuerier(api.MOCK_CONTRACT_ADDR, balance)

	env := api.MockEnv()
	info := api.MockInfo("creator", nil)
	msg1 := []byte(`{"verifier": "fred", "beneficiary": "bob"}`)
	ires, _, err := vm.Instantiate(checksum, env, info, msg1, store, *goapi, querier, gasMeter1, TESTING_GAS_LIMIT, deserCost)
	require.NoError(t, err)
	require.Equal(t, 0, len(ires.Messages))

	// GetMetrics 3
	metrics, err = vm.GetMetrics()
	assert.NoError(t, err)
	require.Equal(t, uint32(0), metrics.HitsMemoryCache)
	require.Equal(t, uint32(1), metrics.HitsFsCache)
	require.Equal(t, uint64(1), metrics.ElementsMemoryCache)
	require.InEpsilon(t, 2832576, metrics.SizeMemoryCache, 0.25)

	// Instantiate 2
	msg2 := []byte(`{"verifier": "fred", "beneficiary": "susi"}`)
	ires, _, err = vm.Instantiate(checksum, env, info, msg2, store, *goapi, querier, gasMeter1, TESTING_GAS_LIMIT, deserCost)
	require.NoError(t, err)
	require.Equal(t, 0, len(ires.Messages))

	// GetMetrics 4
	metrics, err = vm.GetMetrics()
	assert.NoError(t, err)
	require.Equal(t, uint32(1), metrics.HitsMemoryCache)
	require.Equal(t, uint32(1), metrics.HitsFsCache)
	require.Equal(t, uint64(1), metrics.ElementsMemoryCache)
	require.InEpsilon(t, 2832576, metrics.SizeMemoryCache, 0.25)

	// Pin
	err = vm.Pin(checksum)
	require.NoError(t, err)

	// GetMetrics 5
	metrics, err = vm.GetMetrics()
	assert.NoError(t, err)
	require.Equal(t, uint32(1), metrics.HitsMemoryCache)
	require.Equal(t, uint32(2), metrics.HitsFsCache)
	require.Equal(t, uint64(1), metrics.ElementsPinnedMemoryCache)
	require.Equal(t, uint64(1), metrics.ElementsMemoryCache)
	require.InEpsilon(t, 2832576, metrics.SizePinnedMemoryCache, 0.25)
	require.InEpsilon(t, 2832576, metrics.SizeMemoryCache, 0.25)

	// Instantiate 3
	msg3 := []byte(`{"verifier": "fred", "beneficiary": "bert"}`)
	ires, _, err = vm.Instantiate(checksum, env, info, msg3, store, *goapi, querier, gasMeter1, TESTING_GAS_LIMIT, deserCost)
	require.NoError(t, err)
	require.Equal(t, 0, len(ires.Messages))

	// GetMetrics 6
	metrics, err = vm.GetMetrics()
	assert.NoError(t, err)
	require.Equal(t, uint32(1), metrics.HitsPinnedMemoryCache)
	require.Equal(t, uint32(1), metrics.HitsMemoryCache)
	require.Equal(t, uint32(2), metrics.HitsFsCache)
	require.Equal(t, uint64(1), metrics.ElementsPinnedMemoryCache)
	require.Equal(t, uint64(1), metrics.ElementsMemoryCache)
	require.InEpsilon(t, 2832576, metrics.SizePinnedMemoryCache, 0.25)
	require.InEpsilon(t, 2832576, metrics.SizeMemoryCache, 0.25)

	// Unpin
	err = vm.Unpin(checksum)
	require.NoError(t, err)

	// GetMetrics 7
	metrics, err = vm.GetMetrics()
	assert.NoError(t, err)
	require.Equal(t, uint32(1), metrics.HitsPinnedMemoryCache)
	require.Equal(t, uint32(1), metrics.HitsMemoryCache)
	require.Equal(t, uint32(2), metrics.HitsFsCache)
	require.Equal(t, uint64(0), metrics.ElementsPinnedMemoryCache)
	require.Equal(t, uint64(1), metrics.ElementsMemoryCache)
	require.Equal(t, uint64(0), metrics.SizePinnedMemoryCache)
	require.InEpsilon(t, 2832576, metrics.SizeMemoryCache, 0.25)

	// Instantiate 4
	msg4 := []byte(`{"verifier": "fred", "beneficiary": "jeff"}`)
	ires, _, err = vm.Instantiate(checksum, env, info, msg4, store, *goapi, querier, gasMeter1, TESTING_GAS_LIMIT, deserCost)
	require.NoError(t, err)
	require.Equal(t, 0, len(ires.Messages))

	// GetMetrics 8
	metrics, err = vm.GetMetrics()
	assert.NoError(t, err)
	require.Equal(t, uint32(1), metrics.HitsPinnedMemoryCache)
	require.Equal(t, uint32(2), metrics.HitsMemoryCache)
	require.Equal(t, uint32(2), metrics.HitsFsCache)
	require.Equal(t, uint64(0), metrics.ElementsPinnedMemoryCache)
	require.Equal(t, uint64(1), metrics.ElementsMemoryCache)
	require.Equal(t, uint64(0), metrics.SizePinnedMemoryCache)
	require.InEpsilon(t, 2832576, metrics.SizeMemoryCache, 0.25)
}
