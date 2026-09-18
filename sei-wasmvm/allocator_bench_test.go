//go:build cgo

package cosmwasm

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-wasmvm/internal/api"
	"github.com/sei-protocol/sei-chain/sei-wasmvm/types"
	"github.com/sei-protocol/sei-chain/utils/jemalloc"
)

// libwasmvm is a Rust library on the system allocator, so every module
// compile, instance and host call below allocates through the C malloc the
// test binary is linked against. Run once per allocator and diff:
//
//	go test -run '^$' -bench BenchmarkWasmVMCHeap -count 10 ./sei-wasmvm/ > libc.txt
//	go test -run '^$' -bench BenchmarkWasmVMCHeap -count 10 -tags jemalloc ./sei-wasmvm/ > jemalloc.txt
//	benchstat libc.txt jemalloc.txt

func newBenchVM(b *testing.B, cacheSize uint32) *VM {
	vm, err := NewVM(b.TempDir(), TESTING_CAPABILITIES, TESTING_MEMORY_LIMIT, TESTING_PRINT_DEBUG, cacheSize)
	require.NoError(b, err)
	return vm
}

func benchVM(b *testing.B, cacheSize uint32) *VM {
	vm := newBenchVM(b, cacheSize)
	b.Cleanup(vm.Cleanup)
	return vm
}

type benchContract struct {
	vm       *VM
	checksum Checksum
	store    *api.Lookup
	goapi    GoAPI
	querier  Querier
}

// instantiateHackatom stores and instantiates hackatom once so the execute
// and query loops below measure steady-state contract calls.
func instantiateHackatom(b *testing.B, vm *VM) benchContract {
	wasm, err := os.ReadFile(HACKATOM_TEST_CONTRACT)
	require.NoError(b, err)
	checksum, err := vm.StoreCode(wasm)
	require.NoError(b, err)

	gasMeter := api.NewMockGasMeter(TESTING_GAS_LIMIT)
	store := api.NewLookup(gasMeter)
	goapi := api.NewMockAPI()
	querier := api.DefaultQuerier(api.MOCK_CONTRACT_ADDR, types.Coins{types.NewCoin(250, "ATOM")})
	deserCost := types.UFraction{Numerator: 1, Denominator: 1}

	msg := []byte(`{"verifier": "fred", "beneficiary": "bob"}`)
	_, _, err = vm.Instantiate(checksum, api.MockEnv(), api.MockInfo("creator", nil), msg, store, *goapi, querier, gasMeter, TESTING_GAS_LIMIT, deserCost)
	require.NoError(b, err)

	return benchContract{vm: vm, checksum: checksum, store: store, goapi: *goapi, querier: querier}
}

func reportCHeap(b *testing.B) {
	if jemalloc.Enabled {
		b.ReportMetric(float64(jemalloc.Allocated())/(1<<20), "jemalloc-MiB")
	}
}

// BenchmarkWasmVMCHeap measures libwasmvm compile, instantiate, execute and
// query under whichever C allocator the test binary is linked against.
func BenchmarkWasmVMCHeap(b *testing.B) {
	deserCost := types.UFraction{Numerator: 1, Denominator: 1}

	b.Run("compile", func(b *testing.B) {
		wasm, err := os.ReadFile(CYBERPUNK_TEST_CONTRACT)
		require.NoError(b, err)
		for b.Loop() {
			b.StopTimer()
			vm := newBenchVM(b, 0)
			b.StartTimer()
			if _, err := vm.StoreCode(wasm); err != nil {
				b.Fatal(err)
			}
			b.StopTimer()
			vm.Cleanup()
			b.StartTimer()
		}
		reportCHeap(b)
	})

	b.Run("instantiate", func(b *testing.B) {
		vm := benchVM(b, TESTING_CACHE_SIZE)
		wasm, err := os.ReadFile(HACKATOM_TEST_CONTRACT)
		require.NoError(b, err)
		checksum, err := vm.StoreCode(wasm)
		require.NoError(b, err)
		goapi := api.NewMockAPI()
		querier := api.DefaultQuerier(api.MOCK_CONTRACT_ADDR, types.Coins{types.NewCoin(250, "ATOM")})
		msg := []byte(`{"verifier": "fred", "beneficiary": "bob"}`)
		for b.Loop() {
			gasMeter := api.NewMockGasMeter(TESTING_GAS_LIMIT)
			store := api.NewLookup(gasMeter)
			if _, _, err := vm.Instantiate(checksum, api.MockEnv(), api.MockInfo("creator", nil), msg, store, *goapi, querier, gasMeter, TESTING_GAS_LIMIT, deserCost); err != nil {
				b.Fatal(err)
			}
		}
		reportCHeap(b)
	})

	b.Run("execute", func(b *testing.B) {
		c := instantiateHackatom(b, benchVM(b, TESTING_CACHE_SIZE))
		msg := []byte(`{"release":{}}`)
		for b.Loop() {
			gasMeter := api.NewMockGasMeter(TESTING_GAS_LIMIT)
			c.store.SetGasMeter(gasMeter)
			if _, _, err := c.vm.Execute(c.checksum, api.MockEnv(), api.MockInfo("fred", nil), msg, c.store, c.goapi, c.querier, gasMeter, TESTING_GAS_LIMIT, deserCost); err != nil {
				b.Fatal(err)
			}
		}
		reportCHeap(b)
	})

	b.Run("query", func(b *testing.B) {
		c := instantiateHackatom(b, benchVM(b, TESTING_CACHE_SIZE))
		msg := []byte(`{"verifier":{}}`)
		for b.Loop() {
			gasMeter := api.NewMockGasMeter(TESTING_GAS_LIMIT)
			c.store.SetGasMeter(gasMeter)
			if _, _, err := c.vm.Query(c.checksum, api.MockEnv(), msg, c.store, c.goapi, c.querier, gasMeter, TESTING_GAS_LIMIT, deserCost); err != nil {
				b.Fatal(err)
			}
		}
		reportCHeap(b)
	})
}
