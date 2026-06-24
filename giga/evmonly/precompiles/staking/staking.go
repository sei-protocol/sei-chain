package staking

import (
	"bytes"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/util"
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

	unknownMethodGas uint64 = 3000
	readGas          uint64 = 3000
	writeGas         uint64 = 20000
	inputByteGas     uint64 = 16

	bondDenom       = "usei"
	precision int64 = 18
	pageLimit       = 100
)

const (
	bondStatusUnspecified int32 = 0
	bondStatusUnbonded    int32 = 1
	bondStatusUnbonding   int32 = 2
	bondStatusBonded      int32 = 3

	// powerReduction matches Cosmos sdk.DefaultPowerReduction so consensus
	// power is denominated in whole SEI (1e6 usei == 1 power).
	powerReduction int64 = 1_000_000
)

var (
	address                   = common.HexToAddress(StakingAddress)
	useiToSwei                = big.NewInt(1_000_000_000_000)
	errReadOnly               = errors.New("cannot call staking precompile from staticcall")
	errDelegateCall           = errors.New("cannot delegatecall staking")
	errMissingStore           = errors.New("staking precompile requires a store")
	errMissingBalanceTransfer = errors.New("staking precompile requires balance transfer")
	errValidatorMissing       = errors.New("validator not found")
	errSelfRedelegation       = errors.New("cannot redelegate to the same validator")
	errTransitiveRedelegation = errors.New("redelegation to this validator already in progress; first redelegation not complete")
	errMaxRedelegationEntries = errors.New("too many redelegation entries for (delegator, src-validator, dst-validator) tuple")
	errMaxUnbondingEntries    = errors.New("too many unbonding delegation entries for (delegator, validator) tuple")
	errDuplicateConsensusKey  = errors.New("validator consensus pubkey already exists")
	errMinSelfDelegation      = errors.New("minimum self delegation must be greater than the current value")
	errSelfDelegationTooLow   = errors.New("minimum self delegation cannot be greater than the validator's self delegation")
)

//go:embed abi.json
var abiFS embed.FS

// Precompile is the SDK-free staking custom precompile for the evm-only path.
type Precompile struct {
	abi     abi.ABI
	address common.Address
}

// Registry exposes only the staking precompile to the evm-only executor.
type Registry struct {
	contract *Precompile
}

// NewPrecompile constructs the staking precompile without Cosmos keepers or
// sdk.Context dependencies.
func NewPrecompile() (*Precompile, error) {
	abiBz, err := abiFS.ReadFile("abi.json")
	if err != nil {
		return nil, err
	}
	parsedABI, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		return nil, err
	}
	return &Precompile{abi: parsedABI, address: address}, nil
}

// NewRegistry returns a registry containing the staking precompile.
func NewRegistry() (Registry, error) {
	contract, err := NewPrecompile()
	if err != nil {
		return Registry{}, err
	}
	return Registry{contract: contract}, nil
}

func (r Registry) Get(addr common.Address) (precompiles.Contract, bool) {
	if addr != address || r.contract == nil {
		return nil, false
	}
	return r.contract, true
}

func (r Registry) Addresses() []common.Address {
	return []common.Address{address}
}

func (p *Precompile) Address() common.Address {
	return p.address
}

func (p *Precompile) ABI() abi.ABI {
	return p.abi
}

func (p *Precompile) RequiredGas(input []byte) uint64 {
	method, _, err := p.prepare(input)
	if err != nil {
		return unknownMethodGas
	}
	gas := readGas
	if isTransaction(method.Name) {
		gas = writeGas
	}
	return gas + inputByteGas*uint64(len(input)) //nolint:gosec // input length is bounded by memory.
}

