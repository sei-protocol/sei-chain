package staking

import (
	"bytes"
	"embed"
	"encoding/hex"
	"errors"
	"math/big"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	DelegateMethod        = "delegate"
	RedelegateMethod      = "redelegate"
	UndelegateMethod      = "undelegate"
	DelegationMethod      = "delegation"
	CreateValidatorMethod = "createValidator"
	EditValidatorMethod   = "editValidator"
)

const (
	StakingAddress = "0x0000000000000000000000000000000000001005"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	stakingKeeper  utils.StakingKeeper
	stakingQuerier utils.StakingQuerier
	evmKeeper      utils.EVMKeeper
	bankKeeper     utils.BankKeeper
	address        common.Address

	DelegateID        []byte
	RedelegateID      []byte
	UndelegateID      []byte
	DelegationID      []byte
	CreateValidatorID []byte
	EditValidatorID   []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.Precompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		stakingKeeper:  keepers.StakingK(),
		stakingQuerier: keepers.StakingQ(),
		evmKeeper:      keepers.EVMK(),
		bankKeeper:     keepers.BankK(),
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
		case CreateValidatorMethod:
			p.CreateValidatorID = m.ID
		case EditValidatorMethod:
			p.EditValidatorID = m.ID
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
	} else if bytes.Equal(method.ID, p.CreateValidatorID) {
		return 100000
	} else if bytes.Equal(method.ID, p.EditValidatorID) {
		return 100000
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
		return p.delegate(ctx, method, caller, args, value, hooks, evm)
	case RedelegateMethod:
		if readOnly {
			return nil, errors.New("cannot call staking precompile from staticcall")
		}
		return p.redelegate(ctx, method, caller, args, value, evm)
	case UndelegateMethod:
		if readOnly {
			return nil, errors.New("cannot call staking precompile from staticcall")
		}
		return p.undelegate(ctx, method, caller, args, value, evm)
	case CreateValidatorMethod:
		if readOnly {
			return nil, errors.New("cannot call staking precompile from staticcall")
		}
		return p.createValidator(ctx, method, caller, args, value, hooks, evm)
	case EditValidatorMethod:
		if readOnly {
			return nil, errors.New("cannot call staking precompile from staticcall")
		}
		return p.editValidator(ctx, method, caller, args, value, hooks, evm)
	case DelegationMethod:
		return p.delegation(ctx, method, args, value)
	}
	return
}

func (p PrecompileExecutor) delegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, hooks *tracing.Hooks, evm *vm.EVM) ([]byte, error) {
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
	coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), delegator, value, p.bankKeeper, p.evmKeeper, hooks, evm.GetDepth())
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
	
	// Emit EVM event
	if emitErr := pcommon.EmitDelegateEvent(ctx, evm, p.address, caller, validatorBech32, value); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit EVM delegate event", "error", emitErr)
	}
	
	return method.Outputs.Pack(true)
}

func (p PrecompileExecutor) redelegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, evm *vm.EVM) ([]byte, error) {
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
	
	// Emit EVM event
	if emitErr := pcommon.EmitRedelegateEvent(ctx, evm, p.address, caller, srcValidatorBech32, dstValidatorBech32, amount); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit EVM redelegate event", "error", emitErr)
	}
	
	return method.Outputs.Pack(true)
}

