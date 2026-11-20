package node

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"runtime"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

var _ proxy.EVMApplication = (*MockApp)(nil)

var (
	errMockAppNoExecution = errors.New("mock app does not execute EVM calls")
	baseBalance           = *uint256.MustFromBig(new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000_000_000)))
)

type mockAppTransition int

const (
	mockAppTransitionInitialize mockAppTransition = iota
	mockAppTransitionFinalize
	mockAppTransitionReportHashes
)

func (t mockAppTransition) String() string {
	switch t {
	case mockAppTransitionInitialize:
		return "initialize"
	case mockAppTransitionFinalize:
		return "finalize"
	case mockAppTransitionReportHashes:
		return "report_hashes"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

type mockAppState struct {
	nextNonce        map[common.Address]uint64
	lastBlockHeight  int64
	lastBlockAppHash []byte
	validators       proxy.CommitteeWeights
	nextTransition   mockAppTransition
}

// MockApp is an in-memory Autobahn application for EVM transaction load tests.
type MockApp struct {
	app         proxy.EVMApplication
	chainConfig *params.ChainConfig
	state       utils.RWMutex[*mockAppState]
	// hashes receives each finalized block's state, BUD, and receipt hashes.
	hashes utils.Option[builder.AppHashBuilder]
}

func NewMockApp(app proxy.EVMApplication) *MockApp {
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = new(big.Int).SetUint64(app.EvmChainID())
	return &MockApp{
		app:         app,
		chainConfig: &chainConfig,
		state: utils.NewRWMutex(&mockAppState{
			nextNonce:      map[common.Address]uint64{},
			nextTransition: mockAppTransitionInitialize,
		}),
	}
}

func (app *MockApp) InitChain(req proxy.InitConfig) error {
	initialHeight, ok := utils.SafeCast[int64](req.InitialHeight)
	if !ok || initialHeight <= 0 {
		return fmt.Errorf("mock app InitChain initial height must be > 0: %d", req.InitialHeight)
	}
	for state := range app.state.Lock() {
		if err := state.checkTransition(mockAppTransitionInitialize); err != nil {
			return err
		}
		err := app.app.InitChain(req)
		if err != nil {
			return err
		}
		validators := app.app.GetValidators()
		state.lastBlockHeight = initialHeight - 1
		state.lastBlockAppHash = nil
		state.validators = maps.Clone(validators)
		state.nextTransition = mockAppTransitionFinalize
		return nil
	}
	panic("unreachable")
}

func (app *MockApp) RestoreBlockTime(time.Time) {}

func (app *MockApp) Info() *proxy.ResponseInfo {
	info := *app.app.Info()
	for state := range app.state.RLock() {
		info.LastBlockHeight = state.lastBlockHeight
		info.LastBlockAppHash = slices.Clone(state.lastBlockAppHash)
		return &info
	}
	panic("unreachable")
}

func (app *MockApp) GetValidators() proxy.CommitteeWeights {
	for state := range app.state.RLock() {
		return maps.Clone(state.validators)
	}
	panic("unreachable")
}

func (app *MockApp) LastBlockHeight() int64 {
	for state := range app.state.RLock() {
		return state.lastBlockHeight
	}
	panic("unreachable")
}

func (app *MockApp) CheckTx(tx proxy.EvmTx) (proxy.AdmittedTx, error) {
	return checkMockTx(tx)
}

func (app *MockApp) EvmNonce(addr common.Address) uint64 {
	for state := range app.state.RLock() {
		return state.nextNonce[addr]
	}
	panic("unreachable")
}

// EvmCommittedNonce is the same as EvmNonce: the mock app commits synchronously.
func (app *MockApp) EvmCommittedNonce(addr common.Address) uint64 {
	return app.EvmNonce(addr)
}

func (app *MockApp) EvmBalance(common.Address) uint256.Int { return baseBalance }

// AwaitCommits returns immediately.
func (app *MockApp) AwaitCommits() error { return nil }

// EvmCode returns no contract code.
func (app *MockApp) EvmCode(common.Address) []byte { return nil }

// EvmStorage returns zero for every storage slot.
func (app *MockApp) EvmStorage(common.Address, common.Hash) common.Hash { return common.Hash{} }

func (app *MockApp) EvmBalanceAt(common.Address, int64) (uint256.Int, error) {
	return uint256.Int{}, errMockAppNoExecution
}

func (app *MockApp) EvmCodeAt(common.Address, int64) ([]byte, error) {
	return nil, errMockAppNoExecution
}

func (app *MockApp) EvmNonceAt(common.Address, int64) (uint64, error) {
	return 0, errMockAppNoExecution
}

func (app *MockApp) EvmStorageAt(common.Address, common.Hash, int64) (common.Hash, error) {
	return common.Hash{}, errMockAppNoExecution
}

// EvmChainID returns the chain ID of the EVM-only app this app wraps.
func (app *MockApp) EvmChainID() uint64 { return app.app.EvmChainID() }

// EvmChainConfig returns the dev chain configuration with the Autobahn EVM-only
// chain ID.
func (app *MockApp) EvmChainConfig() *params.ChainConfig { return app.chainConfig }

// EvmCall always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmCall(context.Context, *ethcore.Message, evmonly.CallOverrides) (*ethcore.ExecutionResult, error) {
	return nil, errMockAppNoExecution
}

// EvmEstimateGas always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmEstimateGas(context.Context, *ethcore.Message, uint64, evmonly.CallOverrides) (uint64, []byte, error) {
	return 0, nil, errMockAppNoExecution
}