func (p *Precompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	if ctx.DelegateCall {
		return nil, errDelegateCall
	}
	if ctx.Store == nil {
		return nil, errMissingStore
	}
	method, args, err := p.prepare(input)
	if err != nil {
		return nil, err
	}
	switch method.Name {
	case DelegateMethod:
		return p.delegate(ctx, method, args)
	case RedelegateMethod:
		return p.redelegate(ctx, method, args)
	case UndelegateMethod:
		return p.undelegate(ctx, method, args)
	case CreateValidatorMethod:
		return p.createValidator(ctx, method, args)
	case EditValidatorMethod:
		return p.editValidator(ctx, method, args)
	case DelegationMethod:
		return p.delegation(ctx, method, args)
	case ValidatorsMethod:
		return p.validators(ctx, method, args)
	case ValidatorMethod:
		return p.validator(ctx, method, args)
	case ValidatorDelegationsMethod:
		return p.validatorDelegations(ctx, method, args)
	case ValidatorUnbondingDelegationsMethod:
		return p.validatorUnbondingDelegations(ctx, method, args)
	case UnbondingDelegationMethod:
		return p.unbondingDelegation(ctx, method, args)
	case DelegatorDelegationsMethod:
		return p.delegatorDelegations(ctx, method, args)
	case DelegatorValidatorMethod:
		return p.delegatorValidator(ctx, method, args)
	case DelegatorUnbondingDelegationsMethod:
		return p.delegatorUnbondingDelegations(ctx, method, args)
	case RedelegationsMethod:
		return p.redelegations(ctx, method, args)
	case DelegatorValidatorsMethod:
		return p.delegatorValidators(ctx, method, args)
	case HistoricalInfoMethod:
		return p.historicalInfo(ctx, method, args)
	case PoolMethod:
		return p.pool(ctx, method)
	case ParamsMethod:
		return p.params(ctx, method)
	default:
		return nil, fmt.Errorf("unsupported staking method %s", method.Name)
	}
}

func (p *Precompile) prepare(input []byte) (*abi.Method, []interface{}, error) {
	if len(input) < 4 {
		return nil, nil, errors.New("input too short to extract method ID")
	}
	method, err := p.abi.MethodById(input[:4])
	if err != nil {
		return nil, nil, err
	}
	args, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return nil, nil, err
	}
	return method, args, nil
}

func (p *Precompile) delegate(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := validateWritable(ctx); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	validatorAddress := normalizeValidatorAddress(args[0].(string))
	validator, ok, err := getValidator(ctx.Store, validatorAddress)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errValidatorMissing
	}
	useiAmount, err := stakingValue(ctx.ApparentValue)
	if err != nil {
		return nil, err
	}
	if err := transferPrecompileValueToEscrow(ctx); err != nil {
		return nil, err
	}
	delegator := util.AddressString(ctx.Caller)
	if err := addDelegation(ctx.Store, delegator, validatorAddress, useiAmount); err != nil {
		return nil, err
	}
	if err := addValidatorTokens(ctx.Store, validatorAddress, useiAmount); err != nil {
		return nil, err
	}
	if validator.Status == bondStatusBonded {
		if err := addPoolBonded(ctx.Store, useiAmount); err != nil {
			return nil, err
		}
	} else if err := addPoolNotBonded(ctx.Store, useiAmount); err != nil {
		return nil, err
	}
	p.emit(ctx, "Delegate", ctx.Caller, validatorAddress, util.CloneBig(ctx.ApparentValue))
	p.emit(ctx, "DelegationRewardsWithdrawn", ctx.Caller, validatorAddress, new(big.Int))
	return method.Outputs.Pack(true)
}

