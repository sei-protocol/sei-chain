package node

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/utils/helpers"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

var _ proxy.EVMApplication = (*MockApp)(nil)

var (
	errMockAppNoExecution   = errors.New("mock app does not execute EVM calls")
	errMockAppMissingHeader = errors.New("mock app FinalizeBlock requires header")
	baseBalance             = *uint256.MustFromBig(new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000_000_000)))
)

type mockAppTransition int

const blocksToRetain = 10_000

const (
	mockAppTransitionInitialize mockAppTransition = iota
	mockAppTransitionFinalize
	mockAppTransitionCommit
)

func (t mockAppTransition) String() string {
	switch t {
	case mockAppTransitionInitialize:
		return "initialize"
	case mockAppTransitionFinalize:
		return "finalize"
	case mockAppTransitionCommit:
		return "commit"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

type mockAppState struct {
	nextNonce        map[common.Address]uint64
	lastBlockHeight  int64
	lastBlockAppHash []byte
	validators       []proxy.ValidatorUpdate
	nextTransition   mockAppTransition
}

// MockApp is an in-memory Autobahn application for EVM transaction load tests.
type MockApp struct {
	app         proxy.EVMApplication
	chainConfig *params.ChainConfig
	state       utils.RWMutex[*mockAppState]
	// hashes receives each committed block's state, BUD, and receipt hashes.
	// None means Commit does not report them.
	hashes utils.Option[builder.AppHashBuilder]
}

func NewMockApp(app proxy.EVMApplication) *MockApp {
	chainConfig := *params.AllDevChainProtocolChanges
	chainConfig.ChainID = new(big.Int).SetUint64(config.AutobahnEVMOnlyChainID)
	return &MockApp{
		app:         app,
		chainConfig: &chainConfig,
		state: utils.NewRWMutex(&mockAppState{
			nextNonce:      map[common.Address]uint64{},
			nextTransition: mockAppTransitionInitialize,
		}),
	}
}

func (app *MockApp) InitChain(req *proxy.RequestInitChain) (*proxy.ResponseInitChain, error) {
	if req.InitialHeight <= 0 {
		return nil, fmt.Errorf("mock app InitChain initial height must be > 0: %d", req.InitialHeight)
	}
	for state := range app.state.Lock() {
		if err := state.checkTransition(mockAppTransitionInitialize); err != nil {
			return nil, err
		}
		res, err := app.app.InitChain(req)
		if err != nil {
			return nil, err
		}
		validators := app.app.GetValidators()
		state.lastBlockHeight = req.InitialHeight - 1
		state.lastBlockAppHash = nil
		state.validators = slices.Clone(validators)
		state.nextTransition = mockAppTransitionFinalize
		return &proxy.ResponseInitChain{Validators: slices.Clone(res.Validators)}, nil
	}
	panic("unreachable")
}

func (app *MockApp) InitLastHeader(lastHeader *proxy.Header) {}

func (app *MockApp) Info() *proxy.ResponseInfo {
	info := *app.app.Info()
	for state := range app.state.RLock() {
		info.LastBlockHeight = state.lastBlockHeight
		info.LastBlockAppHash = slices.Clone(state.lastBlockAppHash)
		return &info
	}
	panic("unreachable")
}

func (app *MockApp) GetValidators() []proxy.ValidatorUpdate {
	for state := range app.state.RLock() {
		return slices.Clone(state.validators)
	}
	panic("unreachable")
}

func (app *MockApp) LastBlockHeight() int64 {
	for state := range app.state.RLock() {
		return state.lastBlockHeight
	}
	panic("unreachable")
}

func (app *MockApp) CheckTx(_ context.Context, req *proxy.RequestCheckTxV2) *proxy.ResponseCheckTxV2 {
	res, err := parseFastCheckTx(req.Tx)
	if err != nil {
		return &proxy.ResponseCheckTxV2{ResponseCheckTx: &proxy.ResponseCheckTx{Code: 1, Log: err.Error()}}
	}
	return res
}

// RememberEvmSender is a no-op because the mock app does not recover senders.
func (app *MockApp) RememberEvmSender(common.Hash, common.Address) {}

// ForgetEvmSender is a no-op because the mock app does not cache senders.
func (app *MockApp) ForgetEvmSender(common.Hash) {}

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

func (app *MockApp) EvmBalance(common.Address, []byte) uint256.Int { return baseBalance }

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

// EvmChainID returns the chain ID of the Autobahn EVM-only executor this app
// stands in for.
func (app *MockApp) EvmChainID() uint64 { return config.AutobahnEVMOnlyChainID }

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
func (app *MockApp) EvmCallAt(context.Context, *ethcore.Message, *ethtypes.Header, evmonly.CallOverrides) (*ethcore.ExecutionResult, error) {
	return nil, errMockAppNoExecution
}

// EvmEstimateGasAt always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmEstimateGasAt(context.Context, *ethcore.Message, uint64, *ethtypes.Header, evmonly.CallOverrides) (uint64, []byte, error) {
	return 0, nil, errMockAppNoExecution
}

// EvmEstimateGasAfterCalls always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmEstimateGasAfterCalls(context.Context, []*ethcore.Message, *ethcore.Message, uint64, evmonly.CallOverrides) (uint64, []byte, error) {
	return 0, nil, errMockAppNoExecution
}

