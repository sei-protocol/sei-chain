package wasmtesting

import (
	"bytes"
	"crypto/sha256"

	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	wasmvm "github.com/sei-protocol/sei-chain/sei-wasmvm"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

var _ types.WasmerEngine = &MockWasmer{}

// MockWasmer implements types.WasmerEngine for testing purpose. One or multiple messages can be stubbed.
// Without a stub function a panic is thrown.
type MockWasmer struct {
	CreateFn      func(codeID wasmvm.WasmCode) (wasmvm.Checksum, error)
	AnalyzeCodeFn func(codeID wasmvm.Checksum) (*wasmvmtypes.AnalysisReport, error)
	InstantiateFn func(codeID wasmvm.Checksum, env wasmvmtypes.Env, info wasmvmtypes.MessageInfo, initMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error)
	ExecuteFn     func(codeID wasmvm.Checksum, env wasmvmtypes.Env, info wasmvmtypes.MessageInfo, executeMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error)
	QueryFn       func(codeID wasmvm.Checksum, env wasmvmtypes.Env, queryMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) ([]byte, uint64, error)
	MigrateFn     func(codeID wasmvm.Checksum, env wasmvmtypes.Env, migrateMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error)
	SudoFn        func(codeID wasmvm.Checksum, env wasmvmtypes.Env, sudoMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error)
	ReplyFn       func(codeID wasmvm.Checksum, env wasmvmtypes.Env, reply wasmvmtypes.Reply, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error)
	GetCodeFn     func(codeID wasmvm.Checksum) (wasmvm.WasmCode, error)
	CleanupFn     func()
	PinFn         func(checksum wasmvm.Checksum) error
	UnpinFn       func(checksum wasmvm.Checksum) error
	GetMetricsFn  func() (*wasmvmtypes.Metrics, error)
}

func (m *MockWasmer) Create(codeID wasmvm.WasmCode) (wasmvm.Checksum, error) {
	if m.CreateFn == nil {
		panic("not supposed to be called!")
	}
	return m.CreateFn(codeID)
}

func (m *MockWasmer) AnalyzeCode(codeID wasmvm.Checksum) (*wasmvmtypes.AnalysisReport, error) {
	if m.AnalyzeCodeFn == nil {
		panic("not supposed to be called!")
	}
	return m.AnalyzeCodeFn(codeID)
}

func (m *MockWasmer) Instantiate(codeID wasmvm.Checksum, env wasmvmtypes.Env, info wasmvmtypes.MessageInfo, initMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
	if m.InstantiateFn == nil {
		panic("not supposed to be called!")
	}
	return m.InstantiateFn(codeID, env, info, initMsg, store, goapi, querier, gasMeter, gasLimit, deserCost)
}

func (m *MockWasmer) Execute(codeID wasmvm.Checksum, env wasmvmtypes.Env, info wasmvmtypes.MessageInfo, executeMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
	if m.ExecuteFn == nil {
		panic("not supposed to be called!")
	}
	return m.ExecuteFn(codeID, env, info, executeMsg, store, goapi, querier, gasMeter, gasLimit, deserCost)
}

func (m *MockWasmer) Query(codeID wasmvm.Checksum, env wasmvmtypes.Env, queryMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) ([]byte, uint64, error) {
	if m.QueryFn == nil {
		panic("not supposed to be called!")
	}
	return m.QueryFn(codeID, env, queryMsg, store, goapi, querier, gasMeter, gasLimit, deserCost)
}

func (m *MockWasmer) Migrate(codeID wasmvm.Checksum, env wasmvmtypes.Env, migrateMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
	if m.MigrateFn == nil {
		panic("not supposed to be called!")
	}
	return m.MigrateFn(codeID, env, migrateMsg, store, goapi, querier, gasMeter, gasLimit, deserCost)
}

func (m *MockWasmer) Sudo(codeID wasmvm.Checksum, env wasmvmtypes.Env, sudoMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
	if m.SudoFn == nil {
		panic("not supposed to be called!")
	}
	return m.SudoFn(codeID, env, sudoMsg, store, goapi, querier, gasMeter, gasLimit, deserCost)
}

func (m *MockWasmer) Reply(codeID wasmvm.Checksum, env wasmvmtypes.Env, reply wasmvmtypes.Reply, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
	if m.ReplyFn == nil {
		panic("not supposed to be called!")
	}
	return m.ReplyFn(codeID, env, reply, store, goapi, querier, gasMeter, gasLimit, deserCost)
}

func (m *MockWasmer) GetCode(codeID wasmvm.Checksum) (wasmvm.WasmCode, error) {
	if m.GetCodeFn == nil {
		panic("not supposed to be called!")
	}
	return m.GetCodeFn(codeID)
}

func (m *MockWasmer) Cleanup() {
	if m.CleanupFn == nil {
		panic("not supposed to be called!")
	}
	m.CleanupFn()
}

func (m *MockWasmer) Pin(checksum wasmvm.Checksum) error {
	if m.PinFn == nil {
		panic("not supposed to be called!")
	}
	return m.PinFn(checksum)
}

func (m *MockWasmer) Unpin(checksum wasmvm.Checksum) error {
	if m.UnpinFn == nil {
		panic("not supposed to be called!")
	}
	return m.UnpinFn(checksum)
}

func (m *MockWasmer) GetMetrics() (*wasmvmtypes.Metrics, error) {
	if m.GetMetricsFn == nil {
		panic("not expected to be called")
	}
	return m.GetMetricsFn()
}

var AlwaysPanicMockWasmer = &MockWasmer{}

// SelfCallingInstMockWasmer prepares a Wasmer mock that calls itself on instantiation.
func SelfCallingInstMockWasmer(executeCalled *bool) *MockWasmer {
	return &MockWasmer{
		CreateFn: func(code wasmvm.WasmCode) (wasmvm.Checksum, error) {
			anyCodeID := bytes.Repeat([]byte{0x1}, 32)
			return anyCodeID, nil
		},
		InstantiateFn: func(codeID wasmvm.Checksum, env wasmvmtypes.Env, info wasmvmtypes.MessageInfo, initMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
			return &wasmvmtypes.Response{
				Messages: []wasmvmtypes.SubMsg{
					{Msg: wasmvmtypes.CosmosMsg{
						Wasm: &wasmvmtypes.WasmMsg{Execute: &wasmvmtypes.ExecuteMsg{ContractAddr: env.Contract.Address, Msg: []byte(`{}`)}},
					}},
				},
			}, 1, nil
		},
		AnalyzeCodeFn: WithoutIBCAnalyzeFn,
		ExecuteFn: func(codeID wasmvm.Checksum, env wasmvmtypes.Env, info wasmvmtypes.MessageInfo, executeMsg []byte, store wasmvm.KVStore, goapi wasmvm.GoAPI, querier wasmvm.Querier, gasMeter wasmvm.GasMeter, gasLimit uint64, deserCost wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
			*executeCalled = true
			return &wasmvmtypes.Response{}, 1, nil
		},
	}
}

// MakeInstantiable adds some noop functions to not fail when contract is used for instantiation
func MakeInstantiable(m *MockWasmer) {
	m.CreateFn = HashOnlyCreateFn
	m.InstantiateFn = NoOpInstantiateFn
	m.AnalyzeCodeFn = WithoutIBCAnalyzeFn
}

// MakeIBCInstantiable adds some noop functions to not fail when contract is used for instantiation
func MakeIBCInstantiable(m *MockWasmer) {
	MakeInstantiable(m)
	m.AnalyzeCodeFn = HasIBCAnalyzeFn
}

func HashOnlyCreateFn(code wasmvm.WasmCode) (wasmvm.Checksum, error) {
	if code == nil {
		return nil, sdkerrors.Wrap(types.ErrInvalid, "wasm code must not be nil")
	}
	hash := sha256.Sum256(code)
	return hash[:], nil
}

func NoOpInstantiateFn(wasmvm.Checksum, wasmvmtypes.Env, wasmvmtypes.MessageInfo, []byte, wasmvm.KVStore, wasmvm.GoAPI, wasmvm.Querier, wasmvm.GasMeter, uint64, wasmvmtypes.UFraction) (*wasmvmtypes.Response, uint64, error) {
	return &wasmvmtypes.Response{}, 0, nil
}

func NoOpCreateFn(_ wasmvm.WasmCode) (wasmvm.Checksum, error) {
	return rand.Bytes(32), nil
}

func HasIBCAnalyzeFn(wasmvm.Checksum) (*wasmvmtypes.AnalysisReport, error) {
	return &wasmvmtypes.AnalysisReport{
		HasIBCEntryPoints: true,
	}, nil
}

func WithoutIBCAnalyzeFn(wasmvm.Checksum) (*wasmvmtypes.AnalysisReport, error) {
	return &wasmvmtypes.AnalysisReport{}, nil
}