func (p *Precompile) redelegate(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := validateWritable(ctx); err != nil {
		return nil, err
	}
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 3); err != nil {
		return nil, err
	}
	delegator := util.AddressString(ctx.Caller)
	srcValidator := normalizeValidatorAddress(args[0].(string))
	dstValidator := normalizeValidatorAddress(args[1].(string))
	amount := args[2].(*big.Int)
	if err := util.ValidatePositiveAmount(amount, "redelegation amount"); err != nil {
		return nil, err
	}
	if srcValidator == dstValidator {
		return nil, errSelfRedelegation
	}
	src, ok, err := getValidator(ctx.Store, srcValidator)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("source %w", errValidatorMissing)
	}
	dst, ok, err := getValidator(ctx.Store, dstValidator)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("destination %w", errValidatorMissing)
	}
	params, err := loadParams(ctx.Store)
	if err != nil {
		return nil, err
	}
	// Disallow transitive redelegations: tokens already received via an
	// in-progress redelegation into srcValidator cannot be redelegated again
	// until that redelegation completes (matches Cosmos HasReceivingRedelegation).
	receiving, err := hasReceivingRedelegation(ctx.Store, delegator, srcValidator)
	if err != nil {
		return nil, err
	}
	if receiving {
		return nil, errTransitiveRedelegation
	}
	if existing, ok, err := getRedelegation(ctx.Store, delegator, srcValidator, dstValidator); err != nil {
		return nil, err
	} else if ok && len(existing.Entries) >= int(params.MaxEntries) {
		return nil, errMaxRedelegationEntries
	}
	if err := validateDelegationAmount(ctx.Store, delegator, srcValidator, amount); err != nil {
		return nil, err
	}
	if err := addDelegation(ctx.Store, delegator, srcValidator, new(big.Int).Neg(amount)); err != nil {
		return nil, err
	}
	if err := addDelegation(ctx.Store, delegator, dstValidator, amount); err != nil {
		return nil, err
	}
	if err := addValidatorTokens(ctx.Store, srcValidator, new(big.Int).Neg(amount)); err != nil {
		return nil, err
	}
	if err := addValidatorTokens(ctx.Store, dstValidator, amount); err != nil {
		return nil, err
	}
	if err := movePoolsForRedelegation(ctx.Store, src.Status, dst.Status, amount); err != nil {
		return nil, err
	}
	if err := addRedelegation(ctx.Store, delegator, srcValidator, dstValidator, amount, util.SaturatingCompletionTime(ctx.Block.Time, params.UnbondingTime)); err != nil {
		return nil, err
	}
	p.emit(ctx, "Redelegate", ctx.Caller, srcValidator, dstValidator, amount)
	p.emit(ctx, "DelegationRewardsWithdrawn", ctx.Caller, srcValidator, new(big.Int))
	p.emit(ctx, "DelegationRewardsWithdrawn", ctx.Caller, dstValidator, new(big.Int))
	return method.Outputs.Pack(true)
}

func (p *Precompile) undelegate(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := validateWritable(ctx); err != nil {
		return nil, err
	}
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	delegator := util.AddressString(ctx.Caller)
	validatorAddress := normalizeValidatorAddress(args[0].(string))
	amount := args[1].(*big.Int)
	if err := util.ValidatePositiveAmount(amount, "undelegation amount"); err != nil {
		return nil, err
	}
	validator, ok, err := getValidator(ctx.Store, validatorAddress)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errValidatorMissing
	}
	params, err := loadParams(ctx.Store)
	if err != nil {
		return nil, err
	}
	if existing, ok, err := getUnbondingDelegation(ctx.Store, delegator, validatorAddress); err != nil {
		return nil, err
	} else if ok && len(existing.Entries) >= int(params.MaxEntries) {
		return nil, errMaxUnbondingEntries
	}
	if err := validateDelegationAmount(ctx.Store, delegator, validatorAddress, amount); err != nil {
		return nil, err
	}
	if err := addDelegation(ctx.Store, delegator, validatorAddress, new(big.Int).Neg(amount)); err != nil {
		return nil, err
	}
	if err := addValidatorTokens(ctx.Store, validatorAddress, new(big.Int).Neg(amount)); err != nil {
		return nil, err
	}
	if err := addUnbondingDelegation(ctx.Store, delegator, validatorAddress, amount, ctx.Block.Number, util.SaturatingCompletionTime(ctx.Block.Time, params.UnbondingTime)); err != nil {
		return nil, err
	}
	if validator.Status == bondStatusBonded {
		if err := addPoolBonded(ctx.Store, new(big.Int).Neg(amount)); err != nil {
			return nil, err
		}
		if err := addPoolNotBonded(ctx.Store, amount); err != nil {
			return nil, err
		}
	}
	p.emit(ctx, "Undelegate", ctx.Caller, validatorAddress, amount)
	p.emit(ctx, "DelegationRewardsWithdrawn", ctx.Caller, validatorAddress, new(big.Int))
	return method.Outputs.Pack(true)
}

