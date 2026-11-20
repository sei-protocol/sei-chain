package evm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	proxymetrics "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// ErrTxRejected identifies a transaction rejected during decoding or admission.
var ErrTxRejected = errors.New("transaction rejected")

// Proxy records application method timings and adapts EVM RPC inputs and results.
type Proxy struct{ app Application }

// NewProxy wraps an EVM application and records its method timings.
func NewProxy(app Application) *Proxy { return &Proxy{app: app} }

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
	return res, nil
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
	return BlockRef{Height: types.RoadHeight(header.Number.Uint64()), Time: header.Time, ParentHash: header.ParentHash}, nil
}