// EvmCallAt always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmCallAt(context.Context, *ethcore.Message, proxy.BlockRef, evmonly.CallOverrides) (*ethcore.ExecutionResult, error) {
	return nil, errMockAppNoExecution
}

// EvmEstimateGasAt always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmEstimateGasAt(context.Context, *ethcore.Message, uint64, proxy.BlockRef, evmonly.CallOverrides) (uint64, []byte, error) {
	return 0, nil, errMockAppNoExecution
}

// EvmEstimateGasAfterCalls always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmEstimateGasAfterCalls(context.Context, []*ethcore.Message, *ethcore.Message, uint64, evmonly.CallOverrides) (uint64, []byte, error) {
	return 0, nil, errMockAppNoExecution
}

// EvmEstimateGasAfterCallsAt always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmEstimateGasAfterCallsAt(context.Context, []*ethcore.Message, *ethcore.Message, uint64, proxy.BlockRef, evmonly.CallOverrides) (uint64, []byte, error) {
	return 0, nil, errMockAppNoExecution
}

// EvmCreateAccessList always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmCreateAccessList(context.Context, *ethcore.Message) (ethtypes.AccessList, *ethcore.ExecutionResult, error) {
	return nil, nil, errMockAppNoExecution
}

// EvmCreateAccessListAt always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmCreateAccessListAt(context.Context, *ethcore.Message, proxy.BlockRef) (ethtypes.AccessList, *ethcore.ExecutionResult, error) {
	return nil, nil, errMockAppNoExecution
}

func (app *MockApp) EvmBaseFee() *big.Int { return new(big.Int) }

// EvmGasLimit returns 0: the mock app does not meter block gas.
func (app *MockApp) EvmGasLimit() uint64 { return 0 }

func (app *MockApp) EvmMinGasPrice() *big.Int { return new(big.Int) }

// EvmBlockContext always errors: the mock app keeps no EVM state to trace against.
func (app *MockApp) EvmBlockContext(proxy.BlockRef) (vm.BlockContext, error) {
	return vm.BlockContext{}, errMockAppNoExecution
}

// EvmStateAt always errors: the mock app keeps no EVM state to trace against.
func (app *MockApp) EvmStateAt(context.Context, int64) (*evmonly.StateHandle, error) {
	return nil, errMockAppNoExecution
}

// EvmStateBeforeTx always errors: the mock app keeps no EVM state to trace against.
func (app *MockApp) EvmStateBeforeTx(context.Context, proxy.BlockRef, [][]byte) (*evmonly.StateHandle, error) {
	return nil, errMockAppNoExecution
}

func (app *MockApp) FinalizeBlock(ctx context.Context, req proxy.FinalizeBlockReq) (proxy.FinalizeBlockResp, error) {
	txs, err := parseMockAppTxs(req.Txs)
	if err != nil {
		return proxy.FinalizeBlockResp{}, err
	}
	var response proxy.FinalizeBlockResp
	var height int64
	for state := range app.state.Lock() {
		response, err = state.finalizeBlock(req, txs)
		if err != nil {
			return proxy.FinalizeBlockResp{}, err
		}
		height = state.lastBlockHeight
	}
	if err := app.reportFinalizedHashes(ctx, height); err != nil {
		return proxy.FinalizeBlockResp{}, err
	}
	for state := range app.state.Lock() {
		state.nextTransition = mockAppTransitionFinalize
	}
	return response, nil
}

