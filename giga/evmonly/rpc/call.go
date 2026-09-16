package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/export"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// defaultCallGasCap bounds the gas an eth_call may consume, filling in an
// omitted gas limit and capping a caller-supplied one. It matches evmrpc's
// simulation_gas_limit default.
const defaultCallGasCap = 10_000_000

type callAPI struct {
	backend Backend
}

// Call executes args as a read-only EVM message call against the current
// committed state and returns the return data. It creates no transaction and
// persists no state change.
func (api *callAPI) Call(ctx context.Context, args export.TransactionArgs, block ethrpc.BlockNumberOrHash) (hexutil.Bytes, error) {
	if err := requireCurrentState(block); err != nil {
		return nil, err
	}
	baseFee := evmtypes.DefaultMinFeePerGas.TruncateInt().BigInt()
	chainID := new(big.Int).SetUint64(api.backend.EvmChainID())
	if err := args.CallDefaults(defaultCallGasCap, baseFee, chainID); err != nil {
		return nil, err
	}
	msg := args.ToMessage(baseFee, true, true)

	result, err := api.backend.EvmCall(ctx, msg)
	if err != nil {
		return nil, err
	}
	if len(result.Revert()) > 0 {
		return nil, newRevertError(result)
	}
	if result.Err != nil {
		return nil, result.Err
	}
	return result.Return(), nil
}

// newRevertError builds the JSON-RPC error eth_call returns for a reverted
// call, matching evmrpc's SimulationAPI.Call error shape: code 3 with the raw
// revert data, and the ABI-decoded reason in the message when possible.
func newRevertError(result *core.ExecutionResult) *revertError {
	reason, errUnpack := abi.UnpackRevert(result.Revert())
	err := errors.New("execution reverted")
	if errUnpack == nil {
		err = fmt.Errorf("execution reverted: %v", reason)
	}
	return &revertError{error: err, reason: hexutil.Encode(result.Revert())}
}

// revertError is a JSON-RPC error carrying an EVM revert reason.
type revertError struct {
	error
	reason string // revert reason, hex encoded
}

// ErrorCode returns the JSON-RPC error code for a revert.
// See: https://github.com/ethereum/wiki/wiki/JSON-RPC-Error-Codes-Improvement-Proposal
func (e *revertError) ErrorCode() int {
	return 3
}

// ErrorData returns the hex encoded revert reason.
func (e *revertError) ErrorData() any {
	return e.reason
}
