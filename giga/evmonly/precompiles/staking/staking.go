// Package staking is the EVM-only staking precompile. Its ABI is Staking.sol
// and its state is docs/giga-storage/StakingStorage.sol, held as contract
// storage at Address. It applies its own copy of the params precompile's
// staking subspace.
//
// It serves delegate, redelegate and undelegate, and queries of validators,
// delegations, unbonding delegations, staged operations, the pool and its
// params. A delegate, redelegate or undelegate is staged when called and takes
// effect at the end of the epoch after the one it is called in. Delegations,
// the validator set and the params staking applies change only at an epoch's
// end, where EndBlock applies the staged operations due, updates the bonded
// set, completes matured validators, adopts the params precompile's staking
// params and snapshots the bonded set; every EndBlock completes matured
// unbondings and redelegations. Unlike x/staking, it never jails a validator,
// so an operator may hold less than its minimum self-delegation; a validator
// finishes unbonding at the first epoch's end after its unbonding time; a
// param change takes effect at the first epoch's end after it is made; its
// queues are first-in first-out, so an entry never completes before one queued
// ahead of it; and it does not slash unbonding or redelegation entries.
package staking

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

//go:embed abi.json
var abiJSON []byte

// ABI is the staking precompile's ABI.
var ABI = func() abi.ABI {
	parsed, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return parsed
}()

// Methods staking serves, by their names in ABI.
var (
	// Each is found by signature: ABI suffixes every overload of a name but the
	// first solc lists, and solc, not this package, picks that order.
	DelegateMethod             = methodNamed("delegate(address)")
	RedelegateMethod           = methodNamed("redelegate(address,address,uint256)")
	UndelegateMethod           = methodNamed("undelegate(address,uint256)")
	DelegationMethod           = methodNamed("delegation(address,address)")
	ValidatorMethod            = methodNamed("validator(address)")
	ValidatorsMethod           = methodNamed("validators(uint8,bytes)")
	ValidatorDelegationsMethod = methodNamed("validatorDelegations(address,bytes)")
	DelegatorDelegationsMethod = methodNamed("delegatorDelegations(address,bytes)")
	DelegatorValidatorMethod   = methodNamed("delegatorValidator(address,address)")
	DelegatorValidatorsMethod  = methodNamed("delegatorValidators(address,bytes)")
	UnbondingDelegationMethod  = methodNamed("unbondingDelegation(address,address)")
	StagedOperationsMethod     = methodNamed("stagedOperations(address,bytes)")
	PoolMethod                 = methodNamed("pool()")
	ParamsMethod               = methodNamed("params()")
)

// methodNamed returns the name ABI gives the method with signature sig.
func methodNamed(sig string) string {
	for name, m := range ABI.Methods {
		if m.Sig == sig {
			return name
		}
	}
	panic("staking ABI has no method " + sig)
}

// Events.
const (
	DelegateEvent   = "Delegate"
	RedelegateEvent = "Redelegate"
	UndelegateEvent = "Undelegate"
)

const authzUnsupported = "staking authorizations are not supported"

// retiredMethods are the V2 methods, by signature, staking keeps in its ABI
// only to revert with a reason naming the call to use instead. They are checked
// before dispatch.
var retiredMethods = map[string]string{
	"delegate(string)":                                           "delegate(string) is not supported; use delegate(address) with the validator's operator address",
	"redelegate(string,string,uint256)":                          "redelegate(string,string,uint256) is not supported; use redelegate(address,address,uint256) with the validators' operator addresses and an amount in wei",
	"undelegate(string,uint256)":                                 "undelegate(string,uint256) is not supported; use undelegate(address,uint256) with the validator's operator address and an amount in wei",
	"grantStakingAuthorization(address,string[],uint256,int64)":  authzUnsupported,
	"delegateWithAuthorization(address,string)":                  authzUnsupported,
	"redelegateWithAuthorization(address,string,string,uint256)": authzUnsupported,
	"undelegateWithAuthorization(address,string,uint256)":        authzUnsupported,
	"revokeStakingAuthorization(address)":                        authzUnsupported,
	"delegation(address,string)":                                 "delegation(address,string) is not supported; use delegation(address,address) with the validator's operator address",
	"validator(string)":                                          "validator(string) is not supported; use validator(address) with the validator's operator address",
	"validators(string,bytes)":                                   "validators(string,bytes) is not supported; use validators(uint8,bytes) with status 0 for any, 1 for Unbonded, 2 for Unbonding or 3 for Bonded",
	"validatorDelegations(string,bytes)":                         "validatorDelegations(string,bytes) is not supported; use validatorDelegations(address,bytes) with the validator's operator address",
	"unbondingDelegation(address,string)":                        "unbondingDelegation(address,string) is not supported; use unbondingDelegation(address,address) with the validator's operator address",
	"delegatorValidator(address,string)":                         "delegatorValidator(address,string) is not supported; use delegatorValidator(address,address) with the validator's operator address",
	"historicalInfo(int64)":                                      "historicalInfo(int64) is not supported; staking keeps no historical info",
}

// Flat per-call gas, as V2's pcommon.DefaultGasCost prices it from the
// Cosmos KV gas config, charged on the arguments after the method selector.
const (
	unknownMethodGas   = 3_000
	transactionGas     = 2_000
	transactionByteGas = 30
	queryGas           = 1_000
	queryByteGas       = 3
)

var (
	errDelegateCall  = errors.New("cannot delegatecall staking")
	errStaticCall    = fmt.Errorf("cannot call staking precompile from staticcall: %w", vm.ErrWriteProtection)
	errNonPayable    = errors.New("sending funds to a non-payable function")
	errUnknownMethod = errors.New("unknown staking method")
	errInvalidAmount = errors.New("amount must be a positive whole number of usei, in wei")
	errNotStarted    = errors.New("staking starts once an EndBlock has bonded its first validators")
)

