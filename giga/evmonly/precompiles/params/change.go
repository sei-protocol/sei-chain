package params

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

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
	Subspace string `json:"subspace"`
	Key      string `json:"key"`
	Value    []byte `json:"value"`
}

// LegacyChange is one change of a V2 ParameterChange proposal. Value is the
// amino JSON of the V2 key's value; a struct-valued key takes an object with
// any subset of its fields.
type LegacyChange struct {
	Subspace string          `json:"subspace"`
	Key      string          `json:"key"`
	Value    json.RawMessage `json:"value"`
}

// FromLegacy translates V2 changes into Changes, one per field set.
func FromLegacy(legacy []LegacyChange) ([]Change, error) {
	var out []Change
	for _, lc := range legacy {
		ss, ok := lookupSubspace(lc.Subspace)
		if !ok {
			return nil, fmt.Errorf("%w: %s", errUnknownSubspace, lc.Subspace)
		}
		ps, isStruct := ss.v2Params(lc.Key)
		if len(ps) == 0 {
			return nil, fmt.Errorf("%w: %s/%s", errUnknownKey, lc.Subspace, lc.Key)
		}
		if !isStruct {
			c, err := fromJSON(lc.Subspace, ps[0], lc.Value)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
			continue
		}
		fields, err := objectFields(lc.Value)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", lc.Subspace, lc.Key, err)
		}
		for _, name := range slices.Sorted(maps.Keys(fields)) {
			if !slices.ContainsFunc(ps, func(p param) bool { _, f := p.v2Key(); return f == name }) {
				return nil, fmt.Errorf("%w: %s/%s.%s", errUnknownKey, lc.Subspace, lc.Key, name)
			}
		}
		for _, p := range ps {
			_, field := p.v2Key()
			raw, ok := fields[field]
			if !ok {
				continue
			}
			c, err := fromJSON(lc.Subspace, p, raw)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// V2Change is a change as V2's ParameterChangeProposal holds it: Value is the
// JSON text of the V2 key's value.
type V2Change struct {
	Subspace string `json:"subspace"`
	Key      string `json:"key"`
	Value    string `json:"value"`
}

// ToV2 returns changes as V2 changes. Consecutive changes to fields of one
// struct-valued V2 key become one change whose value is an object of those
// fields, with names sorted as V2 marshals a decoded object. A change to an
// undeclared key keeps its flat key and the hex of its value.
func ToV2(changes []Change) []V2Change {
	out := []V2Change{}
	var fields map[string]any // the fields of out's last change, if struct-valued
	for _, c := range changes {
		ss, ok := lookupSubspace(c.Subspace)
		var p param
		if ok {
			p, ok = ss.param(c.Key)
		}
		if !ok {
			out = append(out, V2Change{Subspace: c.Subspace, Key: c.Key, Value: hexutil.Encode(c.Value)})
			fields = nil
			continue
		}
		value := p.kind.toJSON(new(big.Int).SetBytes(c.Value))
		key, field := p.v2Key()
		if field == "" {
			out = append(out, V2Change{Subspace: c.Subspace, Key: key, Value: mustJSON(value)})
			fields = nil
			continue
		}
		if last := len(out) - 1; fields == nil || out[last].Subspace != c.Subspace || out[last].Key != key {
			out = append(out, V2Change{Subspace: c.Subspace, Key: key})
			fields = map[string]any{}
		}
		fields[field] = value
		out[len(out)-1].Value = mustJSON(fields)
	}
	return out
}

// mustJSON marshals v, which holds only strings, maps and slices of them.
func mustJSON(v any) string {
	bz, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(bz)
}

// fromJSON returns the Change setting p to its amino JSON raw.
func fromJSON(subspace string, p param, raw json.RawMessage) (Change, error) {
	v, err := p.kind.fromJSON(raw)
	if err == nil && v.BitLen() > p.kind.bits {
		err = fmt.Errorf("%w: does not fit %s", errInvalidValue, p.kind.solType)
	}
	if err != nil {
		return Change{}, fmt.Errorf("%s/%s: %w", subspace, p.key, err)
	}
	return Change{Subspace: subspace, Key: p.key, Value: common.BigToHash(v).Bytes()}, nil
}

// Check reports whether changes would apply to the current params.
func Check(state precompiles.State, changes []Change) error {
	_, err := plan(state, changes)
	return err
}

// Apply writes changes in order and checks each changed subspace as a whole,
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

// plan decodes changes against the current params and returns the writes
// they make, in order.
func plan(state precompiles.State, changes []Change) ([]update, error) {
	if len(changes) == 0 {
		return nil, errNoChanges
	}
	s := newStore(state)
	working := map[*subspace]values{}
	var touched []*subspace
	updates := make([]update, 0, len(changes))
	for _, c := range changes {
		ss, ok := lookupSubspace(c.Subspace)
		if !ok {
			return nil, fmt.Errorf("%w: %s", errUnknownSubspace, c.Subspace)
		}
		p, ok := ss.param(c.Key)
		if !ok {
			return nil, fmt.Errorf("%w: %s/%s", errUnknownKey, c.Subspace, c.Key)
		}
		v, err := decodeValue(p, c.Value)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", c.Subspace, c.Key, err)
		}
		if _, ok := working[ss]; !ok {
			working[ss] = ss.read(s)
			touched = append(touched, ss)
		}
		working[ss][p.key] = v
		updates = append(updates, update{ss: ss, p: p, v: v})
	}
	for _, ss := range touched {
		if err := ss.check(working[ss]); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", errInvalidValue, ss.name, err)
		}
	}
	return updates, nil
}

// decodeValue decodes abi.encode of p's Solidity type.
func decodeValue(p param, value []byte) (*big.Int, error) {
	if len(value) != common.HashLength {
		return nil, fmt.Errorf("%w: %d bytes, want %d", errInvalidValue, len(value), common.HashLength)
	}
	v := new(big.Int).SetBytes(value)
	if v.BitLen() > p.kind.bits {
		return nil, fmt.Errorf("%w: does not fit %s", errInvalidValue, p.kind.solType)
	}
	return v, nil
}
