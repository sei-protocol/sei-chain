package params

import (
	"encoding/json"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// valueKind is how a param's value is stored, ABI-encoded and written as amino
// JSON.
type valueKind struct {
	solType  string // Solidity type of the stored and ABI-encoded value
	bits     int
	fromJSON func(json.RawMessage) (*big.Int, error)
	toJSON   func(*big.Int) any
	omitZero bool // amino leaves the field out of a struct when the value is zero
}

var (
	durationKind = valueKind{solType: "uint64", bits: 64, fromJSON: parseDuration, toJSON: formatDuration, omitZero: true}
	decKind      = valueKind{solType: "uint256", bits: 256, fromJSON: parseDec, toJSON: formatDec}
	coinsKind    = valueKind{solType: "uint256", bits: 256, fromJSON: parseCoins, toJSON: formatCoins, omitZero: true}
)

// param is one declared param.
type param struct {
	// key is the V2 key for a scalar, or the V2 key, ".", and the field's
	// amino JSON name for a field of a struct-valued V2 key.
	key  string
	kind valueKind
	at   member
}

// v2Key returns the V2 key p is or belongs to, and its field name, empty for a
// scalar.
func (p param) v2Key() (key, field string) {
	key, field, _ = strings.Cut(p.key, ".")
	return key, field
}

// values holds params by flat key.
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

func lookupSubspace(name string) (*subspace, bool) {
	for _, ss := range subspaces {
		if ss.name == name {
			return ss, true
		}
	}
	return nil, false
}

func (ss *subspace) param(key string) (param, bool) {
	for _, p := range ss.params {
		if p.key == key {
			return p, true
		}
	}
	return param{}, false
}

// v2Params returns the params V2 key holds, in amino field order, and whether
// key is a struct-valued key.
func (ss *subspace) v2Params(key string) (ps []param, isStruct bool) {
	for _, p := range ss.params {
		k, field := p.v2Key()
		if k == key {
			ps = append(ps, p)
			isStruct = field != ""
		}
	}
	return ps, isStruct
}

func (ss *subspace) slotOf(p param) common.Hash {
	return solstorage.Offset(solstorage.Slot(ss.slot), p.at.slot)
}

func (ss *subspace) get(s solstorage.Store, p param) *big.Int {
	if p.kind.bits == 64 {
		return new(big.Int).SetUint64(s.Uint64(ss.slotOf(p), p.at.offset))
	}
	return s.Uint256(ss.slotOf(p))
}

// set stores v, which must fit p's kind.
func (ss *subspace) set(s solstorage.Store, p param, v *big.Int) {
	if p.kind.bits == 64 {
		s.SetUint64(ss.slotOf(p), p.at.offset, v.Uint64())
		return
	}
	s.SetUint256(ss.slotOf(p), v)
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
