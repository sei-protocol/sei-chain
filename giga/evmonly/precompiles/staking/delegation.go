package staking

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
)

var (
	errNoValidatorFound         = errors.New("validator does not exist")
	errNoDelegation             = errors.New("no delegation for (address, validator) tuple")
	errNotEnoughShares          = errors.New("not enough delegation shares")
	errInvalidShares            = errors.New("invalid shares amount")
	errExRateInvalid            = errors.New("cannot delegate to validators with invalid (zero) ex-rate")
	errExceedMaxVotingPower     = errors.New("validator's voting power ratio would exceed the max allowed ratio")
	errMaxUnbondingEntries      = errors.New("too many unbonding delegation entries for (delegator, validator) tuple")
	errMaxRedelegationEntries   = errors.New("too many redelegation entries for (delegator, src-validator, dst-validator) tuple")
	errSelfRedelegation         = errors.New("cannot redelegate to the same validator")
	errBadRedelegationValidator = errors.New("redelegation validator does not exist")
	errTransitiveRedelegation   = errors.New("redelegation to this validator already in progress; first redelegation to this validator must complete before next redelegation")
	errTinyRedelegationAmount   = errors.New("too few tokens to redelegate (truncates to zero tokens)")
	errEscrowShort              = errors.New("escrow holds less than the transfer")
	errBalanceOverflow          = errors.New("transfer would overflow the recipient's balance")
	errInvalidTransition        = errors.New("invalid validator status transition")
	errInvalidStatus            = errors.New("invalid validator status")
)

// delegateCall stages delegating the call's value to a validator, holding the
// value in StagedEscrow until it takes effect.
func (p Precompile) delegateCall(ctx *precompiles.Context, s store, method *abi.Method, args []interface{}) ([]byte, error) {
	op := args[0].(common.Address)
	v, ok := s.validator(op)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNoValidatorFound, op)
	}
	amount := new(big.Int).Set(ctx.ApparentValue)
	if err := checkDelegation(ctx.Block, s.lastTotalPower(), s.params(), &v, amount); err != nil {
		return nil, err
	}
	if err := transfer(s.State, ctx.Address, StagedEscrow, amount); err != nil {
		return nil, err
	}
	epoch, err := stage(s, stagedOperation{kind: stagedDelegate, delegator: ctx.Caller, validator: op, amount: amount})
	if err != nil {
		return nil, err
	}
	if err := emit(ctx, DelegateEvent, []common.Hash{addressTopic(ctx.Caller), addressTopic(op)}, amount, epoch); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

