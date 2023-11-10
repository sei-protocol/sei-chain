package wasmd

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

const (
	ExecuteMethod = "execute"
)

const WasmdAddress = "0x0000000000000000000000000000000000001002"

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type Precompile struct {
	pcommon.Precompile
	evmKeeper   pcommon.EVMKeeper
	wasmdKeeper pcommon.WasmdKeeper
	address     common.Address

	ExecuteID []byte
}

func NewPrecompile(evmKeeper pcommon.EVMKeeper, wasmdKeeper pcommon.WasmdKeeper) (*Precompile, error) {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		return nil, fmt.Errorf("error loading the staking ABI %s", err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		return nil, err
	}

	p := &Precompile{
		Precompile:  pcommon.Precompile{ABI: newAbi},
		wasmdKeeper: wasmdKeeper,
		evmKeeper:   evmKeeper,
		address:     common.HexToAddress(WasmdAddress),
	}

	for name, m := range newAbi.Methods {
		if name == "execute" {
			p.ExecuteID = m.ID
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

func (Precompile) IsTransaction(method string) bool {
	switch method {
	case ExecuteMethod:
		return true
	default:
		return false
	}
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) Run(evm *vm.EVM, input []byte) (bz []byte, err error) {
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case ExecuteMethod:
		return p.execute(ctx, method, args)
	}
	return
}

func (p Precompile) execute(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if len(args) != 4 {
		return nil, errors.New("execute requires exactly 4 arguments")
	}
	contractAddrStr, ok := args[0].(string)
	if !ok {
		return nil, errors.New("invalid contract address; must be string")
	}
	// addresses will be sent in Sei format
	contractAddr, err := sdk.AccAddressFromBech32(contractAddrStr)
	if err != nil {
		return nil, err
	}
	senderAddrStr, ok := args[1].(string)
	if !ok {
		return nil, errors.New("invalid sender address; must be string")
	}
	senderAddr, err := sdk.AccAddressFromBech32(senderAddrStr)
	if err != nil {
		return nil, err
	}
	msg, ok := args[2].([]byte)
	if !ok {
		return nil, errors.New("invalid message; must be []byte")
	}
	coins := sdk.NewCoins()
	coinsBz, ok := args[3].([]byte)
	if !ok {
		return nil, errors.New("invalid coins: must be []byte")
	}
	if err := json.Unmarshal(coinsBz, &coins); err != nil {
		return nil, err
	}
	return p.wasmdKeeper.Execute(ctx, contractAddr, senderAddr, msg, coins)
}
