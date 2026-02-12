package staking

import (
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	DelegateMethod                      = "delegate"
	RedelegateMethod                    = "redelegate"
	UndelegateMethod                    = "undelegate"
	DelegationMethod                    = "delegation"
	CreateValidatorMethod               = "createValidator"
	EditValidatorMethod                 = "editValidator"
	ValidatorsMethod                    = "validators"
	ValidatorMethod                     = "validator"
	ValidatorDelegationsMethod          = "validatorDelegations"
	ValidatorUnbondingDelegationsMethod = "validatorUnbondingDelegations"
	UnbondingDelegationMethod           = "unbondingDelegation"
	DelegatorDelegationsMethod          = "delegatorDelegations"
	DelegatorValidatorMethod            = "delegatorValidator"
	DelegatorUnbondingDelegationsMethod = "delegatorUnbondingDelegations"
	RedelegationsMethod                 = "redelegations"
	DelegatorValidatorsMethod           = "delegatorValidators"
	HistoricalInfoMethod                = "historicalInfo"
	PoolMethod                          = "pool"
	ParamsMethod                        = "params"
)

const (
	StakingAddress = "0x0000000000000000000000000000000000001005"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	stakingKeeper      utils.StakingKeeper
	stakingQuerier     utils.StakingQuerier
	evmKeeper          utils.EVMKeeper
	bankKeeper         utils.BankKeeper
	distributionKeeper utils.DistributionKeeper
	address            common.Address

	DelegateID                      []byte
	RedelegateID                    []byte
	UndelegateID                    []byte
	DelegationID                    []byte
	CreateValidatorID               []byte
	EditValidatorID                 []byte
	ValidatorsID                    []byte
	ValidatorID                     []byte
	ValidatorDelegationsID          []byte
	ValidatorUnbondingDelegationsID []byte
	UnbondingDelegationID           []byte
	DelegatorDelegationsID          []byte
	DelegatorValidatorID            []byte
	DelegatorUnbondingDelegationsID []byte
	RedelegationsID                 []byte
	DelegatorValidatorsID           []byte
	HistoricalInfoID                []byte
	PoolID                          []byte
	ParamsID                        []byte
}

func NewPrecompile(keepers utils.Keepers) (*pcommon.DynamicGasPrecompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		stakingKeeper:      keepers.StakingK(),
		stakingQuerier:     keepers.StakingQ(),
		evmKeeper:          keepers.EVMK(),
		bankKeeper:         keepers.BankK(),
		distributionKeeper: keepers.DistributionK(),
		address:            common.HexToAddress(StakingAddress),
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
		case ValidatorsMethod:
			p.ValidatorsID = m.ID
		case ValidatorMethod:
			p.ValidatorID = m.ID
		case ValidatorDelegationsMethod:
			p.ValidatorDelegationsID = m.ID
		case ValidatorUnbondingDelegationsMethod:
			p.ValidatorUnbondingDelegationsID = m.ID
		case UnbondingDelegationMethod:
			p.UnbondingDelegationID = m.ID
		case DelegatorDelegationsMethod:
			p.DelegatorDelegationsID = m.ID
		case DelegatorValidatorMethod:
			p.DelegatorValidatorID = m.ID
		case DelegatorUnbondingDelegationsMethod:
			p.DelegatorUnbondingDelegationsID = m.ID
		case RedelegationsMethod:
			p.RedelegationsID = m.ID
		case DelegatorValidatorsMethod:
			p.DelegatorValidatorsID = m.ID
		case HistoricalInfoMethod:
			p.HistoricalInfoID = m.ID
		case PoolMethod:
			p.PoolID = m.ID
		case ParamsMethod:
			p.ParamsID = m.ID
		}
	}

	return pcommon.NewDynamicGasPrecompile(newAbi, p, p.address, "staking"), nil
}

