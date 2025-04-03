package v520

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common/legacy/v520"
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
	bankKeeper    pcommon.BankKeeper
	address       common.Address

	DelegateID   []byte
	RedelegateID []byte
	UndelegateID []byte
}

func NewPrecompile(stakingKeeper pcommon.StakingKeeper, evmKeeper pcommon.EVMKeeper, bankKeeper pcommon.BankKeeper) (*Precompile, error) {
	newAbi := GetABI()

	p := &Precompile{
		Precompile:    pcommon.Precompile{ABI: newAbi},
		stakingKeeper: stakingKeeper,
		evmKeeper:     evmKeeper,
		bankKeeper:    bankKeeper,
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
	methodID, err := pcommon.ExtractMethodID(input)
	if err != nil {
		return pcommon.UnknownMethodCallGas
	}

	if bytes.Equal(methodID, p.DelegateID) {
		return 50000
	} else if bytes.Equal(methodID, p.RedelegateID) {
		return 70000
	} else if bytes.Equal(methodID, p.UndelegateID) {
		return 50000
	}

	// This should never happen since this is going to fail during Run
	return pcommon.UnknownMethodCallGas
}

func (p Precompile) Address() common.Address {
	return p.address
}

func (p Precompile) GetName() string {
	return "staking"
}

func (p Precompile) Run(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, value *big.Int, readOnly bool, _ bool) (bz []byte, err error) {
	if readOnly {
		return nil, errors.New("cannot call staking precompile from staticcall")
	}
	ctx, method, args, err := p.Prepare(evm, input)
	if err != nil {
		return nil, err
	}
	if caller.Cmp(callingContract) != 0 {
		return nil, errors.New("cannot delegatecall staking")
	}

	switch method.Name {
	case DelegateMethod:
		return p.delegate(ctx, method, caller, args, value)
	case RedelegateMethod:
		return p.redelegate(ctx, method, caller, args, value)
	case UndelegateMethod:
		return p.undelegate(ctx, method, caller, args, value)
	}
	return
}

func (p Precompile) delegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	// if delegator is associated, then it must have Account set already
	// if delegator is not associated, then it can't delegate anyway (since
	// there is no good way to merge delegations if it becomes associated)
	delegator, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, fmt.Errorf("delegator %s is not associated/doesn't have an Account set yet", caller.Hex())
	}
	validatorBech32 := args[0].(string)
	if value == nil || value.Sign() == 0 {
		return nil, errors.New("set `value` field to non-zero to send delegate fund")
	}
	coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), delegator, value, p.bankKeeper)
	if err != nil {
		return nil, err
	}
	_, err = p.stakingKeeper.Delegate(sdk.WrapSDKContext(ctx), &stakingtypes.MsgDelegate{
		DelegatorAddress: delegator.String(),
		ValidatorAddress: validatorBech32,
		Amount:           coin,
	})
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) redelegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 3); err != nil {
		return nil, err
	}
	delegator, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, fmt.Errorf("redelegator %s is not associated/doesn't have an Account set yet", caller.Hex())
	}
	srcValidatorBech32 := args[0].(string)
	dstValidatorBech32 := args[1].(string)
	amount := args[2].(*big.Int)
	_, err := p.stakingKeeper.BeginRedelegate(sdk.WrapSDKContext(ctx), &stakingtypes.MsgBeginRedelegate{
		DelegatorAddress:    delegator.String(),
		ValidatorSrcAddress: srcValidatorBech32,
		ValidatorDstAddress: dstValidatorBech32,
		Amount:              sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewIntFromBigInt(amount)),
	})
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

func (p Precompile) undelegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	delegator, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, fmt.Errorf("undelegator %s is not associated/doesn't have an Account set yet", caller.Hex())
	}
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