// EvmEstimateGasAfterCallsAt always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmEstimateGasAfterCallsAt(context.Context, []*ethcore.Message, *ethcore.Message, uint64, *ethtypes.Header, evmonly.CallOverrides) (uint64, []byte, error) {
	return 0, nil, errMockAppNoExecution
}

// EvmCreateAccessList always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmCreateAccessList(context.Context, *ethcore.Message) (ethtypes.AccessList, *ethcore.ExecutionResult, error) {
	return nil, nil, errMockAppNoExecution
}

// EvmCreateAccessListAt always errors: the mock app keeps no EVM state to execute against.
func (app *MockApp) EvmCreateAccessListAt(context.Context, *ethcore.Message, *ethtypes.Header) (ethtypes.AccessList, *ethcore.ExecutionResult, error) {
	return nil, nil, errMockAppNoExecution
}

func (app *MockApp) EvmBaseFee() *big.Int { return new(big.Int) }

// EvmGasLimit returns 0: the mock app does not meter block gas.
func (app *MockApp) EvmGasLimit() uint64 { return 0 }

func (app *MockApp) EvmMinGasPrice() *big.Int { return new(big.Int) }

// EvmBlockContext always errors: the mock app keeps no EVM state to trace against.
func (app *MockApp) EvmBlockContext(*ethtypes.Header) (vm.BlockContext, error) {
	return vm.BlockContext{}, errMockAppNoExecution
}

// EvmStateAt always errors: the mock app keeps no EVM state to trace against.
func (app *MockApp) EvmStateAt(context.Context, int64) (vm.SeiStateDB, func(), error) {
	return nil, nil, errMockAppNoExecution
}

// EvmStateBeforeTx always errors: the mock app keeps no EVM state to trace against.
func (app *MockApp) EvmStateBeforeTx(context.Context, *ethtypes.Header, [][]byte) (vm.SeiStateDB, func(), error) {
	return nil, nil, errMockAppNoExecution
}

func (app *MockApp) PrepareBlock(context.Context, *proxy.RequestFinalizeBlock) error { return nil }

func (app *MockApp) FinalizeBlock(_ context.Context, req *proxy.RequestFinalizeBlock) (*proxy.ResponseFinalizeBlock, error) {
	if req.Header == nil {
		return nil, errMockAppMissingHeader
	}
	txs, err := parseMockAppTxs(req.Txs)
	if err != nil {
		return nil, err
	}
	for state := range app.state.Lock() {
		return state.finalizeBlock(req, txs)
	}
	panic("unreachable")
}

func (app *MockApp) Commit(ctx context.Context) (*proxy.ResponseCommit, error) {
	var height int64
	for state := range app.state.Lock() {
		if err := state.checkTransition(mockAppTransitionCommit); err != nil {
			return nil, err
		}
		height = state.lastBlockHeight
		break
	}
	if err := app.reportCommittedHashes(ctx, height); err != nil {
		return nil, err
	}
	for state := range app.state.Lock() {
		state.nextTransition = mockAppTransitionFinalize
		return &proxy.ResponseCommit{RetainHeight: max(state.lastBlockHeight-blocksToRetain, 0)}, nil
	}
	panic("unreachable")
}

