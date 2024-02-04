package staking

import (
	"bytes"
	"embed"
	"errors"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

const (
	DelegateMethod   = "delegate"
	RedelegateMethod = "redelegate"
	UndelegateMethod = "undelegate"
)

const (
	StakingAddress = "0x0000000000000000000000000000000000001005"
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
	stakingKeeper pcommon.StakingKeeper
	evmKeeper     pcommon.EVMKeeper
	address       common.Address

	DelegateID   []byte
	RedelegateID []byte
	UndelegateID []byte
}

func NewPrecompile(stakingKeeper pcommon.StakingKeeper, evmKeeper pcommon.EVMKeeper) (*Precompile, error) {
	newAbi := GetABI()

	p := &Precompile{
		Precompile:    pcommon.Precompile{ABI: newAbi},
		stakingKeeper: stakingKeeper,
		evmKeeper:     evmKeeper,
		address:       common.HexToAddress(StakingAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case DelegateMethod:
			p.DelegateID = m.ID
		case RedelegateMethod:
			p.RedelegateID = m.ID
		case UndelegateMethod:
			p.UndelegateID = m.ID
		}
	}

	return p, nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p Precompile) RequiredGas(input []byte) uint64 {
	methodID := input[:4]

	if bytes.Equal(methodID, p.DelegateID) {
		return 50000
	} else if bytes.Equal(methodID, p.RedelegateID) {
		return 70000
	} else if bytes.Equal(methodID, p.UndelegateID) {
		return 50000
	}
	panic("unknown method")
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) Run(evm *vm.EVM, caller common.Address, input []byte) (bz []byte, err error) {
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}

	switch method.Name {
	case DelegateMethod:
		return p.delegate(ctx, method, caller, args)
	case RedelegateMethod:
		return p.redelegate(ctx, method, caller, args)
	case UndelegateMethod:
		return p.undelegate(ctx, method, caller, args)
	}
	return
}

func (p Precompile) delegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 2)
	delegator := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
	validatorBech32 := args[0].(string)
	amount := args[1].(*big.Int)
	_, err := p.stakingKeeper.Delegate(sdk.WrapSDKContext(ctx), &stakingtypes.MsgDelegate{
		DelegatorAddress: delegator.String(),
		ValidatorAddress: validatorBech32,
		Amount:           sdk.NewCoin(p.evmKeeper.GetBaseDenom(ctx), sdk.NewIntFromBigInt(amount)),
	})
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) redelegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 3)
	delegator := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
	srcValidatorBech32 := args[0].(string)
	dstValidatorBech32 := args[1].(string)
	amount := args[2].(*big.Int)
	_, err := p.stakingKeeper.BeginRedelegate(sdk.WrapSDKContext(ctx), &stakingtypes.MsgBeginRedelegate{
		DelegatorAddress:    delegator.String(),
		ValidatorSrcAddress: srcValidatorBech32,
		ValidatorDstAddress: dstValidatorBech32,
		Amount:              sdk.NewCoin(p.evmKeeper.GetBaseDenom(ctx), sdk.NewIntFromBigInt(amount)),
	})
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) undelegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 2)
	delegator := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
	validatorBech32 := args[0].(string)
	amount := args[1].(*big.Int)
	_, err := p.stakingKeeper.Undelegate(sdk.WrapSDKContext(ctx), &stakingtypes.MsgUndelegate{
		DelegatorAddress: delegator.String(),
		ValidatorAddress: validatorBech32,
		Amount:           sdk.NewCoin(p.evmKeeper.GetBaseDenom(ctx), sdk.NewIntFromBigInt(amount)),
	})
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) accAddressFromArg(ctx sdk.Context, arg interface{}) (sdk.AccAddress, error) {
	addr := arg.(common.Address)
	if addr == (common.Address{}) {
		return nil, errors.New("invalid addr")
	}
	return p.evmKeeper.GetSeiAddressOrDefault(ctx, addr), nil
}
