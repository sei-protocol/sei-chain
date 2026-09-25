package ibc

import (
	"embed"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

const IBCAddress = "0x0000000000000000000000000000000000001009"

var ErrIBCPrecompileRetired = errors.New("ibc precompile is retired; IBC transfers are disabled")

var ibcRetiredRevertData = mustEncodeRevertReason(ErrIBCPrecompileRetired.Error())

//go:embed abi.json
var f embed.FS

type retiredExecutor struct {
	evmKeeper utils.EVMKeeper
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newABI := pcommon.MustGetABI(f, "abi.json")
	executor := &retiredExecutor{evmKeeper: keepers.EVMK()}
	return pcommon.NewDynamicGasPrecompile(newABI, executor, common.HexToAddress(IBCAddress), "ibc"), nil
}

func (e retiredExecutor) Execute(
	ctx sdk.Context,
	method *abi.Method,
	_ common.Address,
	_ common.Address,
	_ []interface{},
	value *big.Int,
	readOnly bool,
	_ *vm.EVM,
	_ uint64,
	_ *tracing.Hooks,
) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, 0, errors.New("cannot call IBC precompile from staticcall")
	}
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, 0, errors.New("cannot delegatecall IBC")
	}
	if method == nil {
		return nil, 0, errors.New("unknown IBC precompile method")
	}
	return ibcRetiredRevertData, pcommon.GetRemainingGas(ctx, e.evmKeeper), ErrIBCPrecompileRetired
}

func (e retiredExecutor) EVMKeeper() utils.EVMKeeper {
	return e.evmKeeper
}

func mustEncodeRevertReason(reason string) []byte {
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
