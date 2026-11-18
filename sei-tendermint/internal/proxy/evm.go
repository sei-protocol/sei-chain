package proxy

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// ErrTxRejected identifies a transaction rejected during decoding or admission.
var ErrTxRejected = errors.New("transaction rejected")

// EVMApplication executes EVM transactions for Autobahn and serves read-only
// EVM calls and execution parameters.
type EVMApplication interface {
	// Application state and initialization.
	Info() *ResponseInfo
	GetValidators() []ValidatorUpdate
	LastBlockHeight() int64
	InitChain(InitConfig) error
	RestoreBlockTime(time.Time)

	// Transaction admission and block execution.
	// CheckTx returns admission metadata or an error rejecting the transaction.
	CheckTx(EvmTx) (AdmittedTx, error)
	EvmBalance(common.Address) uint256.Int
	EvmNonce(common.Address) uint64
	// EvmCommittedNonce returns addr's nonce in the last committed EVM state without waiting for a
	// commit in flight, so it can trail EvmNonce by the most recently finalized block.
	EvmCommittedNonce(common.Address) uint64
	FinalizeBlock(context.Context, FinalizeBlockReq) (FinalizeBlockResp, error)
	Commit(context.Context) error
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

	// EvmCall executes msg as a read-only call against the current EVM state,
	// with overrides applied.
	EvmCall(ctx context.Context, msg *core.Message, overrides evmonly.CallOverrides) (*core.ExecutionResult, error)
	// EvmEstimateGas returns the lowest gas limit at or below gasCap that lets
	// msg succeed with overrides applied, and any revert data.
	EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64, overrides evmonly.CallOverrides) (uint64, []byte, error)
	// EvmCallAt executes msg as a read-only call against the committed EVM
	// state after block, in the context that block executed at, with
	// overrides applied.
	EvmCallAt(ctx context.Context, msg *core.Message, block BlockRef, overrides evmonly.CallOverrides) (*core.ExecutionResult, error)
	// EvmEstimateGasAt returns the lowest gas limit at or below gasCap that
	// lets msg succeed against the committed EVM state after block,
	// with overrides applied, and any revert data.
	EvmEstimateGasAt(ctx context.Context, msg *core.Message, gasCap uint64, block BlockRef, overrides evmonly.CallOverrides) (uint64, []byte, error)
	// EvmEstimateGasAfterCalls returns what EvmEstimateGas returns for msg with overrides and
	// then calls applied to the current EVM state first, in order.
	EvmEstimateGasAfterCalls(ctx context.Context, calls []*core.Message, msg *core.Message, gasCap uint64, overrides evmonly.CallOverrides) (uint64, []byte, error)
	// EvmEstimateGasAfterCallsAt returns what EvmEstimateGasAt returns for msg with overrides and
	// then calls applied to the committed EVM state after block first, in order.
	EvmEstimateGasAfterCallsAt(ctx context.Context, calls []*core.Message, msg *core.Message, gasCap uint64, block BlockRef, overrides evmonly.CallOverrides) (uint64, []byte, error)
	// EvmCreateAccessList returns the access list msg touches against the current EVM state, and
	// the result of executing msg with that list.
	EvmCreateAccessList(ctx context.Context, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error)
	// EvmCreateAccessListAt returns the access list msg touches against the committed EVM state
	// after block, in the context that block executed at, and the result of executing
	// msg with that list.
	EvmCreateAccessListAt(ctx context.Context, msg *core.Message, block BlockRef) (ethtypes.AccessList, *core.ExecutionResult, error)
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
	// EvmBlockContext returns the block context block executed at.
	EvmBlockContext(block BlockRef) (vm.BlockContext, error)
	// EvmStateAt returns the committed EVM state after height, or the latest
	// committed state when height is rpc.LatestBlockNumber, held until the returned handle is closed.
	EvmStateAt(ctx context.Context, height int64) (*evmonly.StateHandle, error)
	// EvmStateBeforeTx returns the EVM state the transaction at index len(txs)
	// of block executed against, held until the returned handle is closed.
	EvmStateBeforeTx(ctx context.Context, block BlockRef, txs [][]byte) (*evmonly.StateHandle, error)
}

// EVMProxy records application method timings and adapts EVM RPC inputs and results.
type EVMProxy struct {
	app            EVMApplication
	checkedSenders utils.Mutex[*senderCache]
}

