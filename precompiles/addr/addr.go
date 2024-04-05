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
)

const (
	GetSeiAddressMethod = "getSeiAddr"
	GetEvmAddressMethod = "getEvmAddr"
)

const (
	AddrAddress = "0x0000000000000000000000000000000000001004"
)

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type Precompile struct {
	pcommon.Precompile
	evmKeeper pcommon.EVMKeeper
	address   common.Address

	GetSeiAddressID []byte
	GetEvmAddressID []byte
}

func NewPrecompile(evmKeeper pcommon.EVMKeeper) (*Precompile, error) {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		return nil, fmt.Errorf("error loading the staking ABI %s", err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		return nil, err
	}

	p := &Precompile{
		Precompile: pcommon.Precompile{ABI: newAbi},
		evmKeeper:  evmKeeper,
		address:    common.HexToAddress(AddrAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case GetSeiAddressMethod:
			p.GetSeiAddressID = m.ID
		case GetEvmAddressMethod:
			p.GetEvmAddressID = m.ID
		}
	}

	return p, nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p Precompile) RequiredGas(input []byte) uint64 {
	methodID, err := pcommon.ExtractMethodID(input)
	if err != nil {
		return 0
	}

	method, err := p.ABI.MethodById(methodID)
	if err != nil {
		// This should never happen since this method is going to fail during Run
		return 0
	}

	return p.Precompile.RequiredGas(input, p.IsTransaction(method.Name))
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) Run(evm *vm.EVM, _ common.Address, input []byte, value *big.Int) (bz []byte, err error) {
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case GetSeiAddressMethod:
		return p.getSeiAddr(ctx, method, args, value)
	case GetEvmAddressMethod:
		return p.getEvmAddr(ctx, method, args, value)
	}
	return
}

func (p Precompile) getSeiAddr(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}

	seiAddrStr := p.evmKeeper.GetSeiAddressOrDefault(ctx, args[0].(common.Address)).String()
	return method.Outputs.Pack(seiAddrStr)
}

func (p Precompile) getEvmAddr(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}

	evmAddr, err := p.evmKeeper.GetEVMAddressFromBech32OrDefault(ctx, args[0].(string))
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(evmAddr)
}

func (Precompile) IsTransaction(string) bool {
	return false
}
