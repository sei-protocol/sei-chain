package staking

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// powerIndex is StakingStorage.validatorsByPower: every unjailed validator,
// highest consensus power first, ties to the lower address, as x/staking's
// power index iterates. Each operation takes the indexed power of the
// validator it moves, so it never reads that validator's tokens.
type powerIndex struct {
	s store
}

func (s store) powerIndex() powerIndex {
	return powerIndex{s}
}

func (p powerIndex) base() common.Hash {
	return solstorage.Slot(slotValidatorsByPower)
}

func (p powerIndex) len() uint64 {
	return p.s.Uint64(p.base(), 0)
}

// at returns the operator at index i, which must be below len.
func (p powerIndex) at(i uint64) common.Address {
	return p.s.AddressAt(solstorage.Offset(solstorage.ArrayData(p.base()), i), 0)
}

func (p powerIndex) set(i uint64, op common.Address) {
	p.s.SetWord(solstorage.Offset(solstorage.ArrayData(p.base()), i), solstorage.AddressKey(op))
}

// ranksBefore reports whether a validator of power pa at a comes before one of
// power pb at b.
func ranksBefore(pa *big.Int, a common.Address, pb *big.Int, b common.Address) bool {
	if c := pa.Cmp(pb); c != 0 {
		return c > 0
	}
	return bytes.Compare(a[:], b[:]) < 0
}

