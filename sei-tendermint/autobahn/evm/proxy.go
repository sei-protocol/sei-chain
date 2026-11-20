package evm

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

	proxymetrics "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ErrTxRejected identifies a transaction rejected during decoding or admission.
var ErrTxRejected = errors.New("transaction rejected")

// checkedSendersCap bounds the senders remembered per generation.
// Entries are dropped as their transactions execute; the cap only guards against
// remembered transactions that never reach a block.
const checkedSendersCap = 1 << 22

// minTxsPerHashWorker is the transaction count budgeted per hashing worker.
const minTxsPerHashWorker = 64

// Proxy records application method timings and adapts EVM RPC inputs and results.
type Proxy struct {
	app            Application
	checkedSenders utils.Mutex[*cache[common.Hash, common.Address]]
}

// NewProxy wraps an EVM application and records its method timings.
func NewProxy(app Application) *Proxy {
	return &Proxy{app: app, checkedSenders: utils.NewMutex(new(newCache[common.Hash, common.Address](checkedSendersCap)))}
}

func (app *Proxy) Info() *ResponseInfo {
	defer proxymetrics.Observe("info", "sync")()
	return app.app.Info()
}

func (app *Proxy) GetValidators() CommitteeWeights {
	defer proxymetrics.Observe("get_validators", "sync")()
	return app.app.GetValidators()
}

func (app *Proxy) LastBlockHeight() int64 {
	defer proxymetrics.Observe("last_block_height", "sync")()
	return app.app.LastBlockHeight()
}

func (app *Proxy) InitChain(req InitConfig) error {
	defer proxymetrics.Observe("init_chain", "sync")()
	return app.app.InitChain(req)
}

func (app *Proxy) RestoreBlockTime(blockTime time.Time) {
	defer proxymetrics.Observe("init_last_header", "sync")()
	app.app.RestoreBlockTime(blockTime)
}

// CheckTx checks a transaction with a verified sender for admission and marks rejection errors with ErrTxRejected.
func (app *Proxy) CheckTx(tx EvmTx) (AdmittedTx, error) {
	defer proxymetrics.Observe("check_tx", "sync")()
	res, err := app.app.CheckTx(tx)
	if err != nil {
		return AdmittedTx{}, fmt.Errorf("%w: %w", ErrTxRejected, err)
	}
	app.RememberEvmSender(tx.Hash(), tx.Sender())
	return res, nil
}

// RememberEvmSender records the sender recovered for the transaction with hash.
func (app *Proxy) RememberEvmSender(hash common.Hash, sender common.Address) {
	for senders := range app.checkedSenders.Lock() {
		senders.push(hash, sender)
	}
}

// ForgetEvmSender drops the sender recorded for hash, if any.
func (app *Proxy) ForgetEvmSender(hash common.Hash) {
	for senders := range app.checkedSenders.Lock() {
		senders.pop(hash)
	}
}

func (app *Proxy) EvmNonce(addr common.Address) uint64 {
	defer proxymetrics.Observe("evm_nonce", "sync")()
	return app.app.EvmNonce(addr)
}

// EvmCommittedNonce returns addr's nonce in the wrapped application's committed EVM state without
// waiting for a commit in flight.
func (app *Proxy) EvmCommittedNonce(addr common.Address) uint64 {
	defer proxymetrics.Observe("evm_committed_nonce", "sync")()
	return app.app.EvmCommittedNonce(addr)
}

func (app *Proxy) FinalizeBlock(ctx context.Context, req FinalizeBlockReq) (FinalizeBlockResp, error) {
	defer proxymetrics.Observe("finalize_block", "sync")()
	for senders := range app.checkedSenders.Lock() {
		for _, tx := range req.Txs {
			senders.pop(tx.Hash())
		}
	}
	return app.app.FinalizeBlock(ctx, req)
}

func (app *Proxy) EvmCall(ctx context.Context, msg *core.Message, overrides CallOverrides) (*core.ExecutionResult, error) {
	defer proxymetrics.Observe("evm_call", "sync")()
	return app.app.EvmCall(ctx, msg, overrides)
}

func (app *Proxy) EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64, overrides CallOverrides) (uint64, []byte, error) {
	defer proxymetrics.Observe("evm_estimate_gas", "sync")()
	return app.app.EvmEstimateGas(ctx, msg, gasCap, overrides)
}

func (app *Proxy) EvmCallAt(ctx context.Context, msg *core.Message, header *ethtypes.Header, overrides CallOverrides) (*core.ExecutionResult, error) {
	defer proxymetrics.Observe("evm_call_at", "sync")()
	block, err := executionBlockRef(header)
	if err != nil {
		return nil, err
	}
	return app.app.EvmCallAt(ctx, msg, block, overrides)
}

