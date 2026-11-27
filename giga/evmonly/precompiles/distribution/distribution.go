// Package distribution is the EVM-only distribution precompile. Its ABI is
// Distribution.sol and its state is docs/giga-storage/DistributionStorage.sol,
// held as contract storage at Address. Its params are the params precompile's
// distribution subspace.
//
// It keeps x/distribution's F1 records of the rewards each delegation is owed,
// and holds the rewards in DistributionEscrow until they are paid. Hooks keeps
// the records in step with staking: a delegation's rewards are paid to its
// withdraw address whenever staking changes the delegation. It serves
// withdrawals of delegation rewards and validator commission, which pay at
// once, a delegator's withdraw address, and queries of rewards, commission and
// its params. Unlike x/distribution, amounts are whole wei, and what
// x/distribution adds to the community pool goes to the DAO.
package distribution

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

//go:embed abi.json
var abiJSON []byte

// ABI is the distribution precompile's ABI.
var ABI = func() abi.ABI {
	parsed, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
	return parsed
}()

// Methods distribution serves, by their names in ABI.
var (
	// Each is found by signature: ABI suffixes every overload of a name but the
	// first solc lists, and solc, not this package, picks that order.
	SetWithdrawAddressMethod                = methodNamed("setWithdrawAddress(address)")
	WithdrawDelegationRewardsMethod         = methodNamed("withdrawDelegationRewards(address)")
	WithdrawMultipleDelegationRewardsMethod = methodNamed("withdrawMultipleDelegationRewards(address[])")
	WithdrawValidatorCommissionMethod       = methodNamed("withdrawValidatorCommission()")
	RewardsMethod                           = methodNamed("rewards(address)")
	DelegationRewardsMethod                 = methodNamed("delegationRewards(address,address)")
	ValidatorOutstandingRewardsMethod       = methodNamed("validatorOutstandingRewards(address)")
	ValidatorCommissionMethod               = methodNamed("validatorCommission(address)")
	DelegatorWithdrawAddressMethod          = methodNamed("delegatorWithdrawAddress(address)")
	ParamsMethod                            = methodNamed("params()")
)

// methodNamed returns the name ABI gives the method with signature sig.
func methodNamed(sig string) string {
	for name, m := range ABI.Methods {
		if m.Sig == sig {
			return name
		}
	}
	panic("distribution ABI has no method " + sig)
}

// Events.
const (
	WithdrawAddressSetEvent           = "WithdrawAddressSet"
	DelegationRewardsWithdrawnEvent   = "DelegationRewardsWithdrawn"
	ValidatorCommissionWithdrawnEvent = "ValidatorCommissionWithdrawn"
)

// retiredMethods are the V2 methods, by signature, distribution keeps in its
// ABI only to revert with a reason naming the call to use instead. They are
// checked before dispatch.
var retiredMethods = map[string]string{
	"withdrawDelegationRewards(string)":            "withdrawDelegationRewards(string) is not supported; use withdrawDelegationRewards(address) with the validator's operator address",
	"withdrawMultipleDelegationRewards(string[])":  "withdrawMultipleDelegationRewards(string[]) is not supported; use withdrawMultipleDelegationRewards(address[]) with the validators' operator addresses",
	"validatorOutstandingRewards(string)":          "validatorOutstandingRewards(string) is not supported; use validatorOutstandingRewards(address) with the validator's operator address",
	"validatorCommission(string)":                  "validatorCommission(string) is not supported; use validatorCommission(address) with the validator's operator address",
	"validatorSlashes(string,uint64,uint64,bytes)": "validatorSlashes is not supported; slash events are not served",
	"delegationRewards(address,string)":            "delegationRewards(address,string) is not supported; use delegationRewards(address,address) with the validator's operator address",
	"delegatorValidators(address)":                 "delegatorValidators(address) is not supported; use the staking precompile's delegatorValidators(address,bytes)",
	"communityPool()":                              "communityPool() is not supported; the community pool is retired, and what it would hold goes to the DAO",
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
	errDelegateCall  = errors.New("cannot delegatecall distribution")
	errStaticCall    = fmt.Errorf("cannot call distribution precompile from staticcall: %w", vm.ErrWriteProtection)
	errNonPayable    = errors.New("sending funds to a non-payable function")
	errUnknownMethod = errors.New("unknown distribution method")
	errZeroAddress   = errors.New("address must not be the zero address")
	errNoValidators  = errors.New("validators must not be empty")
	errRepeated      = errors.New("validators must not repeat")
)

