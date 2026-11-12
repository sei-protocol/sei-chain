package proxy

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// EVMApplication executes EVM transactions for Autobahn and serves read-only
// EVM calls and execution parameters.
type EVMApplication interface {
	// Application state and initialization.
	Info() *ResponseInfo
	GetValidators() []ValidatorUpdate
	LastBlockHeight() int64
	InitChain(*RequestInitChain) (*ResponseInitChain, error)
	InitLastHeader(*Header)

	// Transaction admission and block execution.
	CheckTx(context.Context, *RequestCheckTxV2) *ResponseCheckTxV2
	EvmBalance(common.Address, []byte) uint256.Int
	EvmNonce(common.Address) uint64
	FinalizeBlock(context.Context, *RequestFinalizeBlock) (*ResponseFinalizeBlock, error)
	Commit(context.Context) (*ResponseCommit, error)
	// AwaitCommits waits for finalized blocks to reach the store and returns any commit failure.
	AwaitCommits() error

	// EvmCode returns the contract code at address in the current EVM state.
	EvmCode(address common.Address) []byte
	// EvmStorage returns the value at key in address's current EVM storage.
	EvmStorage(address common.Address, key common.Hash) common.Hash
	// EvmBalanceAt returns address's balance in the committed EVM state after height.
	EvmBalanceAt(address common.Address, height int64) (uint256.Int, error)
	// EvmCodeAt returns the contract code at address in the committed EVM state after height.
	EvmCodeAt(address common.Address, height int64) ([]byte, error)
	// EvmNonceAt returns address's nonce in the committed EVM state after height.
	EvmNonceAt(address common.Address, height int64) (uint64, error)
	// EvmStorageAt returns the value at key in address's committed EVM storage after height.
	EvmStorageAt(address common.Address, key common.Hash, height int64) (common.Hash, error)

	// EvmCall executes msg as a read-only call against the current EVM state.
	EvmCall(context.Context, *core.Message) (*core.ExecutionResult, error)
	// EvmEstimateGas returns the lowest gas limit at or below gasCap that lets
	// msg succeed, and any revert data.
	EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error)
	// EvmCallAt executes msg as a read-only call against the committed EVM
	// state after header's block, in the context that block executed at.
	EvmCallAt(ctx context.Context, msg *core.Message, header *ethtypes.Header) (*core.ExecutionResult, error)
	// EvmEstimateGasAt returns the lowest gas limit at or below gasCap that
	// lets msg succeed against the committed EVM state after header's block,
	// and any revert data.
	EvmEstimateGasAt(ctx context.Context, msg *core.Message, gasCap uint64, header *ethtypes.Header) (uint64, []byte, error)
	// EvmEstimateGasAfterCalls returns what EvmEstimateGas returns for msg with calls applied to
	// the current EVM state first, in order.
	EvmEstimateGasAfterCalls(ctx context.Context, calls []*core.Message, msg *core.Message, gasCap uint64) (uint64, []byte, error)
	// EvmCreateAccessList returns the access list msg touches against the current EVM state, and
	// the result of executing msg with that list.
	EvmCreateAccessList(ctx context.Context, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error)
	// EvmChainID returns the EVM chain ID transactions are signed for.
	EvmChainID() uint64
	// EvmChainConfig returns the EVM chain configuration transactions execute
	// against.
	EvmChainConfig() *params.ChainConfig
	// EvmBaseFee returns the base fee every block executes at.
	EvmBaseFee() *big.Int
	// EvmGasLimit returns the gas limit of the most recently committed block.
	EvmGasLimit() uint64
	// EvmMinGasPrice returns the minimum effective gas price a transaction is
	// admitted at.
	EvmMinGasPrice() *big.Int
	// EvmBlockContext returns the block context header's block executed at.
	EvmBlockContext(header *ethtypes.Header) (vm.BlockContext, error)
	// EvmStateAt returns the committed EVM state after height, or the latest
	// committed state when height is rpc.LatestBlockNumber, and the function
	// that releases it.
	EvmStateAt(ctx context.Context, height int64) (vm.SeiStateDB, func(), error)
	// EvmStateBeforeTx returns the EVM state the transaction at index len(txs)
	// of header's block executed against, and the function that releases it.
	EvmStateBeforeTx(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.SeiStateDB, func(), error)
}

// EVMProxy wraps an EVMApplication and records EVM method timings.
type EVMProxy struct {
	app EVMApplication
}

// NewEVM wraps an EVM application and records its method timings.
func NewEVM(app EVMApplication) *EVMProxy {
	return &EVMProxy{app: app}
}

func (app *EVMProxy) Info() *ResponseInfo {
	defer addTimeSample(Global.MethodTimingAt("info", "sync"))()
	return app.app.Info()
}

func (app *EVMProxy) GetValidators() []ValidatorUpdate {
	defer addTimeSample(Global.MethodTimingAt("get_validators", "sync"))()
	return app.app.GetValidators()
}

func (app *EVMProxy) LastBlockHeight() int64 {
	defer addTimeSample(Global.MethodTimingAt("last_block_height", "sync"))()
	return app.app.LastBlockHeight()
}

func (app *EVMProxy) InitChain(req *RequestInitChain) (*ResponseInitChain, error) {
	defer addTimeSample(Global.MethodTimingAt("init_chain", "sync"))()
	return app.app.InitChain(req)
}

func (app *EVMProxy) InitLastHeader(lastHeader *Header) {
	defer addTimeSample(Global.MethodTimingAt("init_last_header", "sync"))()
	app.app.InitLastHeader(lastHeader)
}