// undelegateCall stages undelegating shares of the caller's delegation.
func (p Precompile) undelegateCall(ctx *precompiles.Context, s store, method *abi.Method, args []interface{}) ([]byte, error) {
	op, amount := args[0].(common.Address), args[1].(*big.Int)
	v, shares, err := validateUnbondAmount(s, ctx.Caller, op, amount)
	if err != nil {
		return nil, err
	}
	if err := checkUnbondingEntries(s, s.params(), ctx.Caller, op, s.stagedDelegation(ctx.Caller, op)); err != nil {
		return nil, err
	}
	epoch, err := stage(s, stagedOperation{kind: stagedUndelegate, delegator: ctx.Caller, validator: op, amount: shares})
	if err != nil {
		return nil, err
	}
	if err := emit(ctx, UndelegateEvent, []common.Hash{addressTopic(ctx.Caller), addressTopic(op)}, v.unbondedTokens(shares), epoch); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

// redelegateCall stages moving shares of the caller's delegation to another
// validator.
func (p Precompile) redelegateCall(ctx *precompiles.Context, s store, method *abi.Method, args []interface{}) ([]byte, error) {
	src, dst, amount := args[0].(common.Address), args[1].(common.Address), args[2].(*big.Int)
	srcValidator, shares, err := validateUnbondAmount(s, ctx.Caller, src, amount)
	if err != nil {
		return nil, err
	}
	params := s.params()
	_, dstValidator, err := checkRedelegation(s, params, ctx.Caller, src, dst, s.stagedDelegation(ctx.Caller, src))
	if err != nil {
		return nil, err
	}
	tokens := srcValidator.unbondedTokens(shares)
	if tokens.Sign() == 0 {
		return nil, errTinyRedelegationAmount
	}
	if err := checkDelegation(ctx.Block, s.lastTotalPower(), params, &dstValidator, tokens); err != nil {
		return nil, err
	}
	epoch, err := stage(s, stagedOperation{kind: stagedRedelegate, delegator: ctx.Caller, validator: src, dstValidator: dst, amount: shares})
	if err != nil {
		return nil, err
	}
	if err := emit(ctx, RedelegateEvent, []common.Hash{addressTopic(ctx.Caller), addressTopic(src), addressTopic(dst)}, tokens, epoch); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

// delegate adds amount to delegator's delegation to op's validator v and
// returns the shares it issues, as x/staking's Delegate does, checking the
// voting power ratio against bondedPower. The tokens come from the balance of
// from, or, when from is the zero address, from the escrow of a validator whose
// status was tokenSrc.
func (p Precompile) delegate(
	block precompiles.BlockContext, s store, params chainparams.Staking, bondedPower *big.Int,
	delegator, op common.Address, v *validator, amount *big.Int, tokenSrc uint8, from common.Address,
) (*big.Int, error) {
	if err := checkDelegation(block, bondedPower, params, v, amount); err != nil {
		return nil, err
	}
	shares, found := s.delegation(delegator, op)
	hook := p.hooks().BeforeDelegationCreated
	if found {
		hook = p.hooks().BeforeDelegationSharesModified
	}
	if err := hook(block, s.State, delegator, op); err != nil {
		return nil, err
	}

	dstEscrow, err := escrowOf(v.status)
	if err != nil {
		return nil, err
	}
	if from == (common.Address{}) {
		if from, err = escrowOf(tokenSrc); err != nil {
			return nil, err
		}
	}
	if err := transfer(s.State, from, dstEscrow, amount); err != nil {
		return nil, err
	}

	issued, err := s.addTokensFromDelegation(op, v, amount)
	if err != nil {
		return nil, err
	}
	s.setDelegation(delegator, op, new(big.Int).Add(shares, issued))
	return issued, p.hooks().AfterDelegationModified(block, s.State, delegator, op)
}

// checkDelegation fails when amount may not be delegated to v, as x/staking's
// Delegate checks before it moves any tokens, with bondedPower the bonded set's
// power.
func checkDelegation(block precompiles.BlockContext, bondedPower *big.Int, params chainparams.Staking, v *validator, amount *big.Int) error {
	if v.invalidExRate() {
		return errExRateInvalid
	}
	return checkVotingPowerRatio(block, bondedPower, params, v, amount)
}

// checkVotingPowerRatio fails when delegating amount to v would take v past the
// max voting power ratio of bondedPower, the bonded set's power, once that
// power reaches the enforcement threshold, as x/staking's Delegate checks.
func checkVotingPowerRatio(block precompiles.BlockContext, bondedPower *big.Int, params chainparams.Staking, v *validator, amount *big.Int) error {
	newTotalPower := new(big.Int).Add(bondedPower, consensusPower(amount))
	if block.Number == 0 || newTotalPower.Sign() == 0 || newTotalPower.Cmp(params.MaxVotingPowerEnforcementThreshold) < 0 {
		return nil
	}
	validatorPower := consensusPower(new(big.Int).Add(v.tokens, amount))
	ratio := dec.Quo(dec.FromInt(validatorPower), dec.FromInt(newTotalPower))
	if ratio.Cmp(params.MaxVotingPowerRatio) > 0 {
		return fmt.Errorf("%w: %s, max %s", errExceedMaxVotingPower, ratio, params.MaxVotingPowerRatio)
	}
	return nil
}

// validateUnbondAmount returns op's validator and the shares of delegator's
// delegation to op that amount of tokens is worth, as x/staking's
// ValidateUnbondAmount does, failing when they are more than the delegation
// holds beyond the shares it has staged to take away.
func validateUnbondAmount(s store, delegator, op common.Address, amount *big.Int) (validator, *big.Int, error) {
	v, ok := s.validator(op)
	if !ok {
		return validator{}, nil, fmt.Errorf("%w: %s", errNoValidatorFound, op)
	}
	delegated, ok := s.delegation(delegator, op)
	if !ok {
		return validator{}, nil, errNoDelegation
	}
	shares, err := v.sharesFromTokens(amount)
	if err != nil {
		return validator{}, nil, err
	}
	if shares.Cmp(new(big.Int).Sub(delegated, s.stagedDelegation(delegator, op).sharesOut)) > 0 {
		return validator{}, nil, errInvalidShares
	}
	return v, shares, nil
}

// checkUnbondingEntries fails when delegator's unbonding from op holds
// MaxEntries entries, counting the undelegations staged, as x/staking's
// Undelegate checks.
func checkUnbondingEntries(s store, params chainparams.Staking, delegator, op common.Address, staged stagedDelegation) error {
	if s.unbondingEntryCount(delegator, op)+staged.undelegations >= uint64(params.MaxEntries) {
		return errMaxUnbondingEntries
	}
	return nil
}

// checkRedelegation fails as x/staking's BeginRedelegation does before it moves
// any stake, counting the redelegations staged into and out of src, and returns
// src's and dst's validators.
func checkRedelegation(
	s store, params chainparams.Staking, delegator, src, dst common.Address, staged stagedDelegation,
) (srcValidator, dstValidator validator, err error) {
	if src == dst {
		return validator{}, validator{}, errSelfRedelegation
	}
	dstValidator, ok := s.validator(dst)
	if !ok {
		return validator{}, validator{}, fmt.Errorf("%w: %s", errBadRedelegationValidator, dst)
	}
	srcValidator, ok = s.validator(src)
	if !ok {
		return validator{}, validator{}, fmt.Errorf("%w: %s", errBadRedelegationValidator, src)
	}
	if s.receivingRedelegations(src, delegator).Sign() > 0 || staged.redelegationsIn > 0 {
		return validator{}, validator{}, errTransitiveRedelegation
	}
	entries := new(big.Int).Add(s.redelegationEntryCount(delegator, src, dst), new(big.Int).SetUint64(staged.redelegationsOut))
	if entries.Cmp(new(big.Int).SetUint64(uint64(params.MaxEntries))) >= 0 {
		return validator{}, validator{}, errMaxRedelegationEntries
	}
	return srcValidator, dstValidator, nil
}

// unbond takes shares from delegator's delegation to op and returns the tokens
// they were worth, as x/staking's Unbond does, except that an operator whose
// self-delegation falls below its minimum is not jailed. The tokens stay in
// op's escrow. A validator it leaves vacant is removed.
func (p Precompile) unbond(block precompiles.BlockContext, s store, delegator, op common.Address, shares *big.Int) (*big.Int, error) {
	delegated, ok := s.delegation(delegator, op)
	if !ok {
		return nil, errNoDelegation
	}
	if err := p.hooks().BeforeDelegationSharesModified(block, s.State, delegator, op); err != nil {
		return nil, err
	}
	if delegated.Cmp(shares) < 0 {
		return nil, fmt.Errorf("%w: %s", errNotEnoughShares, delegated)
	}
	v, ok := s.validator(op)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNoValidatorFound, op)
	}

	remaining := new(big.Int).Sub(delegated, shares)
	if remaining.Sign() == 0 {
		if err := p.hooks().BeforeDelegationRemoved(block, s.State, delegator, op); err != nil {
			return nil, err
		}
		s.removeDelegation(delegator, op)
	} else {
		s.setDelegation(delegator, op, remaining)
		if err := p.hooks().AfterDelegationModified(block, s.State, delegator, op); err != nil {
			return nil, err
		}
	}

	amount, err := s.removeDelegationShares(op, &v, shares)
	if err != nil {
		return nil, err
	}
	if err := p.removeIfVacant(block, s, op, v); err != nil {
		return nil, err
	}
	return amount, nil
}