// Precompile is the staking precompile. Hooks are called as x/staking calls its
// StakingHooks; with none, nothing else tracks delegations.
type Precompile struct {
	Hooks Hooks
	// hookFailures, when set, collects the hook failures EndBlock reports in
	// place of failing the operation that called the hook.
	hookFailures *[]error
}

var (
	_ precompiles.Contract   = Precompile{}
	_ precompiles.EndBlocker = Precompile{}
)

func (Precompile) RequiredGas(input []byte) uint64 {
	method, err := methodOf(input)
	if err != nil {
		return unknownMethodGas
	}
	args := uint64(len(input) - 4) //nolint:gosec // methodOf requires a 4-byte selector
	if method.IsConstant() {
		return queryGas + queryByteGas*args
	}
	return transactionGas + transactionByteGas*args
}

func (p Precompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	// CALLCODE also arrives as DelegateCall, with a value no transfer backed;
	// refusing it keeps a call from delegating staking's own balance.
	if ctx.DelegateCall {
		return nil, errDelegateCall
	}
	method, err := methodOf(input)
	if err != nil {
		return nil, err
	}
	if reason, ok := retiredMethods[method.Sig]; ok {
		return precompiles.Revert(reason)
	}
	if ctx.ReadOnly && !method.IsConstant() {
		return nil, errStaticCall
	}
	args, err := method.Inputs.Unpack(input[4:])
	if err != nil {
		return nil, err
	}
	if !method.IsPayable() && ctx.ApparentValue != nil && ctx.ApparentValue.Sign() != 0 {
		return nil, errNonPayable
	}
	if check, ok := argChecks[method.Name]; ok {
		if err := check(ctx, args); err != nil {
			return precompiles.Revert(err.Error())
		}
	}
	s := newStore(ctx.State)

	switch method.Name {
	case DelegationMethod:
		return delegationQuery(s, method, args)
	case ValidatorMethod:
		return validatorQuery(s, method, args)
	case ValidatorsMethod:
		return validatorsQuery(s, method, args)
	case ValidatorDelegationsMethod:
		return validatorDelegationsQuery(s, method, args)
	case DelegatorDelegationsMethod:
		return delegatorDelegationsQuery(s, method, args)
	case DelegatorValidatorMethod:
		return delegatorValidatorQuery(s, method, args)
	case DelegatorValidatorsMethod:
		return delegatorValidatorsQuery(s, method, args)
	case UnbondingDelegationMethod:
		return unbondingDelegationQuery(s, method, args)
	case StagedOperationsMethod:
		return stagedOperationsQuery(s, method, args)
	case PoolMethod:
		return poolQuery(s, method)
	case ParamsMethod:
		return paramsQuery(s, method)
	}

	// x/staking bonds its genesis validators before the first block runs, and
	// here the first EndBlock does. Until then the voting power ratio would be
	// checked against no bonded power.
	if !chainparams.HasStaking(ctx.State) || !s.started() {
		return nil, errNotStarted
	}

	switch method.Name {
	case DelegateMethod:
		return p.delegateCall(ctx, s, method, args)
	case RedelegateMethod:
		return p.redelegateCall(ctx, s, method, args)
	case UndelegateMethod:
		return p.undelegateCall(ctx, s, method, args)
	}
	return nil, errUnknownMethod
}

func methodOf(input []byte) (*abi.Method, error) {
	if len(input) < 4 {
		return nil, errUnknownMethod
	}
	method, err := ABI.MethodById(input[:4])
	if err != nil {
		return nil, errUnknownMethod
	}
	return method, nil
}

// argChecks range-check each method's arguments, and delegate's value, before
// it reads state. A failure reverts the call with the error's message.
var argChecks = map[string]func(ctx *precompiles.Context, args []interface{}) error{
	DelegateMethod:   func(ctx *precompiles.Context, _ []interface{}) error { return checkAmount(ctx.ApparentValue) },
	RedelegateMethod: func(_ *precompiles.Context, args []interface{}) error { return checkAmount(args[2].(*big.Int)) },
	UndelegateMethod: func(_ *precompiles.Context, args []interface{}) error { return checkAmount(args[1].(*big.Int)) },
	ValidatorsMethod: func(_ *precompiles.Context, args []interface{}) error {
		if err := checkStatusFilter(args[0].(uint8)); err != nil {
			return err
		}
		return checkPageKey(args[1].([]byte))
	},
	ValidatorDelegationsMethod: func(_ *precompiles.Context, args []interface{}) error { return checkPageKey(args[1].([]byte)) },
	DelegatorDelegationsMethod: func(_ *precompiles.Context, args []interface{}) error { return checkPageKey(args[1].([]byte)) },
	DelegatorValidatorsMethod:  func(_ *precompiles.Context, args []interface{}) error { return checkPageKey(args[1].([]byte)) },
	StagedOperationsMethod:     func(_ *precompiles.Context, args []interface{}) error { return checkPageKey(args[1].([]byte)) },
}

// checkAmount requires a positive whole number of usei.
func checkAmount(amount *big.Int) error {
	if amount == nil || amount.Sign() <= 0 || new(big.Int).Mod(amount, weiPerUsei).Sign() != 0 {
		return fmt.Errorf("%w: %s", errInvalidAmount, amount)
	}
	return nil
}

// checkStatusFilter requires 0, for any status, or a validator status.
func checkStatusFilter(status uint8) error {
	if status > statusBonded {
		return fmt.Errorf("%w: %d", errInvalidStatus, status)
	}
	return nil
}