func (app *EVMProxy) CheckTxSafe(ctx context.Context, req *RequestCheckTxV2) (*ResponseCheckTxV2, error) {
	return runCheckTx(func() *ResponseCheckTxV2 { return app.app.CheckTx(ctx, req) }, func(res *ResponseCheckTxV2) error {
		return validateEVMMetadata(res.IsEVM, res.EVMHash, res.SeiSenderAddress)
	})
}

func (app *EVMProxy) EvmNonce(addr common.Address) uint64 {
	defer addTimeSample(Global.MethodTimingAt("evm_nonce", "sync"))()
	return app.app.EvmNonce(addr)
}

func (app *EVMProxy) FinalizeBlock(ctx context.Context, req *RequestFinalizeBlock) (*ResponseFinalizeBlock, error) {
	defer addTimeSample(Global.MethodTimingAt("finalize_block", "sync"))()
	return app.app.FinalizeBlock(ctx, req)
}

func (app *EVMProxy) Commit(ctx context.Context) (*ResponseCommit, error) {
	defer addTimeSample(Global.MethodTimingAt("commit", "sync"))()
	return app.app.Commit(ctx)
}

func (app *EVMProxy) EvmCall(ctx context.Context, msg *core.Message) (*core.ExecutionResult, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_call", "sync"))()
	return app.app.EvmCall(ctx, msg)
}

func (app *EVMProxy) EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_estimate_gas", "sync"))()
	return app.app.EvmEstimateGas(ctx, msg, gasCap)
}

func (app *EVMProxy) EvmCallAt(ctx context.Context, msg *core.Message, header *ethtypes.Header) (*core.ExecutionResult, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_call_at", "sync"))()
	return app.app.EvmCallAt(ctx, msg, header)
}

func (app *EVMProxy) EvmEstimateGasAt(ctx context.Context, msg *core.Message, gasCap uint64, header *ethtypes.Header) (uint64, []byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_estimate_gas_at", "sync"))()
	return app.app.EvmEstimateGasAt(ctx, msg, gasCap, header)
}

func (app *EVMProxy) EvmEstimateGasAfterCalls(ctx context.Context, calls []*core.Message, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_estimate_gas_after_calls", "sync"))()
	return app.app.EvmEstimateGasAfterCalls(ctx, calls, msg, gasCap)
}

func (app *EVMProxy) EvmCreateAccessList(ctx context.Context, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_create_access_list", "sync"))()
	return app.app.EvmCreateAccessList(ctx, msg)
}

func (app *EVMProxy) EvmChainID() uint64 {
	defer addTimeSample(Global.MethodTimingAt("evm_chain_id", "sync"))()
	return app.app.EvmChainID()
}

func (app *EVMProxy) EvmChainConfig() *params.ChainConfig {
	defer addTimeSample(Global.MethodTimingAt("evm_chain_config", "sync"))()
	return app.app.EvmChainConfig()
}

func (app *EVMProxy) EvmBaseFee() *big.Int {
	defer addTimeSample(Global.MethodTimingAt("evm_base_fee", "sync"))()
	return app.app.EvmBaseFee()
}

func (app *EVMProxy) EvmGasLimit() uint64 {
	defer addTimeSample(Global.MethodTimingAt("evm_gas_limit", "sync"))()
	return app.app.EvmGasLimit()
}

func (app *EVMProxy) EvmMinGasPrice() *big.Int {
	defer addTimeSample(Global.MethodTimingAt("evm_min_gas_price", "sync"))()
	return app.app.EvmMinGasPrice()
}

func (app *EVMProxy) EvmBlockContext(header *ethtypes.Header) (vm.BlockContext, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_block_context", "sync"))()
	return app.app.EvmBlockContext(header)
}

func (app *EVMProxy) EvmStateAt(ctx context.Context, height int64) (vm.SeiStateDB, func(), error) {
	defer addTimeSample(Global.MethodTimingAt("evm_state_at", "sync"))()
	return app.app.EvmStateAt(ctx, height)
}

func (app *EVMProxy) EvmStateBeforeTx(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.SeiStateDB, func(), error) {
	defer addTimeSample(Global.MethodTimingAt("evm_state_before_tx", "sync"))()
	return app.app.EvmStateBeforeTx(ctx, header, txs)
}

func (app *EVMProxy) AwaitCommits() error {
	defer addTimeSample(Global.MethodTimingAt("await_commits", "sync"))()
	return app.app.AwaitCommits()
}

func (app *EVMProxy) EvmBalance(addr common.Address, seiAddr []byte) uint256.Int {
	defer addTimeSample(Global.MethodTimingAt("evm_balance", "sync"))()
	return app.app.EvmBalance(addr, seiAddr)
}

func (app *EVMProxy) EvmCode(addr common.Address) ([]byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_code", "sync"))()
	return app.app.EvmCode(addr), nil
}

func (app *EVMProxy) EvmStorage(addr common.Address, key common.Hash) (common.Hash, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_storage", "sync"))()
	return app.app.EvmStorage(addr, key), nil
}

func (app *EVMProxy) EvmBalanceAt(addr common.Address, height int64) (uint256.Int, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_balance_at", "sync"))()
	return app.app.EvmBalanceAt(addr, height)
}

func (app *EVMProxy) EvmCodeAt(addr common.Address, height int64) ([]byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_code_at", "sync"))()
	return app.app.EvmCodeAt(addr, height)
}

func (app *EVMProxy) EvmNonceAt(addr common.Address, height int64) (uint64, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_nonce_at", "sync"))()
	return app.app.EvmNonceAt(addr, height)
}

func (app *EVMProxy) EvmStorageAt(addr common.Address, key common.Hash, height int64) (common.Hash, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_storage_at", "sync"))()
	return app.app.EvmStorageAt(addr, key, height)
}