func (app *Proxy) EvmEstimateGasAt(ctx context.Context, msg *core.Message, gasCap uint64, header *ethtypes.Header, overrides CallOverrides) (uint64, []byte, error) {
	defer proxymetrics.Observe("evm_estimate_gas_at", "sync")()
	block, err := executionBlockRef(header)
	if err != nil {
		return 0, nil, err
	}
	return app.app.EvmEstimateGasAt(ctx, msg, gasCap, block, overrides)
}

func (app *Proxy) EvmEstimateGasAfterCalls(ctx context.Context, calls []*core.Message, msg *core.Message, gasCap uint64, overrides CallOverrides) (uint64, []byte, error) {
	defer proxymetrics.Observe("evm_estimate_gas_after_calls", "sync")()
	return app.app.EvmEstimateGasAfterCalls(ctx, calls, msg, gasCap, overrides)
}

func (app *Proxy) EvmEstimateGasAfterCallsAt(ctx context.Context, calls []*core.Message, msg *core.Message, gasCap uint64, header *ethtypes.Header, overrides CallOverrides) (uint64, []byte, error) {
	defer proxymetrics.Observe("evm_estimate_gas_after_calls_at", "sync")()
	block, err := executionBlockRef(header)
	if err != nil {
		return 0, nil, err
	}
	return app.app.EvmEstimateGasAfterCallsAt(ctx, calls, msg, gasCap, block, overrides)
}

func (app *Proxy) EvmCreateAccessList(ctx context.Context, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error) {
	defer proxymetrics.Observe("evm_create_access_list", "sync")()
	return app.app.EvmCreateAccessList(ctx, msg)
}

func (app *Proxy) EvmCreateAccessListAt(ctx context.Context, msg *core.Message, header *ethtypes.Header) (ethtypes.AccessList, *core.ExecutionResult, error) {
	defer proxymetrics.Observe("evm_create_access_list_at", "sync")()
	block, err := executionBlockRef(header)
	if err != nil {
		return nil, nil, err
	}
	return app.app.EvmCreateAccessListAt(ctx, msg, block)
}

func (app *Proxy) EvmChainID() uint64 {
	defer proxymetrics.Observe("evm_chain_id", "sync")()
	return app.app.EvmChainID()
}

func (app *Proxy) EvmChainConfig() *params.ChainConfig {
	defer proxymetrics.Observe("evm_chain_config", "sync")()
	return app.app.EvmChainConfig()
}

func (app *Proxy) EvmBaseFee() *big.Int {
	defer proxymetrics.Observe("evm_base_fee", "sync")()
	return app.app.EvmBaseFee()
}

func (app *Proxy) EvmGasLimit() uint64 {
	defer proxymetrics.Observe("evm_gas_limit", "sync")()
	return app.app.EvmGasLimit()
}

func (app *Proxy) EvmMinGasPrice() *big.Int {
	defer proxymetrics.Observe("evm_min_gas_price", "sync")()
	return app.app.EvmMinGasPrice()
}

func (app *Proxy) EvmBlockContext(header *ethtypes.Header) (vm.BlockContext, error) {
	defer proxymetrics.Observe("evm_block_context", "sync")()
	block, err := executionBlockRef(header)
	if err != nil {
		return vm.BlockContext{}, err
	}
	return app.app.EvmBlockContext(block)
}

func (app *Proxy) EvmStateAt(ctx context.Context, height int64) (vm.SeiStateDB, func(), error) {
	defer proxymetrics.Observe("evm_state_at", "sync")()
	state, err := app.app.EvmStateAt(ctx, height)
	if err != nil {
		return nil, nil, err
	}
	return state, state.Close, nil
}