func (p PrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return p.evmKeeper
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, hooks *tracing.Hooks) (ret []byte, remainingGas uint64, err error) {
	if ctx.EVMPrecompileCalledFromDelegateCall() {
		return nil, 0, errors.New("cannot delegatecall staking")
	}
	switch method.Name {
	case DelegateMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call staking precompile from staticcall")
		}
		return p.delegate(ctx, method, caller, args, value, hooks, evm)
	case RedelegateMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call staking precompile from staticcall")
		}
		return p.redelegate(ctx, method, caller, args, value, evm)
	case UndelegateMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call staking precompile from staticcall")
		}
		return p.undelegate(ctx, method, caller, args, value, evm)
	case CreateValidatorMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call staking precompile from staticcall")
		}
		return p.createValidator(ctx, method, caller, args, value, hooks, evm)
	case EditValidatorMethod:
		if readOnly {
			return nil, 0, errors.New("cannot call staking precompile from staticcall")
		}
		return p.editValidator(ctx, method, caller, args, value, hooks, evm)
	case DelegationMethod:
		return p.delegation(ctx, method, args, value)
	case ValidatorsMethod:
		return p.validators(ctx, method, args, value)
	case ValidatorMethod:
		return p.validator(ctx, method, args, value)
	case ValidatorDelegationsMethod:
		return p.validatorDelegations(ctx, method, args, value)
	case ValidatorUnbondingDelegationsMethod:
		return p.validatorUnbondingDelegations(ctx, method, args, value)
	case UnbondingDelegationMethod:
		return p.unbondingDelegation(ctx, method, args, value)
	case DelegatorDelegationsMethod:
		return p.delegatorDelegations(ctx, method, args, value)
	case DelegatorValidatorMethod:
		return p.delegatorValidator(ctx, method, args, value)
	case DelegatorUnbondingDelegationsMethod:
		return p.delegatorUnbondingDelegations(ctx, method, args, value)
	case RedelegationsMethod:
		return p.redelegations(ctx, method, args, value)
	case DelegatorValidatorsMethod:
		return p.delegatorValidators(ctx, method, args, value)
	case HistoricalInfoMethod:
		return p.historicalInfo(ctx, method, args, value)
	case PoolMethod:
		return p.pool(ctx, method, args, value)
	case ParamsMethod:
		return p.params(ctx, method, args, value)
	}
	return
}