func (p *Precompile) createValidator(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := validateWritable(ctx); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 6); err != nil {
		return nil, err
	}
	pubKeyHex := args[0].(string)
	moniker := args[1].(string)
	commissionRate := args[2].(string)
	commissionMaxRate := args[3].(string)
	commissionMaxChangeRate := args[4].(string)
	minSelfDelegation := args[5].(*big.Int)
	pubKey, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return nil, errors.New("invalid public key hex format")
	}
	params, err := loadParams(ctx.Store)
	if err != nil {
		return nil, err
	}
	if err := validateInitialCommission(commissionRate, commissionMaxRate, commissionMaxChangeRate, params.MinCommissionRate); err != nil {
		return nil, err
	}
	if err := util.ValidatePositiveAmount(minSelfDelegation, "minimum self delegation"); err != nil {
		return nil, errors.New("minimum self delegation must be a positive integer: invalid request")
	}
	selfDelegation, err := stakingValue(ctx.ApparentValue)
	if err != nil {
		return nil, err
	}
	if selfDelegation.Cmp(minSelfDelegation) < 0 {
		return nil, errors.New("self delegation is below minimum self delegation")
	}
	validatorAddress := util.AddressString(ctx.Caller)
	if _, exists, err := getValidator(ctx.Store, validatorAddress); err != nil {
		return nil, err
	} else if exists {
		return nil, errors.New("validator already exists")
	}
	if _, exists, err := getValidatorByConsensusPubkey(ctx.Store, pubKey); err != nil {
		return nil, err
	} else if exists {
		return nil, errDuplicateConsensusKey
	}
	if err := transferPrecompileValueToEscrow(ctx); err != nil {
		return nil, err
	}
	validator := Validator{
		OperatorAddress:         validatorAddress,
		ConsensusPubkey:         pubKey,
		Jailed:                  false,
		Status:                  bondStatusUnbonded,
		Tokens:                  selfDelegation.String(),
		DelegatorShares:         selfDelegation.String(),
		Description:             moniker,
		UnbondingHeight:         0,
		UnbondingTime:           0,
		CommissionRate:          commissionRate,
		CommissionMaxRate:       commissionMaxRate,
		CommissionMaxChangeRate: commissionMaxChangeRate,
		CommissionUpdateTime:    saturatingInt64FromUint64(ctx.Block.Time),
		MinSelfDelegation:       minSelfDelegation.String(),
	}
	if err := setValidator(ctx.Store, validator); err != nil {
		return nil, err
	}
	if err := addDelegation(ctx.Store, validatorAddress, validatorAddress, selfDelegation); err != nil {
		return nil, err
	}
	if err := addPoolNotBonded(ctx.Store, selfDelegation); err != nil {
		return nil, err
	}
	if err := setHistoricalInfo(ctx.Store, ctx.Block.Number); err != nil {
		return nil, err
	}
	p.emit(ctx, "ValidatorCreated", ctx.Caller, validatorAddress, moniker)
	return method.Outputs.Pack(true)
}

func (p *Precompile) editValidator(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := validateWritable(ctx); err != nil {
		return nil, err
	}
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 3); err != nil {
		return nil, err
	}
	validatorAddress := util.AddressString(ctx.Caller)
	validator, ok, err := getValidator(ctx.Store, validatorAddress)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errValidatorMissing
	}
	moniker := args[0].(string)
	commissionRate := args[1].(string)
	minSelfDelegation := args[2].(*big.Int)
	if moniker != "" {
		validator.Description = moniker
	}
	if commissionRate != "" {
		params, err := loadParams(ctx.Store)
		if err != nil {
			return nil, err
		}
		if err := validateCommissionUpdate(validator, commissionRate, params.MinCommissionRate, ctx.Block.Time); err != nil {
			return nil, err
		}
		validator.CommissionRate = commissionRate
		validator.CommissionUpdateTime = saturatingInt64FromUint64(ctx.Block.Time)
	}
	if minSelfDelegation != nil && minSelfDelegation.Sign() > 0 {
		current, err := util.ParseAmount(validator.MinSelfDelegation)
		if err != nil {
			return nil, err
		}
		if minSelfDelegation.Cmp(current) <= 0 {
			return nil, errMinSelfDelegation
		}
		tokens, err := util.ParseAmount(validator.Tokens)
		if err != nil {
			return nil, err
		}
		if minSelfDelegation.Cmp(tokens) > 0 {
			return nil, errSelfDelegationTooLow
		}
		validator.MinSelfDelegation = minSelfDelegation.String()
	}
	if err := setValidator(ctx.Store, validator); err != nil {
		return nil, err
	}
	if err := setHistoricalInfo(ctx.Store, ctx.Block.Number); err != nil {
		return nil, err
	}
	p.emit(ctx, "ValidatorEdited", ctx.Caller, validatorAddress, moniker)
	return method.Outputs.Pack(true)
}

