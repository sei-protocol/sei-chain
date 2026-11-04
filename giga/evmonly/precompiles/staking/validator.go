package staking

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

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