func (p PrecompileExecutor) delegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, hooks *tracing.Hooks, evm *vm.EVM) ([]byte, uint64, error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}
	// if delegator is associated, then it must have Account set already
	// if delegator is not associated, then it can't delegate anyway (since
	// there is no good way to merge delegations if it becomes associated)
	delegator, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, 0, types.NewAssociationMissingErr(caller.Hex())
	}
	validatorBech32 := args[0].(string)
	if value == nil || value.Sign() == 0 {
		return nil, 0, errors.New("set `value` field to non-zero to send delegate fund")
	}
	coin, err := pcommon.HandlePaymentUsei(ctx, p.evmKeeper.GetSeiAddressOrDefault(ctx, p.address), delegator, value, p.bankKeeper, p.evmKeeper, hooks, evm.GetDepth())
	if err != nil {
		return nil, 0, err
	}
	withdrawAddress := p.distributionKeeper.GetDelegatorWithdrawAddr(ctx, delegator)
	withdrawAddressBalanceBefore := p.bankKeeper.GetBalance(ctx, withdrawAddress, sdk.MustGetBaseDenom())
	_, err = p.stakingKeeper.Delegate(sdk.WrapSDKContext(ctx), &stakingtypes.MsgDelegate{
		DelegatorAddress: delegator.String(),
		ValidatorAddress: validatorBech32,
		Amount:           coin,
	})
	if err != nil {
		return nil, 0, err
	}
	withdrawAddressBalanceAfter := p.bankKeeper.GetBalance(ctx, withdrawAddress, sdk.MustGetBaseDenom())
	// Use Int arithmetic because when withdrawAddress == delegator (the default),
	// the delegated coin.Amount is sent from the delegator to the staking module,
	// which can make balanceAfter < balanceBefore. We compensate for that.
	rewardsAmount := withdrawAddressBalanceAfter.Amount.Sub(withdrawAddressBalanceBefore.Amount)
	if withdrawAddress.Equals(delegator) {
		rewardsAmount = rewardsAmount.Add(coin.Amount)
	}
	if rewardsAmount.IsNegative() {
		return nil, 0, fmt.Errorf("unexpected negative rewards amount: %s", rewardsAmount.String())
	}

	// Emit EVM event
	if emitErr := pcommon.EmitDelegateEvent(evm, p.address, caller, validatorBech32, value); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit EVM delegate event", "error", emitErr)
	}

	if emitErr := pcommon.EmitDelegationRewardsWithdrawnEvent(evm, p.address, caller, validatorBech32, rewardsAmount.BigInt()); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit rewards withdrawn event", "error", emitErr)
	}

	bz, err := method.Outputs.Pack(true)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) redelegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, evm *vm.EVM) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 3); err != nil {
		return nil, 0, err
	}
	delegator, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, 0, types.NewAssociationMissingErr(caller.Hex())
	}
	srcValidatorBech32 := args[0].(string)
	dstValidatorBech32 := args[1].(string)
	amount := args[2].(*big.Int)

	// Pre-withdraw rewards from the destination validator if a delegation already exists.
	// WithdrawDelegationRewards reinitializes the delegation's starting info, so the
	// subsequent BeginRedelegate's internal Delegate call will see zero pending dst rewards.
	// This lets us separately attribute rewards to each validator.
	dstRewardAmount := big.NewInt(0)
	dstValAddr, err := sdk.ValAddressFromBech32(dstValidatorBech32)
	if err == nil {
		dstWithdrawnCoins, wErr := p.distributionKeeper.WithdrawDelegationRewards(ctx, delegator, dstValAddr)
		if wErr == nil {
			dstRewardAmount = dstWithdrawnCoins.AmountOf(sdk.MustGetBaseDenom()).BigInt()
		}
	}

	// Track balance changes around BeginRedelegate to capture src validator rewards only
	// (dst rewards were already withdrawn above).
	withdrawAddress := p.distributionKeeper.GetDelegatorWithdrawAddr(ctx, delegator)
	withdrawAddressBalanceBefore := p.bankKeeper.GetBalance(ctx, withdrawAddress, sdk.MustGetBaseDenom())
	_, err = p.stakingKeeper.BeginRedelegate(sdk.WrapSDKContext(ctx), &stakingtypes.MsgBeginRedelegate{
		DelegatorAddress:    delegator.String(),
		ValidatorSrcAddress: srcValidatorBech32,
		ValidatorDstAddress: dstValidatorBech32,
		Amount:              sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewIntFromBigInt(amount)),
	})
	if err != nil {
		return nil, 0, err
	}
	withdrawAddressBalanceAfter := p.bankKeeper.GetBalance(ctx, withdrawAddress, sdk.MustGetBaseDenom())
	srcRewardsWithdrawn := withdrawAddressBalanceAfter.Sub(withdrawAddressBalanceBefore)

	// Emit EVM event
	if emitErr := pcommon.EmitRedelegateEvent(evm, p.address, caller, srcValidatorBech32, dstValidatorBech32, amount); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit EVM redelegate event", "error", emitErr)
	}

	if emitErr := pcommon.EmitDelegationRewardsWithdrawnEvent(evm, p.address, caller, srcValidatorBech32, srcRewardsWithdrawn.Amount.BigInt()); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit rewards withdrawn event", "error", emitErr)
	}

	if emitErr := pcommon.EmitDelegationRewardsWithdrawnEvent(evm, p.address, caller, dstValidatorBech32, dstRewardAmount); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit rewards withdrawn event", "error", emitErr)
	}

	bz, err := method.Outputs.Pack(true)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) undelegate(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, evm *vm.EVM) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}
	delegator, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, 0, types.NewAssociationMissingErr(caller.Hex())
	}
	validatorBech32 := args[0].(string)
	amount := args[1].(*big.Int)
	withdrawAddress := p.distributionKeeper.GetDelegatorWithdrawAddr(ctx, delegator)
	withdrawAddressBalanceBefore := p.bankKeeper.GetBalance(ctx, withdrawAddress, sdk.MustGetBaseDenom())
	_, err := p.stakingKeeper.Undelegate(sdk.WrapSDKContext(ctx), &stakingtypes.MsgUndelegate{
		DelegatorAddress: delegator.String(),
		ValidatorAddress: validatorBech32,
		Amount:           sdk.NewCoin(p.evmKeeper.GetBaseDenom(ctx), sdk.NewIntFromBigInt(amount)),
	})
	if err != nil {
		return nil, 0, err
	}
	withdrawAddressBalanceAfter := p.bankKeeper.GetBalance(ctx, withdrawAddress, sdk.MustGetBaseDenom())
	rewardsWithdrawn := withdrawAddressBalanceAfter.Sub(withdrawAddressBalanceBefore)

	// Emit EVM event
	if emitErr := pcommon.EmitUndelegateEvent(evm, p.address, caller, validatorBech32, amount); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit EVM undelegate event", "error", emitErr)
	}

	if emitErr := pcommon.EmitDelegationRewardsWithdrawnEvent(evm, p.address, caller, validatorBech32, rewardsWithdrawn.Amount.BigInt()); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit rewards withdrawn event", "error", emitErr)
	}

	bz, err := method.Outputs.Pack(true)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
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