func (app *Proxy) EvmStateBeforeTx(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.SeiStateDB, func(), error) {
	defer proxymetrics.Observe("evm_state_before_tx", "sync")()
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

func (app *Proxy) AwaitCommits() error {
	defer proxymetrics.Observe("await_commits", "sync")()
	return app.app.AwaitCommits()
}

func (app *Proxy) EvmBalance(addr common.Address) uint256.Int {
	defer proxymetrics.Observe("evm_balance", "sync")()
	return app.app.EvmBalance(addr)
}

func (app *Proxy) EvmCode(addr common.Address) ([]byte, error) {
	defer proxymetrics.Observe("evm_code", "sync")()
	return app.app.EvmCode(addr), nil
}

func (app *Proxy) EvmStorage(addr common.Address, key common.Hash) (common.Hash, error) {
	defer proxymetrics.Observe("evm_storage", "sync")()
	return app.app.EvmStorage(addr, key), nil
}

func (app *Proxy) EvmBalanceAt(addr common.Address, height int64) (uint256.Int, error) {
	defer proxymetrics.Observe("evm_balance_at", "sync")()
	return app.app.EvmBalanceAt(addr, height)
}

func (app *Proxy) EvmCodeAt(addr common.Address, height int64) ([]byte, error) {
	defer proxymetrics.Observe("evm_code_at", "sync")()
	return app.app.EvmCodeAt(addr, height)
}

func (app *Proxy) EvmNonceAt(addr common.Address, height int64) (uint64, error) {
	defer proxymetrics.Observe("evm_nonce_at", "sync")()
	return app.app.EvmNonceAt(addr, height)
}

func (app *Proxy) EvmStorageAt(addr common.Address, key common.Hash, height int64) (common.Hash, error) {
	defer proxymetrics.Observe("evm_storage_at", "sync")()
	return app.app.EvmStorageAt(addr, key, height)
}

// executionBlockRef validates the header fields consumed by EVM execution.
func executionBlockRef(header *ethtypes.Header) (BlockRef, error) {
	if header.Number == nil || !header.Number.IsInt64() || header.Number.Sign() < 0 {
		return BlockRef{}, fmt.Errorf("invalid EVM block number: %v", header.Number)
	}
	return BlockRef{Height: types.RoadIndex(header.Number.Uint64()), Time: header.Time, ParentHash: header.ParentHash}, nil
}

// ParseTx parses a replay-protected transaction for chainID and recovers or reuses its verified sender.
// chainID must be positive. Rejections are marked with ErrTxRejected.
func (app *Proxy) ParseTx(chainID uint64, raw []byte) (EvmTx, error) {
	signer := ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID))
	tx, err := decodeEVMTransaction(signer, raw)
	if err != nil {
		return EvmTx{}, err
	}
	hash := tx.Hash()
	for senders := range app.checkedSenders.Lock() {
		if sender, ok := senders.get(hash).Get(); ok {
			return EvmTx{tx: tx, sender: sender}, nil
		}
	}
	sender, err := recoverTxSender(signer, tx)
	if err != nil {
		return EvmTx{}, err
	}
	return EvmTx{tx: tx, sender: sender}, nil
}

// ParseTxs parses transactions for chainID and recovers or reuses their verified senders in order.
// chainID must be positive. A zero worker count uses GOMAXPROCS.
func (app *Proxy) ParseTxs(chainID uint64, raw [][]byte, workers int) ([]EvmTx, error) {
	if len(raw) == 0 {
		return []EvmTx{}, nil
	}
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	signer := ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID))
	hashWorkers := min(workers, (len(raw)+minTxsPerHashWorker-1)/minTxsPerHashWorker)
	decoded, err := DecodeBlockTxs(raw, hashWorkers, func(tx *ethtypes.Transaction) error {
		return signerAdmits(signer, tx)
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTxRejected, err)
	}
	txs, missing, missingCount := app.cachedTransactions(decoded)
	if missingCount == 0 {
		return txs, nil
	}
	// Signature recovery remains parallel even for small batches of cache misses.
	if err := forEach(len(decoded), min(workers, missingCount), func(i int) error {
		if !missing[i] {
			return nil
		}
		sender, err := recoverTxSender(signer, decoded[i])
		if err != nil {
			return err
		}
		txs[i] = EvmTx{tx: decoded[i], sender: sender}
		return nil
	}); err != nil {
		return nil, err
	}
	return txs, nil
}

// cachedTransactions returns transactions with cached senders, a cache-miss mask, and its count.
func (app *Proxy) cachedTransactions(decoded []*ethtypes.Transaction) ([]EvmTx, []bool, int) {
	txs := make([]EvmTx, len(decoded))
	missing := make([]bool, len(decoded))
	missingCount := 0
	// Hashes are already cached; decoding, hashing, allocation, and recovery stay outside the lock.
	for senders := range app.checkedSenders.Lock() {
		for i, tx := range decoded {
			if sender, ok := senders.get(tx.Hash()).Get(); ok {
				txs[i] = EvmTx{tx: tx, sender: sender}
			} else {
				missing[i] = true
				missingCount++
			}
		}
	}
	return txs, missing, missingCount
}

// decodeEVMTransaction decodes a replay-protected Ethereum transaction admitted by signer.
func decodeEVMTransaction(signer ethtypes.Signer, raw []byte) (*ethtypes.Transaction, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTxRejected, err)
	}
	if err := signerAdmits(signer, tx); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTxRejected, err)
	}
	return tx, nil
}

func recoverTxSender(signer ethtypes.Signer, tx *ethtypes.Transaction) (common.Address, error) {
	sender, err := ethtypes.Sender(signer, tx)
	if err != nil {
		return common.Address{}, fmt.Errorf("%w: %w", ErrTxRejected, err)
	}
	return sender, nil
}
