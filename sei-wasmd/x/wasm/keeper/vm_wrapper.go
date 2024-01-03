package keeper

import (
	"errors"
	"sync"
	"time"

	"github.com/CosmWasm/wasmd/x/wasm/types"
	wasmvm "github.com/CosmWasm/wasmvm"
	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
)

const CreateTimeout time.Duration = 5 * time.Second

type VMWrapper struct {
	types.WasmerEngine

	mu *sync.Mutex
}

func NewVMWrapper(inner types.WasmerEngine) types.WasmerEngine {
	return &VMWrapper{
		inner,
		&sync.Mutex{},
	}
}

func (w *VMWrapper) Create(code wasmvm.WasmCode) (checksum wasmvm.Checksum, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	timer := time.NewTimer(CreateTimeout)
	done := make(chan struct{}, 1)
	go func() {
		checksum, err = w.WasmerEngine.Create(code)
		done <- struct{}{}
	}()
	select {
	case <-done:
		timer.Stop()
		return
	case <-timer.C:
		err = errors.New("create wasm code timed out")
		return
	}
}

func (w *VMWrapper) Instantiate(
	checksum wasmvm.Checksum,
	env wasmvmtypes.Env,
	info wasmvmtypes.MessageInfo,
	initMsg []byte,
	store wasmvm.KVStore,
	goapi wasmvm.GoAPI,
	querier wasmvm.Querier,
	gasMeter wasmvm.GasMeter,
	gasLimit uint64,
	deserCost wasmvmtypes.UFraction,
) (*wasmvmtypes.Response, uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.WasmerEngine.Instantiate(checksum, env, info, initMsg, store, goapi, querier, gasMeter, gasLimit, deserCost)
}

func (w *VMWrapper) Execute(
	code wasmvm.Checksum,
	env wasmvmtypes.Env,
	info wasmvmtypes.MessageInfo,
	executeMsg []byte,
	store wasmvm.KVStore,
	goapi wasmvm.GoAPI,
	querier wasmvm.Querier,
	gasMeter wasmvm.GasMeter,
	gasLimit uint64,
	deserCost wasmvmtypes.UFraction,
) (*wasmvmtypes.Response, uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.WasmerEngine.Execute(code, env, info, executeMsg, store, goapi, querier, gasMeter, gasLimit, deserCost)
}

func (w *VMWrapper) Migrate(
	checksum wasmvm.Checksum,
	env wasmvmtypes.Env,
	migrateMsg []byte,
	store wasmvm.KVStore,
	goapi wasmvm.GoAPI,
	querier wasmvm.Querier,
	gasMeter wasmvm.GasMeter,
	gasLimit uint64,
	deserCost wasmvmtypes.UFraction,
) (*wasmvmtypes.Response, uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.WasmerEngine.Migrate(checksum, env, migrateMsg, store, goapi, querier, gasMeter, gasLimit, deserCost)
}

func (w *VMWrapper) Sudo(
	checksum wasmvm.Checksum,
	env wasmvmtypes.Env,
	sudoMsg []byte,
	store wasmvm.KVStore,
	goapi wasmvm.GoAPI,
	querier wasmvm.Querier,
	gasMeter wasmvm.GasMeter,
	gasLimit uint64,
	deserCost wasmvmtypes.UFraction,
) (*wasmvmtypes.Response, uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.WasmerEngine.Sudo(checksum, env, sudoMsg, store, goapi, querier, gasMeter, gasLimit, deserCost)
}

func (w *VMWrapper) Reply(
	checksum wasmvm.Checksum,
	env wasmvmtypes.Env,
	reply wasmvmtypes.Reply,
	store wasmvm.KVStore,
	goapi wasmvm.GoAPI,
	querier wasmvm.Querier,
	gasMeter wasmvm.GasMeter,
	gasLimit uint64,
	deserCost wasmvmtypes.UFraction,
) (*wasmvmtypes.Response, uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.WasmerEngine.Reply(checksum, env, reply, store, goapi, querier, gasMeter, gasLimit, deserCost)
}

func (w *VMWrapper) Unpin(checksum wasmvm.Checksum) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.WasmerEngine.Unpin(checksum)
}

func (w *VMWrapper) Pin(checksum wasmvm.Checksum) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.WasmerEngine.Pin(checksum)
}