func (p PrecompileExecutor) delegation(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	seiDelegatorAddress, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	validatorBech32 := args[1].(string)
	delegationRequest := &stakingtypes.QueryDelegationRequest{
		DelegatorAddr: seiDelegatorAddress.String(),
		ValidatorAddr: validatorBech32,
	}

	delegationResponse, err := p.stakingQuerier.Delegation(sdk.WrapSDKContext(ctx), delegationRequest)
	if err != nil {
		return nil, 0, err
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

	bz, err := method.Outputs.Pack(delegation)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

type ValidatorsResponse struct {
	Validators []Validator
	NextKey    []byte
}

type DelegationsResponse struct {
	Delegations []Delegation
	NextKey     []byte
}

type UnbondingDelegationsResponse struct {
	UnbondingDelegations []UnbondingDelegation
	NextKey              []byte
}

type RedelegationsResponse struct {
	Redelegations []Redelegation
	NextKey       []byte
}

type Validator struct {
	OperatorAddress         string
	ConsensusPubkey         []byte
	Jailed                  bool
	Status                  int32
	Tokens                  string
	DelegatorShares         string
	Description             string
	UnbondingHeight         int64
	UnbondingTime           int64
	CommissionRate          string
	CommissionMaxRate       string
	CommissionMaxChangeRate string
	CommissionUpdateTime    int64
	MinSelfDelegation       string
}

func (p PrecompileExecutor) validators(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	validatorsRequest := &stakingtypes.QueryValidatorsRequest{
		Status: args[0].(string),
		Pagination: &query.PageRequest{
			Key: args[1].([]byte),
		},
	}

	validatorsResponse, err := p.stakingQuerier.Validators(sdk.WrapSDKContext(ctx), validatorsRequest)
	if err != nil {
		return nil, 0, err
	}

	res := ValidatorsResponse{
		Validators: make([]Validator, len(validatorsResponse.Validators)),
		NextKey:    validatorsResponse.Pagination.NextKey,
	}
	for i, validator := range validatorsResponse.Validators {
		res.Validators[i] = Validator{
			OperatorAddress:         validator.OperatorAddress,
			ConsensusPubkey:         validator.ConsensusPubkey.Value,
			Jailed:                  validator.Jailed,
			Status:                  int32(validator.Status),
			Tokens:                  validator.Tokens.String(),
			DelegatorShares:         validator.DelegatorShares.String(),
			Description:             validator.Description.String(),
			UnbondingHeight:         validator.UnbondingHeight,
			UnbondingTime:           validator.UnbondingTime.Unix(),
			CommissionRate:          validator.Commission.Rate.String(),
			CommissionMaxRate:       validator.Commission.MaxRate.String(),
			CommissionMaxChangeRate: validator.Commission.MaxChangeRate.String(),
			CommissionUpdateTime:    validator.Commission.UpdateTime.Unix(),
			MinSelfDelegation:       validator.MinSelfDelegation.String(),
		}
	}

	bz, err := method.Outputs.Pack(res)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) createValidator(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, hooks *tracing.Hooks, evm *vm.EVM) ([]byte, uint64, error) {
	if err := pcommon.ValidateArgsLength(args, 6); err != nil {
		return nil, 0, err
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
		return nil, 0, types.NewAssociationMissingErr(caller.Hex())
	}

	// Parse public key from hex
	pubKeyBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return nil, 0, errors.New("invalid public key hex format")
	}

	// Create ed25519 public key
	pubKey := &ed25519.PubKey{Key: pubKeyBytes}

	// Parse commission rates
	commissionRate, err := sdk.NewDecFromStr(commissionRateStr)
	if err != nil {
		return nil, 0, errors.New("invalid commission rate")
	}

	commissionMaxRate, err := sdk.NewDecFromStr(commissionMaxRateStr)
	if err != nil {
		return nil, 0, errors.New("invalid commission max rate")
	}

	commissionMaxChangeRate, err := sdk.NewDecFromStr(commissionMaxChangeRateStr)
	if err != nil {
		return nil, 0, errors.New("invalid commission max change rate")
	}

	commission := stakingtypes.NewCommissionRates(commissionRate, commissionMaxRate, commissionMaxChangeRate)

	if value == nil || value.Sign() == 0 {
		return nil, 0, errors.New("set `value` field to non-zero to send delegate fund")
	}

	// Validate minimum self delegation
	if minSelfDelegation == nil || minSelfDelegation.Sign() <= 0 {
		return nil, 0, errors.New("minimum self delegation must be a positive integer: invalid request")
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
		return nil, 0, err
	}

	description := stakingtypes.NewDescription(moniker, "", "", "", "")

	msg, err := stakingtypes.NewMsgCreateValidator(
		sdk.ValAddress(valAddress),
		pubKey,
		coin,
		description,
		commission,
		sdk.NewIntFromBigInt(minSelfDelegation),
	)
	if err != nil {
		return nil, 0, err
	}

	if err := msg.ValidateBasic(); err != nil {
		return nil, 0, err
	}

	_, err = p.stakingKeeper.CreateValidator(sdk.WrapSDKContext(ctx), msg)
	if err != nil {
		return nil, 0, err
	}

	// Emit EVM event
	if emitErr := pcommon.EmitValidatorCreatedEvent(evm, p.address, caller, sdk.ValAddress(valAddress).String(), moniker); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit EVM validator created event", "error", emitErr)
	}

	bz, err := method.Outputs.Pack(true)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) editValidator(ctx sdk.Context, method *abi.Method, caller common.Address, args []interface{}, value *big.Int, hooks *tracing.Hooks, evm *vm.EVM) ([]byte, uint64, error) {
	if err := pcommon.ValidateArgsLength(args, 3); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	// Extract arguments
	moniker := args[0].(string)
	commissionRateStr := args[1].(string)
	minSelfDelegation := args[2].(*big.Int)

	// Get validator address (caller's associated Sei address)
	valAddress, associated := p.evmKeeper.GetSeiAddress(ctx, caller)
	if !associated {
		return nil, 0, types.NewAssociationMissingErr(caller.Hex())
	}

	// Parse commission rate if provided
	var commissionRate *sdk.Dec
	if commissionRateStr != "" {
		rate, err := sdk.NewDecFromStr(commissionRateStr)
		if err != nil {
			return nil, 0, errors.New("invalid commission rate")
		}
		commissionRate = &rate
	}

	// Convert min self delegation if not zero
	var minSelfDelegationInt *sdk.Int
	if minSelfDelegation != nil && minSelfDelegation.Sign() > 0 {
		msd := sdk.NewIntFromBigInt(minSelfDelegation)
		minSelfDelegationInt = &msd
	}

	description := stakingtypes.NewDescription(
		moniker,
		stakingtypes.DoNotModifyDesc,
		stakingtypes.DoNotModifyDesc,
		stakingtypes.DoNotModifyDesc,
		stakingtypes.DoNotModifyDesc,
	)

	msg := stakingtypes.NewMsgEditValidator(
		sdk.ValAddress(valAddress),
		description,
		commissionRate,
		minSelfDelegationInt,
	)

	if err := msg.ValidateBasic(); err != nil {
		return nil, 0, err
	}

	_, err := p.stakingKeeper.EditValidator(sdk.WrapSDKContext(ctx), msg)
	if err != nil {
		return nil, 0, err
	}

	// Emit EVM event
	if emitErr := pcommon.EmitValidatorEditedEvent(evm, p.address, caller, sdk.ValAddress(valAddress).String(), moniker); emitErr != nil {
		// Log error but don't fail the transaction
		ctx.Logger().Error("Failed to emit EVM validator edited event", "error", emitErr)
	}

	bz, err := method.Outputs.Pack(true)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) validator(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	validatorBech32 := args[0].(string)
	validatorRequest := &stakingtypes.QueryValidatorRequest{
		ValidatorAddr: validatorBech32,
	}

	validatorResponse, err := p.stakingQuerier.Validator(sdk.WrapSDKContext(ctx), validatorRequest)
	if err != nil {
		return nil, 0, err
	}

	validator := convertValidatorToPrecompileType(validatorResponse.Validator)
	bz, err := method.Outputs.Pack(validator)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) validatorDelegations(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	validatorBech32 := args[0].(string)
	nextKey := args[1].([]byte)

	request := &stakingtypes.QueryValidatorDelegationsRequest{
		ValidatorAddr: validatorBech32,
		Pagination: &query.PageRequest{
			Key: nextKey,
		},
	}

	response, err := p.stakingQuerier.ValidatorDelegations(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	delegations := make([]Delegation, len(response.DelegationResponses))
	for i, dr := range response.DelegationResponses {
		delegations[i] = Delegation{
			Balance: Balance{
				Amount: dr.Balance.Amount.BigInt(),
				Denom:  dr.Balance.Denom,
			},
			Delegation: DelegationDetails{
				DelegatorAddress: dr.Delegation.DelegatorAddress,
				Shares:           dr.Delegation.Shares.BigInt(),
				Decimals:         big.NewInt(sdk.Precision),
				ValidatorAddress: dr.Delegation.ValidatorAddress,
			},
		}
	}

	result := DelegationsResponse{
		Delegations: delegations,
		NextKey:     response.Pagination.NextKey,
	}

	bz, err := method.Outputs.Pack(result)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) validatorUnbondingDelegations(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	validatorBech32 := args[0].(string)
	nextKey := args[1].([]byte)

	request := &stakingtypes.QueryValidatorUnbondingDelegationsRequest{
		ValidatorAddr: validatorBech32,
		Pagination: &query.PageRequest{
			Key: nextKey,
		},
	}

	response, err := p.stakingQuerier.ValidatorUnbondingDelegations(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	unbondingDelegations := make([]UnbondingDelegation, len(response.UnbondingResponses))
	for i, ubd := range response.UnbondingResponses {
		entries := make([]UnbondingDelegationEntry, len(ubd.Entries))
		for j, entry := range ubd.Entries {
			entries[j] = UnbondingDelegationEntry{
				CreationHeight: entry.CreationHeight,
				CompletionTime: entry.CompletionTime.Unix(),
				InitialBalance: entry.InitialBalance.String(),
				Balance:        entry.Balance.String(),
			}
		}
		unbondingDelegations[i] = UnbondingDelegation{
			DelegatorAddress: ubd.DelegatorAddress,
			ValidatorAddress: ubd.ValidatorAddress,
			Entries:          entries,
		}
	}

	result := UnbondingDelegationsResponse{
		UnbondingDelegations: unbondingDelegations,
		NextKey:              response.Pagination.NextKey,
	}

	bz, err := method.Outputs.Pack(result)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) unbondingDelegation(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	seiDelegatorAddress, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	validatorBech32 := args[1].(string)
	request := &stakingtypes.QueryUnbondingDelegationRequest{
		DelegatorAddr: seiDelegatorAddress.String(),
		ValidatorAddr: validatorBech32,
	}

	response, err := p.stakingQuerier.UnbondingDelegation(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	entries := make([]UnbondingDelegationEntry, len(response.Unbond.Entries))
	for i, entry := range response.Unbond.Entries {
		entries[i] = UnbondingDelegationEntry{
			CreationHeight: entry.CreationHeight,
			CompletionTime: entry.CompletionTime.Unix(),
			InitialBalance: entry.InitialBalance.String(),
			Balance:        entry.Balance.String(),
		}
	}

	unbondingDelegation := UnbondingDelegation{
		DelegatorAddress: response.Unbond.DelegatorAddress,
		ValidatorAddress: response.Unbond.ValidatorAddress,
		Entries:          entries,
	}

	bz, err := method.Outputs.Pack(unbondingDelegation)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) delegatorDelegations(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	seiDelegatorAddress, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	nextKey := args[1].([]byte)
	request := &stakingtypes.QueryDelegatorDelegationsRequest{
		DelegatorAddr: seiDelegatorAddress.String(),
		Pagination: &query.PageRequest{
			Key: nextKey,
		},
	}

	response, err := p.stakingQuerier.DelegatorDelegations(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	delegations := make([]Delegation, len(response.DelegationResponses))
	for i, dr := range response.DelegationResponses {
		delegations[i] = Delegation{
			Balance: Balance{
				Amount: dr.Balance.Amount.BigInt(),
				Denom:  dr.Balance.Denom,
			},
			Delegation: DelegationDetails{
				DelegatorAddress: dr.Delegation.DelegatorAddress,
				Shares:           dr.Delegation.Shares.BigInt(),
				Decimals:         big.NewInt(sdk.Precision),
				ValidatorAddress: dr.Delegation.ValidatorAddress,
			},
		}
	}

	result := DelegationsResponse{
		Delegations: delegations,
		NextKey:     response.Pagination.NextKey,
	}

	bz, err := method.Outputs.Pack(result)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) delegatorValidator(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	seiDelegatorAddress, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	validatorBech32 := args[1].(string)
	request := &stakingtypes.QueryDelegatorValidatorRequest{
		DelegatorAddr: seiDelegatorAddress.String(),
		ValidatorAddr: validatorBech32,
	}

	response, err := p.stakingQuerier.DelegatorValidator(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	validator := convertValidatorToPrecompileType(response.Validator)
	bz, err := method.Outputs.Pack(validator)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) delegatorUnbondingDelegations(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	seiDelegatorAddress, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	nextKey := args[1].([]byte)
	request := &stakingtypes.QueryDelegatorUnbondingDelegationsRequest{
		DelegatorAddr: seiDelegatorAddress.String(),
		Pagination: &query.PageRequest{
			Key: nextKey,
		},
	}

	response, err := p.stakingQuerier.DelegatorUnbondingDelegations(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	unbondingDelegations := make([]UnbondingDelegation, len(response.UnbondingResponses))
	for i, ubd := range response.UnbondingResponses {
		entries := make([]UnbondingDelegationEntry, len(ubd.Entries))
		for j, entry := range ubd.Entries {
			entries[j] = UnbondingDelegationEntry{
				CreationHeight: entry.CreationHeight,
				CompletionTime: entry.CompletionTime.Unix(),
				InitialBalance: entry.InitialBalance.String(),
				Balance:        entry.Balance.String(),
			}
		}
		unbondingDelegations[i] = UnbondingDelegation{
			DelegatorAddress: ubd.DelegatorAddress,
			ValidatorAddress: ubd.ValidatorAddress,
			Entries:          entries,
		}
	}

	result := UnbondingDelegationsResponse{
		UnbondingDelegations: unbondingDelegations,
		NextKey:              response.Pagination.NextKey,
	}

	bz, err := method.Outputs.Pack(result)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) redelegations(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 4); err != nil {
		return nil, 0, err
	}

	delegatorStr := args[0].(string)
	srcValidatorStr := args[1].(string)
	dstValidatorStr := args[2].(string)
	nextKey := args[3].([]byte)

	request := &stakingtypes.QueryRedelegationsRequest{
		DelegatorAddr:    delegatorStr,
		SrcValidatorAddr: srcValidatorStr,
		DstValidatorAddr: dstValidatorStr,
		Pagination: &query.PageRequest{
			Key: nextKey,
		},
	}

	response, err := p.stakingQuerier.Redelegations(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	redelegations := make([]Redelegation, len(response.RedelegationResponses))
	for i, redel := range response.RedelegationResponses {
		entries := make([]RedelegationEntry, len(redel.Entries))
		for j, entry := range redel.Entries {
			entries[j] = RedelegationEntry{
				CreationHeight: entry.RedelegationEntry.CreationHeight,
				CompletionTime: entry.RedelegationEntry.CompletionTime.Unix(),
				InitialBalance: entry.RedelegationEntry.InitialBalance.String(),
				SharesDst:      entry.Balance.String(),
			}
		}
		redelegations[i] = Redelegation{
			DelegatorAddress:    redel.Redelegation.DelegatorAddress,
			ValidatorSrcAddress: redel.Redelegation.ValidatorSrcAddress,
			ValidatorDstAddress: redel.Redelegation.ValidatorDstAddress,
			Entries:             entries,
		}
	}

	result := RedelegationsResponse{
		Redelegations: redelegations,
		NextKey:       response.Pagination.NextKey,
	}

	bz, err := method.Outputs.Pack(result)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) delegatorValidators(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, 0, err
	}

	seiDelegatorAddress, err := pcommon.GetSeiAddressFromArg(ctx, args[0], p.evmKeeper)
	if err != nil {
		return nil, 0, err
	}

	nextKey := args[1].([]byte)
	request := &stakingtypes.QueryDelegatorValidatorsRequest{
		DelegatorAddr: seiDelegatorAddress.String(),
		Pagination: &query.PageRequest{
			Key: nextKey,
		},
	}

	response, err := p.stakingQuerier.DelegatorValidators(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	validators := make([]Validator, len(response.Validators))
	for i, val := range response.Validators {
		validators[i] = convertValidatorToPrecompileType(val)
	}

	result := ValidatorsResponse{
		Validators: validators,
		NextKey:    response.Pagination.NextKey,
	}

	bz, err := method.Outputs.Pack(result)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) historicalInfo(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, 0, err
	}

	height := args[0].(int64)
	request := &stakingtypes.QueryHistoricalInfoRequest{
		Height: height,
	}

	response, err := p.stakingQuerier.HistoricalInfo(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	if response.Hist == nil {
		return nil, 0, errors.New("historical info not found")
	}

	validators := make([]Validator, len(response.Hist.Valset))
	for i, val := range response.Hist.Valset {
		validators[i] = convertValidatorToPrecompileType(val)
	}

	historicalInfo := HistoricalInfo{
		Height:     height, // Use the requested height
		Validators: validators,
	}

	bz, err := method.Outputs.Pack(historicalInfo)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) pool(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	request := &stakingtypes.QueryPoolRequest{}
	response, err := p.stakingQuerier.Pool(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	pool := Pool{
		NotBondedTokens: response.Pool.NotBondedTokens.String(),
		BondedTokens:    response.Pool.BondedTokens.String(),
	}

	bz, err := method.Outputs.Pack(pool)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

func (p PrecompileExecutor) params(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, uint64, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, 0, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, 0, err
	}

	request := &stakingtypes.QueryParamsRequest{}
	response, err := p.stakingQuerier.Params(sdk.WrapSDKContext(ctx), request)
	if err != nil {
		return nil, 0, err
	}

	params := Params{
		UnbondingTime:                      uint64(response.Params.UnbondingTime.Seconds()),
		MaxValidators:                      response.Params.MaxValidators,
		MaxEntries:                         response.Params.MaxEntries,
		HistoricalEntries:                  response.Params.HistoricalEntries,
		BondDenom:                          response.Params.BondDenom,
		MinCommissionRate:                  response.Params.MinCommissionRate.String(),
		MaxVotingPowerRatio:                response.Params.MaxVotingPowerRatio.String(),
		MaxVotingPowerEnforcementThreshold: response.Params.MaxVotingPowerEnforcementThreshold.String(),
	}

	bz, err := method.Outputs.Pack(params)
	if err != nil {
		return nil, 0, err
	}
	return bz, pcommon.GetRemainingGas(ctx, p.evmKeeper), nil
}

// Helper function to convert stakingtypes.Validator to precompile Validator type
func convertValidatorToPrecompileType(val stakingtypes.Validator) Validator {
	return Validator{
		OperatorAddress:         val.OperatorAddress,
		ConsensusPubkey:         val.ConsensusPubkey.Value,
		Jailed:                  val.Jailed,
		Status:                  int32(val.Status),
		Tokens:                  val.Tokens.String(),
		DelegatorShares:         val.DelegatorShares.String(),
		Description:             val.Description.String(),
		UnbondingHeight:         val.UnbondingHeight,
		UnbondingTime:           val.UnbondingTime.Unix(),
		CommissionRate:          val.Commission.Rate.String(),
		CommissionMaxRate:       val.Commission.MaxRate.String(),
		CommissionMaxChangeRate: val.Commission.MaxChangeRate.String(),
		CommissionUpdateTime:    val.Commission.UpdateTime.Unix(),
		MinSelfDelegation:       val.MinSelfDelegation.String(),
	}
}

// Additional types for new query methods
type UnbondingDelegationEntry struct {
	CreationHeight int64
	CompletionTime int64
	InitialBalance string
	Balance        string
}

type UnbondingDelegation struct {
	DelegatorAddress string
	ValidatorAddress string
	Entries          []UnbondingDelegationEntry
}

type RedelegationEntry struct {
	CreationHeight int64
	CompletionTime int64
	InitialBalance string
	SharesDst      string
}

type Redelegation struct {
	DelegatorAddress    string
	ValidatorSrcAddress string
	ValidatorDstAddress string
	Entries             []RedelegationEntry
}

type HistoricalInfo struct {
	Height     int64
	Validators []Validator
}

type Pool struct {
	NotBondedTokens string
	BondedTokens    string
}

type Params struct {
	UnbondingTime                      uint64
	MaxValidators                      uint32
	MaxEntries                         uint32
	HistoricalEntries                  uint32
	BondDenom                          string
	MinCommissionRate                  string
	MaxVotingPowerRatio                string
	MaxVotingPowerEnforcementThreshold string
}
