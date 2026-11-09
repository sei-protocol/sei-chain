// Package staking is the EVM-only staking precompile. Its ABI is Staking.sol
// and its state is docs/giga-storage/StakingStorage.sol, held as contract
// storage at Address. Its params are the params precompile's staking subspace.
//
// It serves delegate, redelegate and undelegate, and its EndBlock updates the
// bonded set and completes matured unbondings and redelegations. Unlike
// x/staking, it never jails a validator, so an operator may hold less than its
// minimum self-delegation; its queues are first-in first-out, so an entry never
// completes before one queued ahead of it; and it does not slash unbonding or
// redelegation entries.
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

const (
	DelegateMethod   = "delegate"
	RedelegateMethod = "redelegate"
	UndelegateMethod = "undelegate"
)

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
}

// Flat per-call gas, as V2's pcommon.DefaultGasCost prices it from the
// Cosmos KV gas config, charged on the arguments after the method selector.
const (
	unknownMethodGas   = 3_000
	transactionGas     = 2_000
	transactionByteGas = 30
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
}

var (
	_ precompiles.Contract   = Precompile{}
	_ precompiles.EndBlocker = Precompile{}
)

func (Precompile) RequiredGas(input []byte) uint64 {
	if _, err := methodOf(input); err != nil {
		return unknownMethodGas
	}
	return transactionGas + transactionByteGas*uint64(len(input)-4) //nolint:gosec // methodOf requires a 4-byte selector
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
	if ctx.ReadOnly {
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
	// x/staking bonds its genesis validators before the first block runs, and
	// here the first EndBlock does. Until then the voting power ratio would be
	// checked against no bonded power.
	if !chainparams.HasStaking(ctx.State) || s.lastTotalPower().Sign() == 0 {
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
}

// checkAmount requires a positive whole number of usei.
func checkAmount(amount *big.Int) error {
	if amount == nil || amount.Sign() <= 0 || new(big.Int).Mod(amount, weiPerUsei).Sign() != 0 {
		return fmt.Errorf("%w: %s", errInvalidAmount, amount)
	}
	return nil
}