// NewEVM wraps an EVM application and records its method timings.
func NewEVM(app EVMApplication) *EVMProxy {
	return &EVMProxy{app: app, checkedSenders: utils.NewMutex(new(newSenderCache(checkedSendersCap)))}
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

func (app *EVMProxy) InitChain(req InitConfig) error {
	defer addTimeSample(Global.MethodTimingAt("init_chain", "sync"))()
	return app.app.InitChain(req)
}

func (app *EVMProxy) RestoreBlockTime(blockTime time.Time) {
	defer addTimeSample(Global.MethodTimingAt("init_last_header", "sync"))()
	app.app.RestoreBlockTime(blockTime)
}

// CheckTx checks a transaction with a verified sender for admission and marks rejection errors with ErrTxRejected.
func (app *EVMProxy) CheckTx(tx EvmTx) (AdmittedTx, error) {
	defer addTimeSample(Global.MethodTimingAt("check_tx", "sync"))()
	res, err := app.app.CheckTx(tx)
	if err != nil {
		return AdmittedTx{}, fmt.Errorf("%w: %w", ErrTxRejected, err)
	}
	app.RememberEvmSender(tx.Hash(), tx.Sender)
	return res, nil
}

// RememberEvmSender records the sender recovered for the transaction with hash.
func (app *EVMProxy) RememberEvmSender(hash common.Hash, sender common.Address) {
	for senders := range app.checkedSenders.Lock() {
		senders.put(hash, sender)
	}
}

// ForgetEvmSender drops the sender recorded for hash, if any.
func (app *EVMProxy) ForgetEvmSender(hash common.Hash) {
	for senders := range app.checkedSenders.Lock() {
		senders.take(hash)
	}
}

func (app *EVMProxy) EvmNonce(addr common.Address) uint64 {
	defer addTimeSample(Global.MethodTimingAt("evm_nonce", "sync"))()
	return app.app.EvmNonce(addr)
}

// EvmCommittedNonce returns addr's nonce in the wrapped application's committed EVM state without
// waiting for a commit in flight.
func (app *EVMProxy) EvmCommittedNonce(addr common.Address) uint64 {
	defer addTimeSample(Global.MethodTimingAt("evm_committed_nonce", "sync"))()
	return app.app.EvmCommittedNonce(addr)
}

func (app *EVMProxy) FinalizeBlock(ctx context.Context, req FinalizeBlockReq) (FinalizeBlockResp, error) {
	defer addTimeSample(Global.MethodTimingAt("finalize_block", "sync"))()
	for senders := range app.checkedSenders.Lock() {
		for _, tx := range req.Txs {
			senders.take(tx.Hash())
		}
	}
	return app.app.FinalizeBlock(ctx, req)
}

func (app *EVMProxy) Commit(ctx context.Context) error {
	defer addTimeSample(Global.MethodTimingAt("commit", "sync"))()
	return app.app.Commit(ctx)
}

func (app *EVMProxy) EvmCall(ctx context.Context, msg *core.Message, overrides evmonly.CallOverrides) (*core.ExecutionResult, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_call", "sync"))()
	return app.app.EvmCall(ctx, msg, overrides)
}

func (app *EVMProxy) EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_estimate_gas", "sync"))()
	return app.app.EvmEstimateGas(ctx, msg, gasCap, overrides)
}

func (app *EVMProxy) EvmCallAt(ctx context.Context, msg *core.Message, header *ethtypes.Header, overrides evmonly.CallOverrides) (*core.ExecutionResult, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_call_at", "sync"))()
	block, err := executionBlockRef(header)
	if err != nil {
		return nil, err
	}
	return app.app.EvmCallAt(ctx, msg, block, overrides)
}

func (app *EVMProxy) EvmEstimateGasAt(ctx context.Context, msg *core.Message, gasCap uint64, header *ethtypes.Header, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_estimate_gas_at", "sync"))()
	block, err := executionBlockRef(header)
	if err != nil {
		return 0, nil, err
	}
	return app.app.EvmEstimateGasAt(ctx, msg, gasCap, block, overrides)
}

func (app *EVMProxy) EvmEstimateGasAfterCalls(ctx context.Context, calls []*core.Message, msg *core.Message, gasCap uint64, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_estimate_gas_after_calls", "sync"))()
	return app.app.EvmEstimateGasAfterCalls(ctx, calls, msg, gasCap, overrides)
}

func (app *EVMProxy) EvmEstimateGasAfterCallsAt(ctx context.Context, calls []*core.Message, msg *core.Message, gasCap uint64, header *ethtypes.Header, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_estimate_gas_after_calls_at", "sync"))()
	block, err := executionBlockRef(header)
	if err != nil {
		return 0, nil, err
	}
	return app.app.EvmEstimateGasAfterCallsAt(ctx, calls, msg, gasCap, block, overrides)
}

func (app *EVMProxy) EvmCreateAccessList(ctx context.Context, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_create_access_list", "sync"))()
	return app.app.EvmCreateAccessList(ctx, msg)
}

func (app *EVMProxy) EvmCreateAccessListAt(ctx context.Context, msg *core.Message, header *ethtypes.Header) (ethtypes.AccessList, *core.ExecutionResult, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_create_access_list_at", "sync"))()
	block, err := executionBlockRef(header)
	if err != nil {
		return nil, nil, err
	}
	return app.app.EvmCreateAccessListAt(ctx, msg, block)
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
	block, err := executionBlockRef(header)
	if err != nil {
		return vm.BlockContext{}, err
	}
	return app.app.EvmBlockContext(block)
}

