package ibc

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"math/big"

	"github.com/cosmos/cosmos-sdk/types/bech32"

	"github.com/sei-protocol/sei-chain/utils"

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

func (p Precompile) RunAndCalculateGas(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, suppliedGas uint64, value *big.Int) (ret []byte, remainingGas uint64, err error) {
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, 0, err
	}
	if err = pcommon.ValidateCaller(ctx, p.evmKeeper, caller, callingContract); err != nil {
		return nil, 0, err
	}

	gasMultiplier := p.evmKeeper.GetPriorityNormalizer(ctx)
	gasLimitBigInt := new(big.Int).Mul(new(big.Int).SetUint64(suppliedGas), gasMultiplier.RoundInt().BigInt())
	if gasLimitBigInt.Cmp(utils.BigMaxU64) > 0 {
		gasLimitBigInt = utils.BigMaxU64
	}
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(gasLimitBigInt.Uint64()))

	switch method.Name {
	case TransferMethod:
		return p.transfer(ctx, method, args, caller)
	}
	return
}

func (p Precompile) Run(evm *vm.EVM, caller common.Address, input []byte, value *big.Int) (bz []byte, err error) {
	panic("static gas Run is not implemented for dynamic gas precompile")
}

func (p Precompile) transfer(ctx sdk.Context, method *abi.Method, args []interface{}, caller common.Address) (ret []byte, remainingGas uint64, rerr error) {
	defer func() {
		if err := recover(); err != nil {
			ret = nil
			remainingGas = 0
			rerr = fmt.Errorf("%s", err)
			return
		}
	}()
	pcommon.AssertArgsLength(args, 8)
	senderSeiAddr, ok := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !ok {
		rerr = errors.New("caller is not a valid SEI address")
		return
	}

	receiverAddressString, ok := args[0].(string)
	if !ok {
		rerr = errors.New("receiverAddress is not a string")
		return
	}
	_, bz, err := bech32.DecodeAndConvert(receiverAddressString)
	if err != nil {
		rerr = err
		return
	}
	err = sdk.VerifyAddressFormat(bz)
	if err != nil {
		rerr = err
		return
	}

	port, ok := args[1].(string)
	if !ok {
		rerr = errors.New("port is not a string")
		return
	}
	if port == "" {
		rerr = errors.New("port cannot be empty")
		return
	}

	channelID, ok := args[2].(string)
	if !ok {
		rerr = errors.New("channelID is not a string")
		return
	}
	if channelID == "" {
		rerr = errors.New("channelID cannot be empty")
		return
	}

	denom := args[3].(string)
	if denom == "" {
		rerr = errors.New("invalid denom")
		return
	}

	amount, ok := args[4].(*big.Int)
	if !ok {
		rerr = errors.New("amount is not a big.Int")
		return
	}

	if amount.Cmp(big.NewInt(0)) == 0 {
		// short circuit
		remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
		ret, rerr = method.Outputs.Pack(true)
		return
	}

	coin := sdk.Coin{
		Denom:  denom,
		Amount: sdk.NewIntFromBigInt(amount),
	}

	revisionNumber, ok := args[5].(uint64)
	if !ok {
		rerr = errors.New("revisionNumber is not a uint64")
		return
	}

	revisionHeight, ok := args[6].(uint64)
	if !ok {
		rerr = errors.New("revisionHeight is not a uint64")
		return
	}

	height := clienttypes.Height{
		RevisionNumber: revisionNumber,
		RevisionHeight: revisionHeight,
	}

	timeoutTimestamp, ok := args[7].(uint64)
	if !ok {
		rerr = errors.New("timeoutTimestamp is not a uint64")
		return
	}

	err = p.transferKeeper.SendTransfer(ctx, port, channelID, coin, senderSeiAddr, receiverAddressString, height, timeoutTimestamp)

	if err != nil {
		rerr = err
		return
	}
	remainingGas = pcommon.GetRemainingGas(ctx, p.evmKeeper)
	ret, rerr = method.Outputs.Pack(true)
	return
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
