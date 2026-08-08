package common

import (
	"errors"
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
	evmKeeper  putils.EVMKeeper
	err        error
	revertData []byte
}

func (e *retiredExecutor) Execute(ctx sdk.Context, _ *abi.Method, _ common.Address, _ common.Address, _ []interface{}, value *big.Int, _ bool, _ *vm.EVM, _ uint64, _ *tracing.Hooks) ([]byte, uint64, error) {
	if err := ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}
	return e.revertData, GetRemainingGas(ctx, e.evmKeeper), e.err
}

func (e *retiredExecutor) EVMKeeper() putils.EVMKeeper {
	return e.evmKeeper
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