func (p *Precompile) delegation(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	delegator := util.AddressString(args[0].(common.Address))
	validatorAddress := normalizeValidatorAddress(args[1].(string))
	record, ok, err := getDelegation(ctx.Store, delegator, validatorAddress)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("delegation not found")
	}
	delegation, err := delegationFromRecord(record)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(delegation)
}

func (p *Precompile) validators(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	status := args[0].(string)
	nextKey := args[1].([]byte)
	validatorAddresses, err := getStringList(ctx.Store, validatorsIndexKey())
	if err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(validatorAddresses))
	matched := make(map[string]Validator, len(validatorAddresses))
	for _, validatorAddress := range validatorAddresses {
		validator, ok, err := getValidator(ctx.Store, validatorAddress)
		if err != nil {
			return nil, err
		}
		if ok && statusMatches(status, validator.Status) {
			filtered = append(filtered, validatorAddress)
			matched[validatorAddress] = validator
		}
	}
	page, outNextKey, err := pageStrings(filtered, nextKey)
	if err != nil {
		return nil, err
	}
	result := ValidatorsResponse{Validators: make([]Validator, 0, len(page)), NextKey: outNextKey}
	for _, validatorAddress := range page {
		result.Validators = append(result.Validators, matched[validatorAddress])
	}
	return method.Outputs.Pack(result)
}

func (p *Precompile) validator(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	validator, ok, err := getValidator(ctx.Store, normalizeValidatorAddress(args[0].(string)))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errValidatorMissing
	}
	return method.Outputs.Pack(validator)
}

func (p *Precompile) validatorDelegations(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	validatorAddress := normalizeValidatorAddress(args[0].(string))
	nextKey := args[1].([]byte)
	delegators, err := getStringList(ctx.Store, validatorDelegationsIndexKey(validatorAddress))
	if err != nil {
		return nil, err
	}
	page, outNextKey, err := pageStrings(delegators, nextKey)
	if err != nil {
		return nil, err
	}
	result := DelegationsResponse{Delegations: make([]Delegation, 0, len(page)), NextKey: outNextKey}
	for _, delegator := range page {
		record, ok, err := getDelegation(ctx.Store, delegator, validatorAddress)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		delegation, err := delegationFromRecord(record)
		if err != nil {
			return nil, err
		}
		result.Delegations = append(result.Delegations, delegation)
	}
	return method.Outputs.Pack(result)
}

func (p *Precompile) validatorUnbondingDelegations(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	validatorAddress := normalizeValidatorAddress(args[0].(string))
	nextKey := args[1].([]byte)
	delegators, err := getStringList(ctx.Store, validatorUnbondingsIndexKey(validatorAddress))
	if err != nil {
		return nil, err
	}
	page, outNextKey, err := pageStrings(delegators, nextKey)
	if err != nil {
		return nil, err
	}
	result := UnbondingDelegationsResponse{UnbondingDelegations: make([]UnbondingDelegation, 0, len(page)), NextKey: outNextKey}
	for _, delegator := range page {
		record, ok, err := getUnbondingDelegation(ctx.Store, delegator, validatorAddress)
		if err != nil {
			return nil, err
		}
		if ok {
			result.UnbondingDelegations = append(result.UnbondingDelegations, record)
		}
	}
	return method.Outputs.Pack(result)
}

func (p *Precompile) unbondingDelegation(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	delegator := util.AddressString(args[0].(common.Address))
	validatorAddress := normalizeValidatorAddress(args[1].(string))
	record, ok, err := getUnbondingDelegation(ctx.Store, delegator, validatorAddress)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("unbonding delegation not found")
	}
	return method.Outputs.Pack(record)
}

func (p *Precompile) delegatorDelegations(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	delegator := util.AddressString(args[0].(common.Address))
	nextKey := args[1].([]byte)
	validators, err := getStringList(ctx.Store, delegatorDelegationsIndexKey(delegator))
	if err != nil {
		return nil, err
	}
	page, outNextKey, err := pageStrings(validators, nextKey)
	if err != nil {
		return nil, err
	}
	result := DelegationsResponse{Delegations: make([]Delegation, 0, len(page)), NextKey: outNextKey}
	for _, validatorAddress := range page {
		record, ok, err := getDelegation(ctx.Store, delegator, validatorAddress)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		delegation, err := delegationFromRecord(record)
		if err != nil {
			return nil, err
		}
		result.Delegations = append(result.Delegations, delegation)
	}
	return method.Outputs.Pack(result)
}

