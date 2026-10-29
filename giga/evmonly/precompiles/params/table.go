package params

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// valueKind is how a param's value is stored and ABI-encoded: its Solidity
// type and width.
type valueKind struct {
	solType string
	bits    int
}

var (
	decKind      = valueKind{solType: "uint256", bits: 256}
	durationKind = valueKind{solType: "uint64", bits: 64}
	coinsKind    = valueKind{solType: "uint256", bits: 256}
)

// param is one declared param.
type param struct {
	// key is the V2 key for a scalar, or the V2 key, ".", and the field's
	// name for a field of a struct-valued V2 key.
	key  string
	kind valueKind
	at   member
}

// values holds every param of one subspace, by flat key.
type values map[string]*big.Int

// subspace is one declared subspace, a struct of ParamsStorage.
type subspace struct {
	name   string
	slot   uint64
	params []param
	// check validates the subspace's params as a whole.
	check func(values) error
}

// subspaces are the declared subspaces, in ParamsStorage order.
var subspaces = []*subspace{&govSubspace}

// Key is a declared param: its subspace, flat key, and the Solidity type of its
// value.
type Key struct {
	Subspace string
	Key      string
	SolType  string
}

// Keys returns the declared params in table order.
func Keys() []Key {
	var out []Key
	for _, ss := range subspaces {
		for _, p := range ss.params {
			out = append(out, Key{Subspace: ss.name, Key: p.key, SolType: p.kind.solType})
		}
	}
	return out
}

func lookupSubspace(name string) (*subspace, bool) {
	for _, ss := range subspaces {
		if ss.name == name {
			return ss, true
		}
	}
	return nil, false
}

// mustParam returns the declared param key, panicking at package initialization
// when it is not in the table.
func (ss *subspace) mustParam(key string) param {
	p, ok := ss.param(key)
	if !ok {
		panic(fmt.Sprintf("params: %s/%s is not declared", ss.name, key))
	}
	return p
}

func (ss *subspace) param(key string) (param, bool) {
	for _, p := range ss.params {
		if p.key == key {
			return p, true
		}
	}
	return param{}, false
}

func (ss *subspace) slotOf(p param) common.Hash {
	return solstorage.Offset(solstorage.Slot(ss.slot), p.at.slot)
}

// get reads p's value from the bytes of its slot its Solidity type occupies.
func (ss *subspace) get(s solstorage.Store, p param) *big.Int {
	w := s.Word(ss.slotOf(p))
	return new(big.Int).SetBytes(p.bytesOf(w[:]))
}

// set stores v, which validate has checked fits p's kind, leaving the rest of
// p's slot unchanged.
func (ss *subspace) set(s solstorage.Store, p param, v *big.Int) {
	slot := ss.slotOf(p)
	w := s.Word(slot)
	v.FillBytes(p.bytesOf(w[:]))
	s.SetWord(slot, w)
}

// bytesOf returns the bytes of word that p occupies.
func (p param) bytesOf(word []byte) []byte {
	end := len(word) - p.at.offset
	return word[end-p.kind.bits/8 : end]
}

// validate checks that v holds every param of ss, each fitting its kind, and
// that the subspace as a whole is valid. Every write of a subspace's params is
// checked here first.
func (ss *subspace) validate(v values) error {
	for _, p := range ss.params {
		x, ok := v[p.key]
		if !ok || x == nil {
			return fmt.Errorf("%w: %s has no value", errInvalidValue, p.key)
		}
		if x.Sign() < 0 || x.BitLen() > p.kind.bits {
			return fmt.Errorf("%w: %s does not fit %s", errInvalidValue, p.key, p.kind.solType)
		}
	}
	return ss.check(v)
}

// read returns every param of the subspace.
func (ss *subspace) read(s solstorage.Store) values {
	out := make(values, len(ss.params))
	for _, p := range ss.params {
		out[p.key] = ss.get(s, p)
	}
	return out
}

func newStore(state precompiles.State) solstorage.Store {
	return solstorage.Store{State: state, Address: Address}
}
