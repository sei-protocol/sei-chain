package params

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

var (
	errUnknownSubspace = errors.New("unknown param subspace")
	errUnknownKey      = errors.New("unknown param key")
	errNoChanges       = errors.New("at least one parameter change must be specified")
)

// Change sets one param. Key is its flat key and Value is its new value as
// abi.encode of the param's Solidity type.
type Change struct {
	Subspace string
	Key      string
	Value    []byte
}

// Check reports whether changes would apply to the current params.
func Check(state precompiles.State, changes []Change) error {
	_, err := plan(state, changes)
	return err
}

// Apply writes each changed param's final value after checking each changed
// subspace as a whole, writing nothing if any change fails.
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

// plan decodes changes against the current params and returns one write per
// changed param, holding its last value, in the order first changed. Later
// changes to a param win.
func plan(state precompiles.State, changes []Change) ([]update, error) {
	if len(changes) == 0 {
		return nil, errNoChanges
	}
	s := newStore(state)
	working := map[*subspace]values{}
	var touched []*subspace
	var updates []update
	changed := map[*subspace]map[string]bool{}
	for _, c := range changes {
		ss, ok := lookupSubspace(c.Subspace)
		if !ok {
			return nil, fmt.Errorf("%w: %.64q", errUnknownSubspace, c.Subspace)
		}
		p, ok := ss.param(c.Key)
		if !ok {
			return nil, fmt.Errorf("%w: %.64q/%.64q", errUnknownKey, c.Subspace, c.Key)
		}
		v, err := decodeValue(c.Value)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", c.Subspace, c.Key, err)
		}
		if _, ok := working[ss]; !ok {
			working[ss] = ss.read(s)
			changed[ss] = map[string]bool{}
			touched = append(touched, ss)
		}
		working[ss][p.key] = v
		if !changed[ss][p.key] {
			changed[ss][p.key] = true
			updates = append(updates, update{ss: ss, p: p})
		}
	}
	for _, ss := range touched {
		if err := ss.validate(working[ss]); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", errInvalidValue, ss.name, err)
		}
	}
	for i := range updates {
		u := &updates[i]
		u.v = working[u.ss][u.p.key]
		if u.p.least != nil && u.v.Cmp(u.p.least) < 0 {
			return nil, fmt.Errorf("%w: %s/%s must be at least %s", errInvalidValue, u.ss.name, u.p.key, u.p.least)
		}
	}
	return updates, nil
}

// decodeValue decodes a param value, the 32-byte abi.encode of its Solidity
// type. validate checks that it fits that type.
func decodeValue(value []byte) (*big.Int, error) {
	if len(value) != common.HashLength {
		return nil, fmt.Errorf("%w: %d bytes, want %d", errInvalidValue, len(value), common.HashLength)
	}
	return new(big.Int).SetBytes(value), nil
}
