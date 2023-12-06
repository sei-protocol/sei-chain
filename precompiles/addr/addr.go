package addr

import (
	"bytes"
	"embed"
	"fmt"

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
	methodID := input[:4]

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

func (p Precompile) Run(evm *vm.EVM, _ common.Address, input []byte) (bz []byte, err error) {
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case GetSeiAddressMethod:
		return p.getSeiAddr(ctx, method, args)
	case GetEvmAddressMethod:
		return p.getEvmAddr(ctx, method, args)
	}
	return
}

func (p Precompile) getSeiAddr(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 1)

	evmAddr := args[0].(common.Address)
	seiAddrStr := sdk.AccAddress(evmAddr[:]).String()
	associatedAddr, found := p.evmKeeper.GetSeiAddress(ctx, evmAddr)
	if found {
		seiAddrStr = associatedAddr.String()
	}
	return method.Outputs.Pack(seiAddrStr)
}

func (p Precompile) getEvmAddr(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 1)

	seiAddrStr := args[0].(string)
	seiAddr, err := sdk.AccAddressFromBech32(seiAddrStr)
	if err != nil {
		return nil, err
	}
	evmAddr := common.BytesToAddress(seiAddr)
	associatedAddr, found := p.evmKeeper.GetEVMAddress(ctx, seiAddr)
	if found {
		evmAddr = associatedAddr
	}
	return method.Outputs.Pack(evmAddr)
}

func (Precompile) IsTransaction(string) bool {
	return false
}
