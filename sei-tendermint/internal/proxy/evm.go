package proxy

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// EVMApplication is an ABCI application that executes EVM transactions and
// serves read-only EVM calls and its execution parameters.
type EVMApplication interface {
	types.Application

	// EvmCall executes msg as a read-only call against the current EVM state,
	// with overrides applied.
	EvmCall(ctx context.Context, msg *core.Message, overrides evmonly.CallOverrides) (*core.ExecutionResult, error)
	// EvmEstimateGas returns the lowest gas limit at or below gasCap that lets
	// msg succeed with overrides applied, and any revert data.
	EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64, overrides evmonly.CallOverrides) (uint64, []byte, error)
	// EvmCallAt executes msg as a read-only call against the committed EVM
	// state after header's block, in the context that block executed at, with
	// overrides applied.
	EvmCallAt(ctx context.Context, msg *core.Message, header *ethtypes.Header, overrides evmonly.CallOverrides) (*core.ExecutionResult, error)
	// EvmEstimateGasAt returns the lowest gas limit at or below gasCap that
	// lets msg succeed against the committed EVM state after header's block,
	// with overrides applied, and any revert data.
	EvmEstimateGasAt(ctx context.Context, msg *core.Message, gasCap uint64, header *ethtypes.Header, overrides evmonly.CallOverrides) (uint64, []byte, error)
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
	EvmStateAt(ctx context.Context, height int64) (vm.StateDB, func(), error)
	// EvmStateBeforeTx returns the EVM state the transaction at index len(txs)
	// of header's block executed against, and the function that releases it.
	EvmStateBeforeTx(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.StateDB, func(), error)
}

// EVMProxy wraps an EVMApplication and records EVM method timings.
type EVMProxy struct {
	app EVMApplication
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
	return app.app.EvmCallAt(ctx, msg, header, overrides)
}

func (app *EVMProxy) EvmEstimateGasAt(ctx context.Context, msg *core.Message, gasCap uint64, header *ethtypes.Header, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_estimate_gas_at", "sync"))()
	return app.app.EvmEstimateGasAt(ctx, msg, gasCap, header, overrides)
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

func (app *EVMProxy) EvmStateAt(ctx context.Context, height int64) (vm.StateDB, func(), error) {
	defer addTimeSample(Global.MethodTimingAt("evm_state_at", "sync"))()
	return app.app.EvmStateAt(ctx, height)
}

func (app *EVMProxy) EvmStateBeforeTx(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.StateDB, func(), error) {
	defer addTimeSample(Global.MethodTimingAt("evm_state_before_tx", "sync"))()
	return app.app.EvmStateBeforeTx(ctx, header, txs)
}
