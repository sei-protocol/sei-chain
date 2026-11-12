package core

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
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
func (env *EVMEnvironment) EvmCall(ctx context.Context, msg *ethcore.Message, overrides evmonly.CallOverrides) (*ethcore.ExecutionResult, error) {
	return env.EVM.EvmCall(ctx, msg, overrides)
}

// EvmBaseFee returns the base fee the application executes every block at.
func (env *EVMEnvironment) EvmBaseFee() (*big.Int, error) {
	return env.EVM.EvmBaseFee(), nil
}

// EvmEstimateGas returns the lowest gas limit that lets msg succeed against
// the current committed EVM state, persisting no state change.
func (env *EVMEnvironment) EvmEstimateGas(ctx context.Context, msg *ethcore.Message, gasCap uint64, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	return env.EVM.EvmEstimateGas(ctx, msg, gasCap, overrides)
}

// EvmCallAt executes msg as a read-only call against the committed EVM state
// after header's block, in the context that block executed at.
func (env *EVMEnvironment) EvmCallAt(ctx context.Context, msg *ethcore.Message, header *ethtypes.Header, overrides evmonly.CallOverrides) (*ethcore.ExecutionResult, error) {
	return env.EVM.EvmCallAt(ctx, msg, header, overrides)
}

// EvmEstimateGasAt returns the lowest gas limit that lets msg succeed against
// the committed EVM state after header's block, persisting no state change.
func (env *EVMEnvironment) EvmEstimateGasAt(ctx context.Context, msg *ethcore.Message, gasCap uint64, header *ethtypes.Header, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	return env.EVM.EvmEstimateGasAt(ctx, msg, gasCap, header, overrides)
}

// EvmEstimateGasAfterCalls returns what EvmEstimateGas returns for msg with calls applied to the
// current committed EVM state first, in order, persisting no state change.
func (env *EVMEnvironment) EvmEstimateGasAfterCalls(ctx context.Context, calls []*ethcore.Message, msg *ethcore.Message, gasCap uint64) (uint64, []byte, error) {
	return env.EVM.EvmEstimateGasAfterCalls(ctx, calls, msg, gasCap)
}

// EvmCreateAccessList returns the access list msg touches against the current committed EVM
// state, and the result of executing msg with that list, persisting no state change.
func (env *EVMEnvironment) EvmCreateAccessList(ctx context.Context, msg *ethcore.Message) (ethtypes.AccessList, *ethcore.ExecutionResult, error) {
	return env.EVM.EvmCreateAccessList(ctx, msg)
}

// EvmBlockContext returns the block context header's block executed at.
func (env *EVMEnvironment) EvmBlockContext(header *ethtypes.Header) (vm.BlockContext, error) {
	return env.EVM.EvmBlockContext(header)
}

// EvmStateAt returns the committed EVM state after height, or the latest committed state when height
// is rpc.LatestBlockNumber, and the function that releases it.
func (env *EVMEnvironment) EvmStateAt(ctx context.Context, height int64) (vm.SeiStateDB, func(), error) {
	return env.EVM.EvmStateAt(ctx, height)
}

// EvmStateBeforeTx returns the EVM state the transaction at index len(txs) of header's block
// executed against, and the function that releases it.
func (env *EVMEnvironment) EvmStateBeforeTx(ctx context.Context, header *ethtypes.Header, txs [][]byte) (vm.SeiStateDB, func(), error) {
	return env.EVM.EvmStateBeforeTx(ctx, header, txs)
}

// RememberEvmSender records the sender recovered for the transaction with this hash in the
// application, which then skips recovering it again.
func (env *EVMEnvironment) RememberEvmSender(hash common.Hash, sender common.Address) {
	env.EVM.RememberEvmSender(hash, sender)
}

// ForgetEvmSender drops the sender recorded for the transaction with this hash, if any.
func (env *EVMEnvironment) ForgetEvmSender(hash common.Hash) {
	env.EVM.ForgetEvmSender(hash)
}

// EvmTransactionCountAt returns the address transaction count (nonce) in the
// committed EVM state after height.
func (env *EVMEnvironment) EvmTransactionCountAt(address common.Address, height int64) (uint64, error) {
	return env.EVM.EvmNonceAt(address, height)
}

// EvmBalanceAt returns the address balance in the committed EVM state after height.
func (env *EVMEnvironment) EvmBalanceAt(address common.Address, height int64) (uint256.Int, error) {
	return env.EVM.EvmBalanceAt(address, height)
}

// EvmCodeAt returns the contract code at address in the committed EVM state after height.
func (env *EVMEnvironment) EvmCodeAt(address common.Address, height int64) ([]byte, error) {
	return env.EVM.EvmCodeAt(address, height)
}

// EvmStorageAt returns the value at key in address's storage in the committed EVM state after height.
func (env *EVMEnvironment) EvmStorageAt(address common.Address, key common.Hash, height int64) (common.Hash, error) {
	return env.EVM.EvmStorageAt(address, key, height)
}

// EvmCode returns the contract code at address in the current committed EVM
// state.
func (env *EVMEnvironment) EvmCode(address common.Address) ([]byte, error) {
	return env.EVM.EvmCode(address)
}

// EvmStorage returns the value at key in address's storage in the current
// committed EVM state.
func (env *EVMEnvironment) EvmStorage(address common.Address, key common.Hash) (common.Hash, error) {
	return env.EVM.EvmStorage(address, key)
}
