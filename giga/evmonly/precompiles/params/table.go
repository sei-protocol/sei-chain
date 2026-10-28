package params

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// V2Type is a param value's V2 type, as Params.sol's V2Type enum numbers it.
type V2Type uint8

// The V2 types.
const (
	V2Dec V2Type = iota
	V2Duration
	V2Coins
	V2DecCoins
	V2Int
	V2Uint
	V2Uint64
	V2Uint32
	V2Int64
	V2Int32
	V2Bool
	V2String
	V2Address
	V2StringList
)

// valueKind is how a param's value is stored and ABI-encoded: its Solidity
// type, and the V2 type that gives its unit.
type valueKind struct {
	solType string
	bits    int
	v2Type  V2Type
}

var (
	decKind      = valueKind{solType: "uint256", bits: 256, v2Type: V2Dec}
	durationKind = valueKind{solType: "uint64", bits: 64, v2Type: V2Duration}
	coinsKind    = valueKind{solType: "uint256", bits: 256, v2Type: V2Coins}
)

// param is one declared param.
type param struct {
	// key is the V2 key for a scalar, or the V2 key, ".", and the field's
	// name for a field of a struct-valued V2 key.
	key  string
	kind valueKind
	at   member
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
