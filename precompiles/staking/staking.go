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
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	DelegateMethod   = "delegate"
	RedelegateMethod = "redelegate"
	UndelegateMethod = "undelegate"
	DelegationMethod = "delegation"
)

const (
	StakingAddress = "0x0000000000000000000000000000000000001005"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	stakingKeeper  pcommon.StakingKeeper
	stakingQuerier pcommon.StakingQuerier
	evmKeeper      pcommon.EVMKeeper
	bankKeeper     pcommon.BankKeeper
	address        common.Address

	DelegateID   []byte
	RedelegateID []byte
	UndelegateID []byte
	DelegationID []byte
}

func NewPrecompile(stakingKeeper pcommon.StakingKeeper, stakingQuerier pcommon.StakingQuerier, evmKeeper pcommon.EVMKeeper, bankKeeper pcommon.BankKeeper) (*pcommon.Precompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		stakingKeeper:  stakingKeeper,
		stakingQuerier: stakingQuerier,
		evmKeeper:      evmKeeper,
		bankKeeper:     bankKeeper,
		address:        common.HexToAddress(StakingAddress),
	}

	for name, m := range newAbi.Methods {
		switch name {
		case DelegateMethod:
			p.DelegateID = m.ID
		case RedelegateMethod:
			p.RedelegateID = m.ID
		case UndelegateMethod:
			p.UndelegateID = m.ID
		case DelegationMethod:
			p.DelegationID = m.ID
		}
	}

	return pcommon.NewPrecompile(newAbi, p, p.address, "staking"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	if bytes.Equal(method.ID, p.DelegateID) {
		return 50000
	} else if bytes.Equal(method.ID, p.RedelegateID) {
		return 70000
	} else if bytes.Equal(method.ID, p.UndelegateID) {
		return 50000
	}

	// This should never happen since this is going to fail during Run
	return pcommon.UnknownMethodCallGas
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) (bz []byte, err error) {
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, errors.New("cannot delegatecall staking")
	}
	switch method.Name {
	case DelegateMethod:
		if readOnly {
			return nil, errors.New("cannot call staking precompile from staticcall")
		}
		return p.delegate(ctx, method, caller, args, value, hooks)
	case RedelegateMethod:
		if readOnly {
			return nil, errors.New("cannot call staking precompile from staticcall")
		}
		return p.redelegate(ctx, method, caller, args, value)
	case UndelegateMethod:
		if readOnly {
			return nil, errors.New("cannot call staking precompile from staticcall")
		}
		return p.undelegate(ctx, method, caller, args, value)
	case DelegationMethod:
		return p.delegation(ctx, method, args, value)
	}
	return
}

func (p PrecompileExecutor) delegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, hooks *tracing.Hooks) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	// if delegator is associated, then it must have Account set already
	// if delegator is not associated, then it can't delegate anyway (since
	// there is no good way to merge delegations if it becomes associated)
	delegator, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, types.NewAssociationMissingErr(caller.Hex())
	}
	validatorBech32 := args[0].(string)
	if value == nil || value.Sign() == 0 {
		return nil, errors.New("set `value` field to non-zero to send delegate fund")
	}
	coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), delegator, value, p.bankKeeper, p.evmKeeper, hooks)
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

func (p PrecompileExecutor) redelegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 3); err != nil {
		return nil, err
	}
	delegator, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, types.NewAssociationMissingErr(caller.Hex())
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

func (p PrecompileExecutor) undelegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	delegator, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, types.NewAssociationMissingErr(caller.Hex())
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

type Delegation struct {
	Balance    Balance
	Delegation DelegationDetails
}

type Balance struct {
	Amount *big.Int
	Denom  string
}

type DelegationDetails struct {
	DelegatorAddress string
	Shares           *big.Int
	Decimals         *big.Int
	ValidatorAddress string
}

func (p PrecompileExecutor) delegation(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}

	seiDelegatorAddress, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, err
	}

	validatorBech32 := args[1].(string)
	delegationRequest := &stakingtypes.QueryDelegationRequest{
		DelegatorAddr: seiDelegatorAddress.String(),
		ValidatorAddr: validatorBech32,
	}

	delegationResponse, err := p.stakingQuerier.Delegation(sdk.WrapSDKContext(ctx), delegationRequest)
	if err != nil {
		return nil, err
	}

	delegation := Delegation{
		Balance: Balance{
			Amount: delegationResponse.GetDelegationResponse().GetBalance().Amount.BigInt(),
			Denom:  delegationResponse.GetDelegationResponse().GetBalance().Denom,
		},
		Delegation: DelegationDetails{
			DelegatorAddress: delegationResponse.GetDelegationResponse().GetDelegation().DelegatorAddress,
			Shares:           delegationResponse.GetDelegationResponse().GetDelegation().Shares.BigInt(),
			Decimals:         big.NewInt(sdk.Precision),
			ValidatorAddress: delegationResponse.GetDelegationResponse().GetDelegation().ValidatorAddress,
		},
	}

	return method.Outputs.Pack(delegation)
}
