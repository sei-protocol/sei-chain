package core

import (
	"context"
	"math/big"

	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
)

// EVMEnvironment is an Environment whose application is an EVMApplication,
// serving the EVM call and execution parameter RPCs.
type EVMEnvironment struct {
	*Environment
	EVM *proxy.EVMProxy
}

// EvmChainID returns the EVM chain ID this node is configured for.
func (env *EVMEnvironment) EvmChainID() uint64 {
	return env.EVM.EvmChainID()
}

// EvmGasLimit returns the gas limit of the most recently committed block.
func (env *EVMEnvironment) EvmGasLimit() (uint64, error) {
	return env.EVM.EvmGasLimit(), nil
}

// EvmMinGasPrice returns the minimum effective gas price the application
// admits a transaction at.
func (env *EVMEnvironment) EvmMinGasPrice() (*big.Int, error) {
	return env.EVM.EvmMinGasPrice(), nil
}

// EvmChainConfig returns the application's EVM chain configuration.
func (env *EVMEnvironment) EvmChainConfig() (*params.ChainConfig, error) {
	return env.EVM.EvmChainConfig(), nil
}

// EvmCall executes msg as a read-only call against the current committed EVM
// state, without creating a transaction or persisting any state change.
func (env *EVMEnvironment) EvmCall(ctx context.Context, msg *ethcore.Message) (*ethcore.ExecutionResult, error) {
	return env.EVM.EvmCall(ctx, msg)
}

// EvmBaseFee returns the base fee the application executes every block at.
func (env *EVMEnvironment) EvmBaseFee() (*big.Int, error) {
	return env.EVM.EvmBaseFee(), nil
}

// EvmEstimateGas returns the lowest gas limit that lets msg succeed against
// the current committed EVM state, persisting no state change.
func (env *EVMEnvironment) EvmEstimateGas(ctx context.Context, msg *ethcore.Message, gasCap uint64) (uint64, []byte, error) {
	return env.EVM.EvmEstimateGas(ctx, msg, gasCap)
}

// EvmBlockContext returns the block context header's block executed at.
func (env *EVMEnvironment) EvmBlockContext(header *ethtypes.Header) (vm.BlockContext, error) {
	return env.EVM.EvmBlockContext(header)
}

// EvmState returns the latest committed EVM state and the function that releases it.
func (env *EVMEnvironment) EvmState(ctx context.Context) (vm.StateDB, func(), error) {
	return env.EVM.EvmState(ctx)
}

// EvmStateAt returns the committed EVM state after height and the function that releases it.
func (env *EVMEnvironment) EvmStateAt(ctx context.Context, height int64) (vm.StateDB, func(), error) {
	return env.EVM.EvmStateAt(ctx, height)
}

// EvmStateBeforeTx returns the EVM state the transaction at index len(txs) of header's block
// executed against, and the function that releases it.
func (env *EVMEnvironment) EvmStateBeforeTx(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.StateDB, func(), error) {
	return env.EVM.EvmStateBeforeTx(ctx, header, txs)
}