func (app *EVMProxy) EvmStateAt(ctx context.Context, height int64) (vm.SeiStateDB, func(), error) {
	defer addTimeSample(Global.MethodTimingAt("evm_state_at", "sync"))()
	state, err := app.app.EvmStateAt(ctx, height)
	if err != nil {
		return nil, nil, err
	}
	return state, state.Close, nil
}

func (app *EVMProxy) EvmStateBeforeTx(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.SeiStateDB, func(), error) {
	defer addTimeSample(Global.MethodTimingAt("evm_state_before_tx", "sync"))()
	block, err := executionBlockRef(header)
	if err != nil {
		return nil, nil, err
	}
	state, err := app.app.EvmStateBeforeTx(ctx, block, txs)
	if err != nil {
		return nil, nil, err
	}
	return state, state.Close, nil
}

func (app *EVMProxy) AwaitCommits() error {
	defer addTimeSample(Global.MethodTimingAt("await_commits", "sync"))()
	return app.app.AwaitCommits()
}

func (app *EVMProxy) EvmBalance(addr common.Address) uint256.Int {
	defer addTimeSample(Global.MethodTimingAt("evm_balance", "sync"))()
	return app.app.EvmBalance(addr)
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

// executionBlockRef validates the header fields consumed by EVM execution.
func executionBlockRef(header *ethtypes.Header) (BlockRef, error) {
	if header.Number == nil || !header.Number.IsInt64() || header.Number.Sign() < 0 {
		return BlockRef{}, fmt.Errorf("invalid EVM block number: %v", header.Number)
	}
	return BlockRef{Height: types.RoadIndex(header.Number.Uint64()), Time: header.Time, ParentHash: header.ParentHash}, nil
}

// DecodeTx decodes an Ethereum transaction and recovers its sender.
// Invalid encodings and signatures are marked with ErrTxRejected.
func DecodeTx(raw []byte) (EvmTx, error) {
	return decodeTx(raw, func(common.Hash) utils.Option[common.Address] {
		return utils.None[common.Address]()
	})
}

// DecodeTx decodes an Ethereum transaction and recovers or reuses its verified sender.
// Invalid encodings and signatures are marked with ErrTxRejected.
func (app *EVMProxy) DecodeTx(raw []byte) (EvmTx, error) {
	return decodeTx(raw, func(hash common.Hash) utils.Option[common.Address] {
		for senders := range app.checkedSenders.Lock() {
			return senders.peek(hash)
		}
		panic("unreachable")
	})
}

func decodeTx(raw []byte, cached func(common.Hash) utils.Option[common.Address]) (EvmTx, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return EvmTx{}, fmt.Errorf("%w: %w", ErrTxRejected, err)
	}
	sender, ok := cached(tx.Hash()).Get()
	if !ok {
		var signer ethtypes.Signer = ethtypes.HomesteadSigner{}
		if tx.Protected() {
			if tx.ChainId().Sign() <= 0 {
				return EvmTx{}, fmt.Errorf("%w: %w", ErrTxRejected, ethtypes.ErrInvalidChainId)
			}
			signer = ethtypes.LatestSignerForChainID(tx.ChainId())
		}
		var err error
		sender, err = ethtypes.Sender(signer, tx)
		if err != nil {
			return EvmTx{}, fmt.Errorf("%w: %w", ErrTxRejected, err)
		}
	}
	return EvmTx{Transaction: tx, Sender: sender}, nil
}

// DecodeTxs decodes transactions and recovers or reuses their verified senders in order.
// A zero worker count uses GOMAXPROCS.
func (app *EVMProxy) DecodeTxs(raw [][]byte, workers int) ([]EvmTx, error) {
	return decodeTxs(raw, workers, app.DecodeTx)
}

// DecodeTxs decodes Ethereum transactions and recovers their senders in order.
// A zero worker count uses GOMAXPROCS.
func DecodeTxs(raw [][]byte, workers int) ([]EvmTx, error) {
	return decodeTxs(raw, workers, DecodeTx)
}

func decodeTxs(raw [][]byte, workers int, decode func([]byte) (EvmTx, error)) ([]EvmTx, error) {
	txs := make([]EvmTx, len(raw))
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	workers = min(workers, len(raw))
	if workers <= 1 {
		for i, rawTx := range raw {
			tx, err := decode(rawTx)
			if err != nil {
				return nil, fmt.Errorf("transaction %d: %w", i, err)
			}
			txs[i] = tx
		}
		return txs, nil
	}
	if err := scope.Parallel(func(s scope.ParallelScope) error {
		for worker := range workers {
			s.Spawn(func() error {
				for i := worker; i < len(raw); i += workers {
					tx, err := decode(raw[i])
					if err != nil {
						return fmt.Errorf("transaction %d: %w", i, err)
					}
					txs[i] = tx
				}
				return nil
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return txs, nil
}