func (p *Precompile) delegatorValidator(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	delegator := util.AddressString(args[0].(common.Address))
	validatorAddress := normalizeValidatorAddress(args[1].(string))
	if _, ok, err := getDelegation(ctx.Store, delegator, validatorAddress); err != nil {
		return nil, err
	} else if !ok {
		return nil, errors.New("delegation not found")
	}
	validator, ok, err := getValidator(ctx.Store, validatorAddress)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errValidatorMissing
	}
	return method.Outputs.Pack(validator)
}

func (p *Precompile) delegatorUnbondingDelegations(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	delegator := util.AddressString(args[0].(common.Address))
	nextKey := args[1].([]byte)
	validators, err := getStringList(ctx.Store, delegatorUnbondingsIndexKey(delegator))
	if err != nil {
		return nil, err
	}
	page, outNextKey, err := pageStrings(validators, nextKey)
	if err != nil {
		return nil, err
	}
	result := UnbondingDelegationsResponse{UnbondingDelegations: make([]UnbondingDelegation, 0, len(page)), NextKey: outNextKey}
	for _, validatorAddress := range page {
		record, ok, err := getUnbondingDelegation(ctx.Store, delegator, validatorAddress)
		if err != nil {
			return nil, err
		}
		if ok {
			result.UnbondingDelegations = append(result.UnbondingDelegations, record)
		}
	}
	return method.Outputs.Pack(result)
}

func (p *Precompile) redelegations(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 4); err != nil {
		return nil, err
	}
	delegatorFilter := normalizeValidatorAddress(args[0].(string))
	srcFilter := normalizeValidatorAddress(args[1].(string))
	dstFilter := normalizeValidatorAddress(args[2].(string))
	nextKey := args[3].([]byte)
	ids, err := getStringList(ctx.Store, redelegationsIndexKey())
	if err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		delegator, src, dst, ok := splitRedelegationID(id)
		if !ok {
			continue
		}
		if delegatorFilter != "" && delegatorFilter != delegator {
			continue
		}
		if srcFilter != "" && srcFilter != src {
			continue
		}
		if dstFilter != "" && dstFilter != dst {
			continue
		}
		filtered = append(filtered, id)
	}
	page, outNextKey, err := pageStrings(filtered, nextKey)
	if err != nil {
		return nil, err
	}
	result := RedelegationsResponse{Redelegations: make([]Redelegation, 0, len(page)), NextKey: outNextKey}
	for _, id := range page {
		delegator, src, dst, ok := splitRedelegationID(id)
		if !ok {
			continue
		}
		record, ok, err := getRedelegation(ctx.Store, delegator, src, dst)
		if err != nil {
			return nil, err
		}
		if ok {
			result.Redelegations = append(result.Redelegations, record)
		}
	}
	return method.Outputs.Pack(result)
}

func (p *Precompile) delegatorValidators(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	delegator := util.AddressString(args[0].(common.Address))
	nextKey := args[1].([]byte)
	validators, err := getStringList(ctx.Store, delegatorDelegationsIndexKey(delegator))
	if err != nil {
		return nil, err
	}
	page, outNextKey, err := pageStrings(validators, nextKey)
	if err != nil {
		return nil, err
	}
	result := ValidatorsResponse{Validators: make([]Validator, 0, len(page)), NextKey: outNextKey}
	for _, validatorAddress := range page {
		validator, ok, err := getValidator(ctx.Store, validatorAddress)
		if err != nil {
			return nil, err
		}
		if ok {
			result.Validators = append(result.Validators, validator)
		}
	}
	return method.Outputs.Pack(result)
}

func (p *Precompile) historicalInfo(ctx *precompiles.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	if err := util.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	height := args[0].(int64)
	info, ok, err := getHistoricalInfo(ctx.Store, height)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("historical info not found")
	}
	return method.Outputs.Pack(info)
}

func (p *Precompile) pool(ctx *precompiles.Context, method *abi.Method) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	pool, err := loadPool(ctx.Store)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(pool)
}

func (p *Precompile) params(ctx *precompiles.Context, method *abi.Method) ([]byte, error) {
	if err := util.ValidateNonPayable(ctx.ApparentValue); err != nil {
		return nil, err
	}
	params, err := loadParams(ctx.Store)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(params)
}