// undelegate unbonds shares of delegator's delegation to op into a new
// unbonding entry, paid one unbonding period from now, as x/staking's
// Undelegate does. It returns the entry's balance and completion time.
func (p Precompile) undelegate(
	block precompiles.BlockContext, s store, params chainparams.Staking, delegator, op common.Address, shares *big.Int,
) (*big.Int, uint64, error) {
	v, ok := s.validator(op)
	if !ok {
		return nil, 0, fmt.Errorf("%w: %s", errNoValidatorFound, op)
	}
	if err := checkUnbondingEntries(s, params, delegator, op, stagedDelegation{}); err != nil {
		return nil, 0, err
	}
	amount, err := p.unbond(block, s, delegator, op, shares)
	if err != nil {
		return nil, 0, err
	}
	if v.bonded() {
		if err := transfer(s.State, BondedEscrow, NotBondedEscrow, amount); err != nil {
			return nil, 0, err
		}
	}

	q := s.unbondingQueue()
	completionTime := q.clamp(block.UnixNano + params.UnbondingTime)
	s.appendUnbondingEntry(delegator, op, unbondingEntry{
		creationHeight: block.Number,
		completionTime: completionTime,
		initialBalance: amount,
		balance:        amount,
	})
	e, err := q.push(completionTime)
	if err != nil {
		return nil, 0, err
	}
	s.SetAddressAt(at(e, unbondingQueueDelegator), unbondingQueueDelegator.offset, delegator)
	s.SetAddressAt(at(e, unbondingQueueValidator), unbondingQueueValidator.offset, op)
	return amount, completionTime, nil
}

