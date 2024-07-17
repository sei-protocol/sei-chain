package addr

import (
	"bytes"
	"embed"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	GetSeiAddressMethod = "getSeiAddr"
	GetEvmAddressMethod = "getEvmAddr"
)

const (
	AddrAddress = "0x0000000000000000000000000000000000001004"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper pcommon.EVMKeeper

	GetSeiAddressID []byte
	GetEvmAddressID []byte
}

func NewPrecompile(evmKeeper pcommon.EVMKeeper) (*pcommon.Precompile, error) {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		return nil, fmt.Errorf("error loading the addr ABI %s", err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		return nil, err
	}

	p := &PrecompileExecutor{
		evmKeeper: evmKeeper,
	}

	for name, m := range newAbi.Methods {
		switch name {
		case GetSeiAddressMethod:
			p.GetSeiAddressID = m.ID
		case GetEvmAddressMethod:
			p.GetEvmAddressID = m.ID
		}
	}

	return pcommon.NewPrecompile(newAbi, p, common.HexToAddress(AddrAddress), "addr"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return pcommon.DefaultGasCost(input, p.IsTransaction(method.Name))
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, _ common.Address, _ common.Address, args []interface{}, value *big.Int, _ bool, _ *vm.EVM) (bz []byte, err error) {
	switch method.Name {
	case GetSeiAddressMethod:
		return p.getSeiAddr(ctx, method, args, value)
	case GetEvmAddressMethod:
		return p.getEvmAddr(ctx, method, args, value)
	}
	return
}

func (p PrecompileExecutor) getSeiAddr(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}

	seiAddr, found := p.evmKeeper.GetSeiAddress(ctx, args[0].(common.Address))
	if !found {
		metrics.IncrementAssociationError("getSeiAddr", types.NewAssociationMissingErr(args[0].(common.Address).Hex()))
		return nil, fmt.Errorf("EVM address %s is not associated", args[0].(common.Address).Hex())
	}
	return method.Outputs.Pack(seiAddr.String())
}

func (p PrecompileExecutor) getEvmAddr(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}

	seiAddr, err := sdk.AccAddressFromBech32(args[0].(string))
	if err != nil {
		return nil, err
	}

	evmAddr, found := p.evmKeeper.GetEVMAddress(ctx, seiAddr)
	if !found {
		metrics.IncrementAssociationError("getEvmAddr", types.NewAssociationMissingErr(args[0].(string)))
		return nil, fmt.Errorf("sei address %s is not associated", args[0].(string))
	}
	return method.Outputs.Pack(evmAddr)
}

func (PrecompileExecutor) IsTransaction(string) bool {
	return false
}
