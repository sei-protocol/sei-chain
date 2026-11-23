package core

import (
<<<<<<< HEAD
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
=======
	"context"

	ethcore "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
>>>>>>> origin/main
)

// EVMEnvironment combines node RPC services with an EVM application.
type EVMEnvironment struct {
	*Environment
	*abevm.Proxy
}

// EvmCall executes a read-only call through the application proxy.
func (e *EVMEnvironment) EvmCall(ctx context.Context, msg *ethcore.Message, overrides evmonly.CallOverrides) (*ethcore.ExecutionResult, error) {
	return e.EVMProxy.EvmCall(ctx, msg, proxy.CallOverrides(overrides))
}

// EvmCallAt executes a read-only call against header's committed state.
func (e *EVMEnvironment) EvmCallAt(ctx context.Context, msg *ethcore.Message, header *ethtypes.Header, overrides evmonly.CallOverrides) (*ethcore.ExecutionResult, error) {
	return e.EVMProxy.EvmCallAt(ctx, msg, header, proxy.CallOverrides(overrides))
}

// EvmEstimateGas returns the gas estimate for a call through the application proxy.
func (e *EVMEnvironment) EvmEstimateGas(ctx context.Context, msg *ethcore.Message, gasCap uint64, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	return e.EVMProxy.EvmEstimateGas(ctx, msg, gasCap, proxy.CallOverrides(overrides))
}

// EvmEstimateGasAt returns the gas estimate against header's committed state.
func (e *EVMEnvironment) EvmEstimateGasAt(ctx context.Context, msg *ethcore.Message, gasCap uint64, header *ethtypes.Header, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	return e.EVMProxy.EvmEstimateGasAt(ctx, msg, gasCap, header, proxy.CallOverrides(overrides))
}

// EvmEstimateGasAfterCalls returns the gas estimate after applying calls in order.
func (e *EVMEnvironment) EvmEstimateGasAfterCalls(ctx context.Context, calls []*ethcore.Message, msg *ethcore.Message, gasCap uint64, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	return e.EVMProxy.EvmEstimateGasAfterCalls(ctx, calls, msg, gasCap, proxy.CallOverrides(overrides))
}

// EvmEstimateGasAfterCallsAt returns the gas estimate after applying calls to header's committed state.
func (e *EVMEnvironment) EvmEstimateGasAfterCallsAt(ctx context.Context, calls []*ethcore.Message, msg *ethcore.Message, gasCap uint64, header *ethtypes.Header, overrides evmonly.CallOverrides) (uint64, []byte, error) {
	return e.EVMProxy.EvmEstimateGasAfterCallsAt(ctx, calls, msg, gasCap, header, proxy.CallOverrides(overrides))
}
