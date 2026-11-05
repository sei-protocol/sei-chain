package params

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var (
	errUnknownSubspace = errors.New("unknown param subspace")
	errUnknownKey      = errors.New("unknown param key")
	errNoChanges       = errors.New("at least one parameter change must be specified")
	errRepeatedKey     = errors.New("param changed more than once")
)

// Change sets one param. Key is its flat key and Value is its new value as
// abi.encode of the param's Solidity type.
type Change struct {
	Subspace string
	Key      string
	Value    []byte
}

// CheckChanges range-checks changes without reading state: there is at least
// one, and each names a declared param no other change names, with a value
// that fits its type and is at least its least value.
func CheckChanges(changes []Change) error {
	_, err := resolve(changes)
	return err
}

// Check reports whether changes would apply to the current params.
func Check(state precompiles.State, changes []Change) error {
	_, err := plan(state, changes)
	return err
}

// Apply writes changes after checking each changed subspace as a whole,
// writing nothing if any change fails.
func Apply(state precompiles.State, changes []Change) error {
	updates, err := plan(state, changes)
	if err != nil {
		return err
	}
	s := newStore(state)
	for _, u := range updates {
		u.ss.set(s, u.p, u.v)
	}
	return nil
}

// update is one param write.
type update struct {
	ss *subspace
	p  param
	v  *big.Int
}

// resolve range-checks changes as CheckChanges does and returns their writes,
// in order.
func resolve(changes []Change) ([]update, error) {
	if len(changes) == 0 {
		return nil, errNoChanges
	}
	type ref struct {
		ss  *subspace
		key string
	}
	seen := map[ref]bool{}
	updates := make([]update, 0, len(changes))
	for _, c := range changes {
		ss, p, err := lookupParam(c.Subspace, c.Key)
		if err != nil {
			return nil, err
		}
		r := ref{ss, p.key}
		if seen[r] {
			return nil, fmt.Errorf("%w: %s/%s", errRepeatedKey, ss.name, p.key)
		}
		seen[r] = true
		v, err := p.decode(c.Value)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", ss.name, p.key, err)
		}
		updates = append(updates, update{ss: ss, p: p, v: v})
	}
	return updates, nil
}

// plan resolves changes and checks each changed subspace, with the changes
// applied to its current params, as a whole.
func plan(state precompiles.State, changes []Change) ([]update, error) {
	updates, err := resolve(changes)
	if err != nil {
		return nil, err
	}
	s := newStore(state)
	working := map[*subspace]values{}
	var touched []*subspace
	for _, u := range updates {
		if _, ok := working[u.ss]; !ok {
			working[u.ss] = u.ss.read(s)
			touched = append(touched, u.ss)
		}
		working[u.ss][u.p.key] = u.v
	}
	for _, ss := range touched {
		if err := ss.validate(working[ss]); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", errInvalidValue, ss.name, err)
		}
	}
	return updates, nil
}
