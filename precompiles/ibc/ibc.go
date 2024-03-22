package ibc

import (
	"bytes"
	"embed"
	"errors"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

const (
	TransferMethod = "transfer"
)

const (
	IBCAddress = "0x0000000000000000000000000000000000001009"
)

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

func GetABI() abi.ABI {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		panic(err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		panic(err)
	}
	return newAbi
}

type Precompile struct {
	pcommon.Precompile
	address        common.Address
	transferKeeper pcommon.TransferKeeper
	evmKeeper      pcommon.EVMKeeper

	TransferID []byte
}

func NewPrecompile(transferKeeper pcommon.TransferKeeper, evmKeeper pcommon.EVMKeeper) (*Precompile, error) {
	newAbi := GetABI()

	p := &Precompile{
		Precompile:     pcommon.Precompile{ABI: newAbi},
		address:        common.HexToAddress(IBCAddress),
		transferKeeper: transferKeeper,
		evmKeeper:      evmKeeper,
	}

	for name, m := range newAbi.Methods {
		switch name {
		case TransferMethod:
			p.TransferID = m.ID
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

func (p Precompile) Run(evm *vm.EVM, caller common.Address, input []byte, value *big.Int) (bz []byte, err error) {
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case TransferMethod:
		return p.transfer(ctx, method, args)
	}
	return
}
func (p Precompile) transfer(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 6)

	senderAddress, err := p.accAddressFromArg(ctx, args[0])
	if err != nil {
		return nil, err
	}

	receiverAddress, err := p.accAddressFromArg(ctx, args[1])
	if err != nil {
		return nil, err
	}

	port, ok := args[2].(string)
	if !ok {
		return nil, errors.New("port is not a string")
	}

	channelID, ok := args[3].(string)
	if !ok {
		return nil, errors.New("channelID is not a string")
	}

	denom := args[4].(string)
	if denom == "" {
		return nil, errors.New("invalid denom")
	}

	amount, ok := args[5].(*big.Int)
	if !ok {
		return nil, errors.New("amount is not a big.Int")
	}

	if amount.Cmp(big.NewInt(0)) == 0 {
		// short circuit
		return method.Outputs.Pack(true)
	}

	coin := sdk.Coin{
		Denom:  denom,
		Amount: sdk.NewIntFromBigInt(amount),
	}

	ctx.BlockHeight()
	height := clienttypes.Height{
		RevisionNumber: 1,
		RevisionHeight: uint64(ctx.BlockHeight() + 1),
	}

	err = p.transferKeeper.SendTransfer(ctx, port, channelID, coin, senderAddress, receiverAddress.String(), height, 0)

	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (Precompile) IsTransaction(method string) bool {
	switch method {
	case TransferMethod:
		return true
	default:
		return false
	}
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, errors.New("invalid addr")
	}
	return p.evmKeeper.GetSeiAddressOrDefault(ctx, addr), nil
}