// Precompile is the distribution precompile.
type Precompile struct{}

var _ precompiles.Contract = Precompile{}

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

func (Precompile) Run(ctx *precompiles.Context, input []byte) ([]byte, error) {
	// CALLCODE also arrives as DelegateCall, with a value no transfer backed;
	// refusing it keeps a call from paying out of distribution's balance.
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
	if ctx.ApparentValue != nil && ctx.ApparentValue.Sign() != 0 {
		return nil, errNonPayable
	}
	if check, ok := argChecks[method.Name]; ok {
		if err := check(args); err != nil {
			return precompiles.Revert(err.Error())
		}
	}
	s := newStore(ctx.State)

	switch method.Name {
	case RewardsMethod:
		return rewardsQuery(ctx.Block, s, method, args)
	case DelegationRewardsMethod:
		return delegationRewardsQuery(ctx.Block, s, method, args)
	case ValidatorOutstandingRewardsMethod:
		return method.Outputs.Pack(s.outstandingRewards(args[0].(common.Address)))
	case ValidatorCommissionMethod:
		return method.Outputs.Pack(s.accumulatedCommission(args[0].(common.Address)))
	case DelegatorWithdrawAddressMethod:
		return method.Outputs.Pack(s.withdrawAddress(args[0].(common.Address)))
	case ParamsMethod:
		return paramsQuery(s, method)
	case SetWithdrawAddressMethod:
		return setWithdrawAddressCall(ctx, s, method, args)
	case WithdrawDelegationRewardsMethod:
		return withdrawDelegationRewardsCall(ctx, s, method, []common.Address{args[0].(common.Address)})
	case WithdrawMultipleDelegationRewardsMethod:
		return withdrawDelegationRewardsCall(ctx, s, method, args[0].([]common.Address))
	case WithdrawValidatorCommissionMethod:
		return withdrawValidatorCommissionCall(ctx, s, method)
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

// argChecks range-check each method's arguments before it reads state. A
// failure reverts the call with the error's message.
var argChecks = map[string]func(args []interface{}) error{
	SetWithdrawAddressMethod:                func(args []interface{}) error { return checkAddress("withdrawAddr", args[0].(common.Address)) },
	WithdrawDelegationRewardsMethod:         func(args []interface{}) error { return checkAddress("validator", args[0].(common.Address)) },
	WithdrawMultipleDelegationRewardsMethod: func(args []interface{}) error { return checkValidators(args[0].([]common.Address)) },
}

// checkAddress requires the argument named name to be nonzero.
func checkAddress(name string, a common.Address) error {
	if a == (common.Address{}) {
		return fmt.Errorf("%s: %w", name, errZeroAddress)
	}
	return nil
}

// checkValidators requires a nonempty list of nonzero addresses, none
// repeated.
func checkValidators(validators []common.Address) error {
	if len(validators) == 0 {
		return errNoValidators
	}
	seen := make(map[common.Address]bool, len(validators))
	for _, v := range validators {
		if err := checkAddress("validators", v); err != nil {
			return err
		}
		if seen[v] {
			return fmt.Errorf("%w: %s", errRepeated, v)
		}
		seen[v] = true
	}
	return nil
}

func emit(ctx *precompiles.Context, name string, indexed []common.Hash, data ...interface{}) error {
	event := ABI.Events[name]
	bz, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		return err
	}
	ctx.Logs.AddLog(&ethtypes.Log{Address: ctx.Address, Topics: append([]common.Hash{event.ID}, indexed...), Data: bz})
	return nil
}

func addressTopic(a common.Address) common.Hash {
	return common.BytesToHash(a[:])
}