func (p PrecompileExecutor) undelegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, evm *vm.EVM) ([]byte, error) {
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
	
	// Emit EVM event
	if emitErr := pcommon.EmitUndelegateEvent(ctx, evm, p.address, caller, validatorBech32, amount); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit EVM undelegate event", "error", emitErr)
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

func (p PrecompileExecutor) createValidator(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, hooks *tracing.Hooks, evm *vm.EVM) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(args, 6); err != nil {
		return nil, err
	}

	// Extract arguments
	pubKeyHex := args[0].(string)
	moniker := args[1].(string)
	commissionRateStr := args[2].(string)
	commissionMaxRateStr := args[3].(string)
	commissionMaxChangeRateStr := args[4].(string)
	minSelfDelegation := args[5].(*big.Int)

	// Get validator address (caller's associated Sei address)
	valAddress, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, types.NewAssociationMissingErr(caller.Hex())
	}

	// Parse public key from hex
	pubKeyBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return nil, errors.New("invalid public key hex format")
	}

	// Create ed25519 public key
	pubKey := &ed25519.PubKey{Key: pubKeyBytes}

	// Parse commission rates
	commissionRate, err := sdk.NewDecFromStr(commissionRateStr)
	if err != nil {
		return nil, errors.New("invalid commission rate")
	}

	commissionMaxRate, err := sdk.NewDecFromStr(commissionMaxRateStr)
	if err != nil {
		return nil, errors.New("invalid commission max rate")
	}

	commissionMaxChangeRate, err := sdk.NewDecFromStr(commissionMaxChangeRateStr)
	if err != nil {
		return nil, errors.New("invalid commission max change rate")
	}

	commission := stakingtypes.NewCommissionRates(commissionRate, commissionMaxRate, commissionMaxChangeRate)

	if value == nil || value.Sign() == 0 {
		return nil, errors.New("set `value` field to non-zero to send delegate fund")
	}

	coin, err := pcommon.HandlePaymentUsei(
		ctx,
		p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address),
		valAddress,
		value,
		p.bankKeeper,
		p.evmKeeper,
		hooks,
		evm.GetDepth())
	if err != nil {
		return nil, err
	}

	description := stakingtypes.Description{
		Moniker: moniker,
	}

	msg, err := stakingtypes.NewMsgCreateValidator(
		sdk.ValAddress(valAddress),
		pubKey,
		coin,
		description,
		commission,
		sdk.NewIntFromBigInt(minSelfDelegation),
	)
	if err != nil {
		return nil, err
	}

	_, err = p.stakingKeeper.CreateValidator(sdk.WrapSDKContext(ctx), msg)
	if err != nil {
		return nil, err
	}

	// Emit EVM event
	if emitErr := pcommon.EmitValidatorCreatedEvent(ctx, evm, p.address, caller, sdk.ValAddress(valAddress).String(), moniker); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit EVM validator created event", "error", emitErr)
	}

	return method.Outputs.Pack(true)
}

func (p PrecompileExecutor) editValidator(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, hooks *tracing.Hooks, evm *vm.EVM) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(args, 4); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	// Extract arguments
	moniker := args[0].(string)
	commissionRateStr := args[1].(string)
	minSelfDelegationStr := args[2].(string)
	identity := args[3].(string)

	// Get validator address (caller's associated Sei address)
	valAddress, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, types.NewAssociationMissingErr(caller.Hex())
	}

	// Parse commission rate if provided
	var commissionRate *sdk.Dec
	if commissionRateStr != "" {
		rate, err := sdk.NewDecFromStr(commissionRateStr)
		if err != nil {
			return nil, errors.New("invalid commission rate")
		}
		commissionRate = &rate
	}

	// Parse min self delegation if provided
	var minSelfDelegation *sdk.Int
	if minSelfDelegationStr != "" {
		msd, ok := new(big.Int).SetString(minSelfDelegationStr, 10)
		if !ok {
			return nil, errors.New("invalid min self delegation")
		}
		minSelfDelegationInt := sdk.NewIntFromBigInt(msd)
		minSelfDelegation = &minSelfDelegationInt
	}

	description := stakingtypes.Description{
		Moniker:  moniker,
		Identity: identity,
	}

	msg := stakingtypes.NewMsgEditValidator(
		sdk.ValAddress(valAddress),
		description,
		commissionRate,
		minSelfDelegation,
	)

	_, err := p.stakingKeeper.EditValidator(sdk.WrapSDKContext(ctx), msg)
	if err != nil {
		return nil, err
	}

	// Emit EVM event
	if emitErr := pcommon.EmitValidatorEditedEvent(ctx, evm, p.address, caller, sdk.ValAddress(valAddress).String(), moniker); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit EVM validator edited event", "error", emitErr)
	}

	return method.Outputs.Pack(true)
}