// search returns the first index whose validator does not rank before op at
// power, reading each probed validator's power from its tokens.
func (p powerIndex) search(op common.Address, power *big.Int) uint64 {
	lo, hi := uint64(0), p.len()
	for lo < hi && !p.s.State.Failed() {
		mid := lo + (hi-lo)/2
		if v := p.at(mid); v != op && ranksBefore(p.s.validatorPower(v), v, power, op) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// find returns op's index, where its indexed power places it.
func (p powerIndex) find(op common.Address, indexed *big.Int) (uint64, error) {
	i := p.search(op, indexed)
	if i >= p.len() || p.at(i) != op {
		return 0, fmt.Errorf("%w: %s", errPowerIndexCorrupt, op)
	}
	return i, nil
}

// insert adds op at power.
func (p powerIndex) insert(op common.Address, power *big.Int) {
	i, n := p.search(op, power), p.len()
	for j := n; j > i && !p.s.State.Failed(); j-- {
		p.set(j, p.at(j-1))
	}
	p.set(i, op)
	p.s.SetWord(p.base(), solstorage.Slot(n+1))
}

// remove takes out op, indexed at power.
func (p powerIndex) remove(op common.Address, indexed *big.Int) error {
	i, err := p.find(op, indexed)
	if err != nil {
		return err
	}
	n := p.len()
	for j := i; j+1 < n && !p.s.State.Failed(); j++ {
		p.set(j, p.at(j+1))
	}
	p.s.SetWord(solstorage.Offset(solstorage.ArrayData(p.base()), n-1), common.Hash{})
	p.s.SetWord(p.base(), solstorage.Slot(n-1))
	return nil
}

// move reorders op from indexed power from to power to, one write per
// validator it passes.
func (p powerIndex) move(op common.Address, from, to *big.Int) error {
	if from.Cmp(to) == 0 {
		return nil
	}
	start, err := p.find(op, from)
	if err != nil {
		return err
	}
	i := start
	if to.Cmp(from) > 0 {
		for ; i > 0 && !p.s.State.Failed(); i-- {
			prev := p.at(i - 1)
			if !ranksBefore(to, op, p.s.validatorPower(prev), prev) {
				break
			}
			p.set(i, prev)
		}
	} else {
		for n := p.len(); i+1 < n && !p.s.State.Failed(); i++ {
			next := p.at(i + 1)
			if !ranksBefore(p.s.validatorPower(next), next, to, op) {
				break
			}
			p.set(i, next)
		}
	}
	if i != start {
		p.set(i, op)
	}
	return nil
}

// setTokensAndShares stores v as op's validator with tokens and shares,
// reordering it in the power index unless it is jailed.
func (s store) setTokensAndShares(op common.Address, v *validator, tokens, shares *big.Int) error {
	if !v.jailed {
		if err := s.powerIndex().move(op, consensusPower(v.tokens), consensusPower(tokens)); err != nil {
			return err
		}
	}
	v.tokens, v.delegatorShares = tokens, shares
	s.setValidator(op, *v)
	return nil
}

// addTokensFromDelegation adds amount to v's tokens and returns the shares it
// issues, as x/staking's AddValidatorTokensAndShares does: the first delegation
// to a validator with no shares sets the exchange rate to one.
func (s store) addTokensFromDelegation(op common.Address, v *validator, amount *big.Int) (*big.Int, error) {
	issued := intToDec(amount)
	if v.delegatorShares.Sign() != 0 {
		var err error
		if issued, err = v.sharesFromTokens(amount); err != nil {
			return nil, err
		}
	}
	tokens := new(big.Int).Add(v.tokens, amount)
	shares := new(big.Int).Add(v.delegatorShares, issued)
	return issued, s.setTokensAndShares(op, v, tokens, shares)
}

// removeDelegationShares takes shares from v and returns the tokens they are
// worth, truncated, as x/staking's RemoveValidatorTokensAndShares does: the last
// shares out take every token left.
func (s store) removeDelegationShares(op common.Address, v *validator, shares *big.Int) (*big.Int, error) {
	remaining := new(big.Int).Sub(v.delegatorShares, shares)
	if remaining.Sign() < 0 {
		return nil, fmt.Errorf("%w: removing %s of %s", errInsufficientShares, shares, v.delegatorShares)
	}
	issued := new(big.Int).Set(v.tokens)
	if remaining.Sign() != 0 {
		issued = truncateDec(v.tokensFromShares(shares))
	}
	tokens := new(big.Int).Sub(v.tokens, issued)
	if tokens.Sign() < 0 {
		return nil, fmt.Errorf("%w: removing %s tokens of %s", errInsufficientShares, issued, v.tokens)
	}
	return issued, s.setTokensAndShares(op, v, tokens, remaining)
}

// jail jails op's validator and takes it out of the power index.
func (s store) jail(op common.Address, v *validator) error {
	if err := s.powerIndex().remove(op, consensusPower(v.tokens)); err != nil {
		return err
	}
	v.jailed = true
	s.setValidator(op, *v)
	return nil
}

// unjail unjails op's validator and returns it to the power index.
func (s store) unjail(op common.Address, v *validator) {
	s.powerIndex().insert(op, consensusPower(v.tokens))
	v.jailed = false
	s.setValidator(op, *v)
}

// removeValidator deletes op's validator, which must be unbonded and hold no
// tokens, and every index naming it, then runs AfterValidatorRemoved.
func (p Precompile) removeValidator(block precompiles.BlockContext, s store, op common.Address, v validator) error {
	if v.status != statusUnbonded || v.tokens.Sign() != 0 {
		return fmt.Errorf("%w: removing %s with status %d and %s tokens", errInvalidTransition, op, v.status, v.tokens)
	}
	if !v.jailed {
		if err := s.powerIndex().remove(op, consensusPower(v.tokens)); err != nil {
			return err
		}
	}
	s.setValidatorByPubkey(v.consensusPubkey, common.Address{})
	s.validatorSet().Remove(solstorage.AddressKey(op))
	if err := s.deleteValidator(op); err != nil {
		return err
	}
	return p.hooks().AfterValidatorRemoved(block, s.State, op)
}

// bond makes op's validator bonded, as x/staking's bondValidator does.
func (p Precompile) bond(block precompiles.BlockContext, s store, op common.Address, v *validator) error {
	if v.status != statusUnbonded && v.status != statusUnbonding {
		return fmt.Errorf("%w: bonding %s with status %d", errInvalidTransition, op, v.status)
	}
	v.status = statusBonded
	s.setValidator(op, *v)
	return p.hooks().AfterValidatorBonded(block, s.State, op)
}

// beginUnbonding makes op's validator unbonding until one unbonding period from
// now, as x/staking's beginUnbondingValidator does, and queues its completion.
func (p Precompile) beginUnbonding(block precompiles.BlockContext, s store, op common.Address, v *validator, unbondingTime uint64) error {
	if v.status != statusBonded {
		return fmt.Errorf("%w: unbonding %s with status %d", errInvalidTransition, op, v.status)
	}
	q := s.validatorQueue()
	v.status = statusUnbonding
	v.unbondingTime = q.clamp(block.UnixNano + unbondingTime)
	v.unbondingHeight = block.Number
	s.setValidator(op, *v)
	e, err := q.push(v.unbondingTime)
	if err != nil {
		return err
	}
	s.SetUint64(at(e, validatorQueueUnbondingHeight), validatorQueueUnbondingHeight.offset, v.unbondingHeight)
	s.SetAddressAt(at(e, validatorQueueOperator), validatorQueueOperator.offset, op)
	return p.hooks().AfterValidatorBeginUnbonding(block, s.State, op)
}