// reportCommittedHashes reports zero state, BUD, and receipt hashes for a committed mock block.
func (app *MockApp) reportCommittedHashes(ctx context.Context, height int64) error {
	hashes, ok := app.hashes.Get()
	if !ok {
		return nil
	}
	if height < 1 {
		return fmt.Errorf("mock app commit height = %d, want >= 1", height)
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
	checkTx  *proxy.ResponseCheckTxV2
	parseErr error
}

func (state *mockAppState) finalizeBlock(req *proxy.RequestFinalizeBlock, txs []mockAppTx) (*proxy.ResponseFinalizeBlock, error) {
	if err := state.checkTransition(mockAppTransitionFinalize); err != nil {
		return nil, err
	}
	if req.Header.Height != state.lastBlockHeight+1 {
		return nil, fmt.Errorf("mock app FinalizeBlock non-sequential height: got %d, want %d", req.Header.Height, state.lastBlockHeight+1)
	}

	txResults := make([]*proxy.ExecTxResult, len(txs))
	for i, parsed := range txs {
		if parsed.parseErr != nil {
			txResults[i] = &proxy.ExecTxResult{
				Code: 1,
				Log:  parsed.parseErr.Error(),
			}
			continue
		}
		tx := parsed.checkTx
		wantNonce := state.nextNonce[tx.EVMSenderAddress]
		if tx.EVMNonce == wantNonce {
			txResults[i] = &proxy.ExecTxResult{
				Code:      proxy.CodeTypeOK,
				GasWanted: tx.GasWanted,
				GasUsed:   tx.GasWanted,
			}
			state.nextNonce[tx.EVMSenderAddress]++
		} else {
			logger.Warn(
				"unexpected nonce",
				"height", req.Header.Height,
				"addr", tx.EVMSenderAddress,
				"got", tx.EVMNonce,
				"want", wantNonce,
			)
			err := sdkerrors.ErrWrongSequence
			txResults[i] = &proxy.ExecTxResult{
				Codespace: err.Codespace(),
				Code:      err.ABCICode(),
				Log:       err.Error(),
			}
		}
	}
	state.lastBlockHeight = req.Header.Height
	state.lastBlockAppHash = mockAppHash(state.lastBlockAppHash, req.Hash)
	state.nextTransition = mockAppTransitionCommit
	return &proxy.ResponseFinalizeBlock{
		TxResults: txResults,
		AppHash:   slices.Clone(state.lastBlockAppHash),
	}, nil
}

func (state *mockAppState) checkTransition(want mockAppTransition) error {
	if state.nextTransition != want {
		return fmt.Errorf("mock app unexpected transition: got %s, want %s", state.nextTransition, want)
	}
	return nil
}

func parseMockAppTxs(txs [][]byte) ([]mockAppTx, error) {
	parsed := make([]mockAppTx, len(txs))
	workers := min(runtime.GOMAXPROCS(0), len(txs))
	if workers == 0 {
		return parsed, nil
	}
	if err := scope.Parallel(func(s scope.ParallelScope) error {
		for worker := range workers {
			s.Spawn(func() error {
				for i := worker; i < len(txs); i += workers {
					res, err := parseFastCheckTx(txs[i])
					if err != nil {
						parsed[i].parseErr = err
						continue
					}
					parsed[i].checkTx = res
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

const evmTxTypeURL = "/seiprotocol.seichain.evm.MsgEVMTransaction"

func parseFastCheckTx(txBytes []byte) (*proxy.ResponseCheckTxV2, error) {
	var rawTx tx.TxRaw
	if err := gogoproto.Unmarshal(txBytes, &rawTx); err != nil {
		return nil, err
	}
	var body tx.TxBody
	if err := gogoproto.Unmarshal(rawTx.BodyBytes, &body); err != nil {
		return nil, err
	}
	if len(body.Messages) != 1 {
		return nil, fmt.Errorf("fast check tx only accepts single-message EVM transactions")
	}
	anyMsg := body.Messages[0]
	if anyMsg == nil {
		return nil, fmt.Errorf("fast check tx received nil message")
	}
	if anyMsg.GetTypeUrl() != evmTxTypeURL {
		return nil, fmt.Errorf("fast check tx only accepts EVM transactions")
	}
	var msg evmtypes.MsgEVMTransaction
	if err := gogoproto.Unmarshal(anyMsg.Value, &msg); err != nil {
		return nil, err
	}
	ethTx, _ := msg.AsTransaction()
	if ethTx == nil {
		return nil, fmt.Errorf("failed to unpack EVM transaction")
	}
	gas, ok := utils.SafeCast[int64](ethTx.Gas())
	if !ok {
		return nil, fmt.Errorf("EVM gas wanted exceeds int64 max")
	}
	if !ethTx.Protected() {
		return nil, fmt.Errorf("unprotected EVM transaction")
	}
	// The mock-app load-test path intentionally recovers against
	// the transaction's own chain ID. The real app ante handler enforces the
	// genesis EVM chain ID; this lightweight path only needs sender/nonce data.
	signer := ethtypes.LatestSignerForChainID(ethTx.ChainId())
	evmAddr, seiAddr, _, err := helpers.RecoverAddressesFromTx(ethTx, signer, ethTx.ChainId())
	if err != nil {
		return nil, err
	}
	return &proxy.ResponseCheckTxV2{
		ResponseCheckTx:  &proxy.ResponseCheckTx{GasWanted: gas, GasEstimated: gas},
		IsEVM:            true,
		EVMNonce:         ethTx.Nonce(),
		EVMHash:          ethTx.Hash(),
		EVMSenderAddress: evmAddr,
		SeiSenderAddress: seiAddr,
	}, nil
}