// redelegate moves shares of delegator's delegation to src into a delegation
// to dst, as x/staking's BeginRedelegation does, checking the voting power
// ratio against bondedPower, and queues the redelegation entry unless src is
// unbonded and still exists once the shares leave it. It returns the tokens
// moved and the entry's completion time, or 0 for no entry.
func (p Precompile) redelegate(
	block precompiles.BlockContext, s store, params chainparams.Staking, bondedPower *big.Int, delegator, src, dst common.Address, shares *big.Int,
) (*big.Int, uint64, error) {
	srcValidator, dstValidator, err := checkRedelegation(s, params, delegator, src, dst, stagedDelegation{})
	if err != nil {
		return nil, 0, err
	}
	amount, err := p.unbond(block, s, delegator, src, shares)
	if err != nil {
		return nil, 0, err
	}
	if amount.Sign() == 0 {
		return nil, 0, errTinyRedelegationAmount
	}
	sharesDst, err := p.delegate(block, s, params, bondedPower, delegator, dst, &dstValidator, amount, srcValidator.status, common.Address{})
	if err != nil {
		return nil, 0, err
	}

	// Read after unbond, as x/staking does: taking the last shares out of an
	// unbonded src removes it, and a missing src waits a full unbonding period.
	completionTime, creationHeight, completeNow := redelegationBeginInfo(block, s, params, src)
	if completeNow {
		return amount, 0, nil
	}
	q := s.redelegationQueue()
	completionTime = q.clamp(completionTime)
	e, err := q.push(completionTime)
	if err != nil {
		return nil, 0, err
	}
	s.SetAddressAt(at(e, redelegationQueueDelegator), redelegationQueueDelegator.offset, delegator)
	s.SetUint64(at(e, redelegationQueueCreationHeight), redelegationQueueCreationHeight.offset, creationHeight)
	s.SetAddressAt(at(e, redelegationQueueSrcValidator), redelegationQueueSrcValidator.offset, src)
	s.SetAddressAt(at(e, redelegationQueueDstValidator), redelegationQueueDstValidator.offset, dst)
	s.SetUint256(at(e, redelegationQueueInitialBalance), amount)
	s.SetUint256(at(e, redelegationQueueSharesDst), sharesDst)
	entries := s.redelegationEntryCount(delegator, src, dst)
	s.setRedelegationEntryCount(delegator, src, dst, entries.Add(entries, big.NewInt(1)))
	received := s.receivingRedelegations(dst, delegator)
	s.setReceivingRedelegations(dst, delegator, received.Add(received, big.NewInt(1)))
	return amount, completionTime, nil
}

// redelegationBeginInfo returns when a redelegation out of src completes and
// the height it counts from, as x/staking's getBeginInfo does: one unbonding
// period from now when src is bonded or gone, src's own unbonding end when it
// is unbonding, and now, with completeNow, when it is unbonded.
func redelegationBeginInfo(block precompiles.BlockContext, s store, params chainparams.Staking, src common.Address) (completionTime, height uint64, completeNow bool) {
	v, ok := s.validator(src)
	switch {
	case !ok || v.status == statusBonded:
		return block.UnixNano + params.UnbondingTime, block.Number, false
	case v.status == statusUnbonding:
		return v.unbondingTime, v.unbondingHeight, false
	default:
		return 0, 0, true
	}
}

// escrowOf returns the escrow holding the tokens of a validator with status.
func escrowOf(status uint8) (common.Address, error) {
	switch status {
	case statusBonded:
		return BondedEscrow, nil
	case statusUnbonded, statusUnbonding:
		return NotBondedEscrow, nil
	}
	return common.Address{}, fmt.Errorf("%w: %d", errInvalidStatus, status)
}

// transfer moves amount from one balance to another, failing without moving
// anything when from holds less than amount or to would overflow. A transfer
// between the same address moves nothing.
func transfer(state precompiles.State, from, to common.Address, amount *big.Int) error {
	if from == to || amount.Sign() == 0 {
		return nil
	}
	if state.GetBalance(from).Cmp(amount) < 0 {
		return fmt.Errorf("%w: %s holds less than %s", errEscrowShort, from, amount)
	}
	if new(big.Int).Add(state.GetBalance(to), amount).BitLen() > 256 {
		return fmt.Errorf("%w: %s", errBalanceOverflow, to)
	}
	if err := state.SubBalance(from, amount); err != nil {
		return err
	}
	state.AddBalance(to, amount)
	return nil
}

// emit logs event name with the indexed topics after its signature and data as
// its non-indexed fields.
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
