package proxy

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

// EVMApplication is an ABCI application that executes EVM transactions and
// serves read-only EVM calls and its execution parameters.
type EVMApplication interface {
	types.Application

	// EvmCall executes msg as a read-only call against the current EVM state.
	EvmCall(context.Context, *core.Message) (*core.ExecutionResult, error)
	// EvmEstimateGas returns the lowest gas limit at or below gasCap that lets
	// msg succeed, and any revert data.
	EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error)
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
	// EvmStateAt returns the committed EVM state after height and the function
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

func (app *EVMProxy) EvmCall(ctx context.Context, msg *core.Message) (*core.ExecutionResult, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_call", "sync"))()
	return app.app.EvmCall(ctx, msg)
}

func (app *EVMProxy) EvmEstimateGas(ctx context.Context, msg *core.Message, gasCap uint64) (uint64, []byte, error) {
	defer addTimeSample(Global.MethodTimingAt("evm_estimate_gas", "sync"))()
	return app.app.EvmEstimateGas(ctx, msg, gasCap)
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
