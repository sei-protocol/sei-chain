package params

import (
	"embed"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
)

const (
	ParamsMethod = "params"
)

const (
	ParamsAddress = "0x0000000000000000000000000000000000001013"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper     utils.EVMKeeper
	paramsQuerier utils.ParamsQuerier

	ParamsID []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:     keepers.EVMK(),
		paramsQuerier: keepers.ParamsQ(),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case ParamsMethod:
			p.ParamsID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, common.HexToAddress(ParamsAddress), "params"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return pcommon.DefaultGasCost(input, p.IsTransaction(method.Name))
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, hooks *tracing.Hooks) (bz []byte, remainingGas uint64, err error) {
	// Needed to catch gas meter panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("execution reverted: %v", r)
		}
	}()
	switch method.Name {
	case ParamsMethod:
		return p.params(ctx, method, args, value)
	}
	return
}

func (p PrecompileExecutor) params(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	subspace := args[0].(string)
	key := args[1].(string)

	request := &proposal.QueryParamsRequest{
		Subspace: subspace,
		Key:      key,
	}
	response, err := p.paramsQuerier.Params(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	bz, err := method.Outputs.Pack(response.Param.Value)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return p.evmKeeper
}

func (PrecompileExecutor) IsTransaction(string) bool {
	return false
}