// reportFinalizedHashes reports zero state, BUD, and receipt hashes for a finalized mock block.
func (app *MockApp) reportFinalizedHashes(ctx context.Context, height int64) error {
	hashes, ok := app.hashes.Get()
	if !ok {
		return nil
	}
	if height < 1 {
		return fmt.Errorf("mock app finalized height = %d, want >= 1", height)
	}
	//nolint:gosec // height is at least 1
	blockHeight := uint64(height)
	var zero [32]byte
	if err := hashes.ReportReceiptHash(ctx, blockHeight, apphash.ReceiptHash(zero)); err != nil {
		return fmt.Errorf("report the receipt hash of block %d: %w", blockHeight, err)
	}
	if err := hashes.ReportStateHash(ctx, blockHeight, zero, nil); err != nil {
		return fmt.Errorf("report the state hash of block %d: %w", blockHeight, err)
	}
	if err := hashes.ReportBUDHash(ctx, blockHeight, zero); err != nil {
		return fmt.Errorf("report the BUD hash of block %d: %w", blockHeight, err)
	}
	return nil
}

type mockAppTx struct {
	tx       proxy.EvmTx
	parseErr error
}

func (state *mockAppState) finalizeBlock(req proxy.FinalizeBlockReq, txs []mockAppTx) (proxy.FinalizeBlockResp, error) {
	if err := state.checkTransition(mockAppTransitionFinalize); err != nil {
		return proxy.FinalizeBlockResp{}, err
	}
	height, ok := utils.SafeCast[int64](req.Height)
	if !ok || height != state.lastBlockHeight+1 {
		return proxy.FinalizeBlockResp{}, fmt.Errorf("mock app FinalizeBlock non-sequential height: got %d, want %d", req.Height, state.lastBlockHeight+1)
	}
	var gasUsed uint64
	for _, parsed := range txs {
		if parsed.parseErr != nil {
			continue
		}
		tx := parsed.tx
		sender := parsed.tx.Sender()
		wantNonce := state.nextNonce[sender]
		if tx.Nonce() == wantNonce {
			gasUsed += tx.Gas()
			state.nextNonce[sender]++
		} else {
			logger.Warn("unexpected nonce", "height", req.Height, "addr", sender, "got", tx.Nonce(), "want", wantNonce)
		}
	}
	state.lastBlockHeight = height
	state.lastBlockAppHash = mockAppHash(state.lastBlockAppHash, req.Hash[:])
	state.nextTransition = mockAppTransitionReportHashes
	return proxy.FinalizeBlockResp{GasUsed: gasUsed}, nil
}

func (state *mockAppState) checkTransition(want mockAppTransition) error {
	if state.nextTransition != want {
		return fmt.Errorf("mock app unexpected transition: got %s, want %s", state.nextTransition, want)
	}
	return nil
}

func parseMockAppTxs(txs []proxy.EvmTx) ([]mockAppTx, error) {
	parsed := make([]mockAppTx, len(txs))
	workers := min(runtime.GOMAXPROCS(0), len(txs))
	if workers == 0 {
		return parsed, nil
	}
	if err := scope.Parallel(func(s scope.ParallelScope) error {
		for worker := range workers {
			s.Spawn(func() error {
				for i := worker; i < len(txs); i += workers {
					_, err := checkMockTx(txs[i])
					if err != nil {
						parsed[i].parseErr = err
						continue
					}
					parsed[i].tx = txs[i]
				}
				return nil
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return parsed, nil
}

func mockAppHash(prevAppHash []byte, blockHash []byte) []byte {
	h := sha256.New()
	_, _ = h.Write(prevAppHash)
	_, _ = h.Write(blockHash)
	return h.Sum(nil)
}

func checkMockTx(ethTx proxy.EvmTx) (proxy.AdmittedTx, error) {
	gas := ethTx.Gas()
	if gas > math.MaxInt64 {
		return proxy.AdmittedTx{}, fmt.Errorf("EVM gas wanted exceeds int64 max")
	}
	return proxy.AdmittedTx{GasEstimated: gas}, nil
}
