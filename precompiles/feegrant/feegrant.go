package feegrant

import (
	"embed"
	"errors"
	"fmt"
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

const (
	AllowanceMethod           = "allowance"
	AllowancesMethod          = "allowances"
	AllowancesByGranterMethod = "allowancesByGranter"
	FeegrantAddress           = "0x0000000000000000000000000000000000001010"
)

var ErrFeegrantPrecompileRetired = errors.New("feegrant precompile is retired; fee grant queries are disabled")

var feegrantRetiredRevertData = mustEncodeRevertReason(ErrFeegrantPrecompileRetired.Error())

//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper utils.EVMKeeper

	AllowanceID           []byte
	AllowancesID          []byte
	AllowancesByGranterID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newABI := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper: keepers.EVMK(),
	}

	for name, method := range newABI.Methods {
		switch name {
		case AllowanceMethod:
			p.AllowanceID = method.ID
		case AllowancesMethod:
			p.AllowancesID = method.ID
		case AllowancesByGranterMethod:
			p.AllowancesByGranterID = method.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newABI, p, common.HexToAddress(FeegrantAddress), "feegrant"), nil
}

// RequiredGas returns the minimum gas required to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return pcommon.DefaultGasCost(input, p.IsTransaction(method.Name))
}

func (p PrecompileExecutor) Execute(
	ctx sdk.Context,
	method *abi.Method,
	_ common.Address,
	_ common.Address,
	args []interface{},
	value *big.Int,
	_ bool,
	_ *vm.EVM,
	_ uint64,
	_ *tracing.Hooks,
) (bz []byte, remainingGas uint64, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("execution reverted: %v", r)
		}
	}()

	switch method.Name {
	case AllowanceMethod:
		return p.retired(ctx, args, value, 2)
	case AllowancesMethod, AllowancesByGranterMethod:
		return p.retired(ctx, args, value, 2)
	default:
		return nil, 0, nil
	}
}

func (p PrecompileExecutor) retired(ctx sdk.Context, args []interface{}, value *big.Int, expectedArgs int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}
	if err := pcommon.ValidateArgsLength(args, expectedArgs); err != nil {
		return nil, 0, err
	}
	return feegrantRetiredRevertData, pcommon.GetRemainingGas(ctx, p.evmKeeper), ErrFeegrantPrecompileRetired
}

func (p PrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return p.evmKeeper
}

func (PrecompileExecutor) IsTransaction(string) bool {
	return false
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
