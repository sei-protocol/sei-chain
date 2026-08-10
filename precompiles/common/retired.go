package common

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	putils "github.com/sei-protocol/sei-chain/precompiles/utils"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// NewRetiredPrecompile keeps a precompile address registered while making every
// valid method call revert with reason. Unregistering an address would instead
// make calls succeed with empty output, which can appear successful to callers.
func NewRetiredPrecompile(a abi.ABI, address common.Address, name string, evmKeeper putils.EVMKeeper, reason string) *DynamicGasPrecompile {
	return NewDynamicGasPrecompile(a, &retiredExecutor{
		evmKeeper:  evmKeeper,
		err:        errors.New(reason),
		revertData: encodeRevertReason(reason),
	}, address, name)
}

type retiredExecutor struct {
	evmKeeper                    putils.EVMKeeper
	err                          error
	revertData                   []byte
	historicalTraceUnsupportedAt func(sdk.Context) bool
	name                         string
}

func (e *retiredExecutor) Execute(ctx sdk.Context, _ *abi.Method, _ common.Address, _ common.Address, _ []interface{}, value *big.Int, _ bool, _ *vm.EVM, _ uint64, _ *tracing.Hooks) ([]byte, uint64, error) {
	if e.historicalTraceUnsupportedAt != nil && e.historicalTraceUnsupportedAt(ctx) {
		recordHistoricalTraceError(ctx, &HistoricalTraceUnavailableError{Precompile: e.name})
	}
	if err := ValidateNonPayable(value); err != nil {
		return common.CopyBytes(e.revertData), 0, err
	}
	return common.CopyBytes(e.revertData), GetRemainingGas(ctx, e.evmKeeper), e.err
}

func (e *retiredExecutor) EVMKeeper() putils.EVMKeeper {
	return e.evmKeeper
}

// NewRetiredPrecompileWithTraceGuard builds a retired precompile whose valid
// calls mark selected trace contexts as unsupported. The EVM still receives a
// normal revert internally; the RPC replay boundary turns the marker into a
// hard error.
func NewRetiredPrecompileWithTraceGuard(
	a abi.ABI,
	address common.Address,
	name string,
	evmKeeper putils.EVMKeeper,
	reason string,
	historicalTraceUnsupportedAt func(sdk.Context) bool,
) *DynamicGasPrecompile {
	p := NewRetiredPrecompile(a, address, name, evmKeeper, reason)
	executor := p.GetExecutor().(*retiredExecutor)
	executor.historicalTraceUnsupportedAt = historicalTraceUnsupportedAt
	executor.name = name
	return p
}

// HistoricalTraceUnavailableError means replay reached precompile code that is
// no longer available and must not return a plausible but incorrect trace.
type HistoricalTraceUnavailableError struct {
	Precompile string
}

func (e *HistoricalTraceUnavailableError) Error() string {
	return fmt.Sprintf("historical trace unavailable: %s precompile implementation has been retired", e.Precompile)
}

type HistoricalTraceErrorRecorder interface {
	RecordHistoricalTraceError(error)
}

type historicalTraceErrorRecorderKey struct{}

// WithHistoricalTraceErrorRecorder installs the trace-only error side channel
// used to carry incompatibility beyond the EVM's normal revert boundary.
func WithHistoricalTraceErrorRecorder(ctx sdk.Context, recorder HistoricalTraceErrorRecorder) sdk.Context {
	return ctx.WithContext(context.WithValue(ctx.Context(), historicalTraceErrorRecorderKey{}, recorder))
}

func recordHistoricalTraceError(ctx sdk.Context, err error) {
	recorder, ok := ctx.Context().Value(historicalTraceErrorRecorderKey{}).(HistoricalTraceErrorRecorder)
	if ok {
		recorder.RecordHistoricalTraceError(err)
	}
}

func encodeRevertReason(reason string) []byte {
	stringType, err := abi.NewType("string", "", nil)
	if err != nil {
		panic(err)
	}
	reasonData, err := abi.Arguments{{Type: stringType}}.Pack(reason)
	if err != nil {
		panic(err)
	}
	return append(crypto.Keccak256([]byte("Error(string)"))[:4], reasonData...)
}
