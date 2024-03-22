package staking

import (
	"bytes"
	"embed"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
)

const (
	DelegateMethod           = "delegate"
	RedelegateMethod         = "redelegate"
	UndelegateMethod         = "undelegate"
	DelegationQuery          = "getDelegation"
	StakingPoolQuery         = "getStakingPool"
	UnbondingDelegationQuery = "getUnbondingDelegation"
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

	DelegateID            []byte
	RedelegateID          []byte
	UndelegateID          []byte
	DelegationID          []byte
	StakingPoolID         []byte
	UnbondingDelegationID []byte
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
		case DelegationQuery:
			p.DelegationID = m.ID
		case StakingPoolQuery:
			p.StakingPoolID = m.ID
		case UnbondingDelegationQuery:
			p.UnbondingDelegationID = m.ID
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
	} else if bytes.Equal(methodID, p.DelegationID) {
		return 5000
	} else if bytes.Equal(methodID, p.StakingPoolID) {
		return 5000
	} else if bytes.Equal(methodID, p.UnbondingDelegationID) {
		return 10000
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
	case DelegationQuery:
		return p.getDelegation(ctx, method, caller, args)
	case StakingPoolQuery:
		return p.getStakingPool(ctx, method, caller, args)
	case UnbondingDelegationQuery:
		return p.getUnbondingDelegation(ctx, method, caller, args)
	}
	return
}

func (p Precompile) delegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 2)
	delegator := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
	validatorBech32 := p.evmKeeper.GetSeiAddressOrDefault(ctx, args[0].(common.Address)).String()
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
	srcValidatorBech32 := p.evmKeeper.GetSeiAddressOrDefault(ctx, args[0].(common.Address)).String()
	dstValidatorBech32 := p.evmKeeper.GetSeiAddressOrDefault(ctx, args[1].(common.Address)).String()
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
	validatorBech32 := p.evmKeeper.GetSeiAddressOrDefault(ctx, args[0].(common.Address)).String()
	amount := args[1].(*big.Int)
	_, err := p.stakingKeeper.Undelegate(sdk.WrapSDKContext(ctx), &stakingtypes.MsgUndelegate{
		DelegatorAddress: delegator.String(),
		ValidatorAddress: validatorBech32,
		Amount:           sdk.NewCoin(p.evmKeeper.GetBaseDenom(ctx), sdk.NewIntFromBigInt(amount)),
	})
	if err != nil {
		return nil, err
	}
	unbonds, err := p.stakingKeeper.UnbondingDelegation(sdk.WrapSDKContext(ctx), delegator.String(), validatorBech32)
	if err != nil {
		return nil, err
	}
	entry := unbonds.Unbond.Entries[len(unbonds.Unbond.Entries)-1]
	return method.Outputs.Pack(entry.UnbondingId)
}

func (p Precompile) getDelegation(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 2)
	delegator := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
	validatorBech32 := p.evmKeeper.GetSeiAddressOrDefault(ctx, args[0].(common.Address)).String()
	delegation, err := p.stakingKeeper.Delegation(sdk.WrapSDKContext(ctx), delegator.String(), validatorBech32)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(delegation.DelegationResponse.Balance)
}

func (p Precompile) getStakingPool(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 1)
	validatorBech32 := p.evmKeeper.GetSeiAddressOrDefault(ctx, args[0].(common.Address)).String()
	validator, err := p.stakingKeeper.Validator(sdk.WrapSDKContext(ctx), validatorBech32)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(struct {
		TotalShares *big.Int
		TotalTokens *big.Int
		Status      stakingtypes.BondStatus
		Jailed      bool
	}{
		validator.Validator.DelegatorShares.BigInt(),
		validator.Validator.Tokens.BigInt(),
		validator.Validator.Status,
		validator.Validator.Jailed,
	})
}

func (p Precompile) getUnbondingDelegation(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}) ([]byte, error) {
	pcommon.AssertArgsLength(args, 2)
	delegator := p.evmKeeper.GetSeiAddressOrDefault(ctx, caller)
	validatorBech32 := p.evmKeeper.GetSeiAddressOrDefault(ctx, args[0].(common.Address)).String()
	unbonding, err := p.stakingKeeper.UnbondingDelegation(sdk.WrapSDKContext(ctx), delegator.String(), validatorBech32)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(unbonding)
}
