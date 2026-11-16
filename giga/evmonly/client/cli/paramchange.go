package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	paramproposal "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types/proposal"
)

// declaredParam returns the declared param subspace/flatKey.
func declaredParam(subspace, flatKey string) (chainparams.Key, bool) {
	for _, k := range chainparams.Keys() {
		if k.Subspace == subspace && k.Key == flatKey {
			return k, true
		}
	}
	return chainparams.Key{}, false
}

// fieldsOf returns the declared params of subspace that are fields of v2Key,
// in table order.
func fieldsOf(subspace, v2Key string) []chainparams.Key {
	var out []chainparams.Key
	for _, k := range chainparams.Keys() {
		if k.Subspace == subspace && strings.HasPrefix(k.Key, v2Key+".") {
			out = append(out, k)
		}
	}
	return out
}

// declaredKeys lists subspace's flat keys, or every subspace/flatKey when
// subspace declares none.
func declaredKeys(subspace string) string {
	keys := chainparams.Keys()
	own := make([]string, 0, len(keys))
	all := make([]string, 0, len(keys))
	for _, k := range keys {
		all = append(all, k.Subspace+"/"+k.Key)
		if k.Subspace == subspace {
			own = append(own, k.Key)
		}
	}
	if len(own) > 0 {
		return strings.Join(own, ", ")
	}
	return strings.Join(all, ", ")
}

// flatChanges translates V2 param changes into the precompile's flat-key
// changes. A change to a struct-valued V2 key, such as tallyparams with value
// {"quorum":"0.45"}, becomes one change per field; a change may also name a
// flat key, such as tallyparams.quorum, with a scalar value.
func flatChanges(v2 []v2ParamChange) ([]paramChange, error) {
	var out []paramChange
	seen := map[string]bool{}
	add := func(k chainparams.Key, raw json.RawMessage) error {
		id := k.Subspace + "/" + k.Key
		if seen[id] {
			return fmt.Errorf("%s is changed more than once", id)
		}
		seen[id] = true
		v, err := encodeParam(k, raw)
		if err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		out = append(out, paramChange{Subspace: k.Subspace, Key: k.Key, Value: v})
		return nil
	}
	for _, c := range v2 {
		if k, ok := declaredParam(c.Subspace, c.Key); ok {
			if err := add(k, c.Value); err != nil {
				return nil, err
			}
			continue
		}
		fields := fieldsOf(c.Subspace, c.Key)
		if len(fields) == 0 {
			return nil, fmt.Errorf("%s/%s is not a param of this chain; declared: %s", c.Subspace, c.Key, declaredKeys(c.Subspace))
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(c.Value, &obj); err != nil {
			return nil, fmt.Errorf("%s/%s: value must be an object of its fields: %w", c.Subspace, c.Key, err)
		}
		names := make([]string, 0, len(obj))
		for name := range obj {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			k, ok := declaredParam(c.Subspace, c.Key+"."+name)
			if !ok {
				return nil, fmt.Errorf("%s/%s.%s is not a param of this chain; declared: %s", c.Subspace, c.Key, name, declaredKeys(c.Subspace))
			}
			if err := add(k, obj[name]); err != nil {
				return nil, err
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no param changes")
	}
	return out, nil
}

// encodeParam returns raw, a V2 JSON value, as the 32-byte abi.encode of k's
// value. A Dec is a decimal string or number, a Duration nanoseconds or a Go
// duration string such as "30s", and Coins a usei coin string or V2's coin
// array.
func encodeParam(k chainparams.Key, raw json.RawMessage) ([]byte, error) {
	var v *big.Int
	var err error
	switch k.Kind {
	case chainparams.KindDec:
		v, err = parseDecValue(raw)
	case chainparams.KindDuration:
		v, err = parseDurationValue(raw)
	case chainparams.KindCoins:
		v, err = parseCoinsValue(raw)
	default:
		err = fmt.Errorf("unknown param kind %d", k.Kind)
	}
	if err != nil {
		return nil, err
	}
	if v.Sign() < 0 || v.BitLen() > 256 {
		return nil, fmt.Errorf("value %s is out of range", v)
	}
	return common.BigToHash(v).Bytes(), nil
}

// jsonScalar returns raw, a JSON string or number, as text.
func jsonScalar(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&n); err != nil {
		return "", fmt.Errorf("want a string or number: %w", err)
	}
	return n.String(), nil
}

func parseDecValue(raw json.RawMessage) (*big.Int, error) {
	s, err := jsonScalar(raw)
	if err != nil {
		return nil, err
	}
	d, err := sdk.NewDecFromStr(s)
	if err != nil {
		return nil, err
	}
	return scaledFromDec(d), nil
}

func parseDurationValue(raw json.RawMessage) (*big.Int, error) {
	s, err := jsonScalar(raw)
	if err != nil {
		return nil, err
	}
	if ns, ok := new(big.Int).SetString(s, 10); ok {
		if ns.BitLen() > 64 {
			return nil, fmt.Errorf("duration %s overflows uint64", s)
		}
		return ns, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, fmt.Errorf("duration %q: want nanoseconds or a duration such as \"30s\"", s)
	}
	return big.NewInt(int64(d)), nil
}

func parseCoinsValue(raw json.RawMessage) (*big.Int, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '[' {
		var coins []struct {
			Denom  string `json:"denom"`
			Amount string `json:"amount"`
		}
		if err := json.Unmarshal(raw, &coins); err != nil {
			return nil, err
		}
		parts := make([]string, len(coins))
		for i, c := range coins {
			parts[i] = c.Amount + c.Denom
		}
		return depositWei(strings.Join(parts, ","))
	}
	s, err := jsonScalar(raw)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s) == "" {
		return nil, errors.New("want a coin amount such as \"10usei\"")
	}
	return depositWei(s)
}

// formatParam returns v, a value of k, as V2 JSON: a Dec as a decimal string,
// a Duration as nanoseconds, and Coins as a usei coin array.
func formatParam(k chainparams.Key, v *big.Int) (json.RawMessage, error) {
	switch k.Kind {
	case chainparams.KindDec:
		return json.Marshal(decFromScaled(v).String())
	case chainparams.KindDuration:
		if v.BitLen() > 64 || v.Uint64() > math.MaxInt64 {
			return nil, fmt.Errorf("%s/%s: duration %s overflows", k.Subspace, k.Key, v)
		}
		return json.Marshal(strconv.FormatUint(v.Uint64(), 10))
	case chainparams.KindCoins:
		return json.Marshal(weiToCoins(v))
	default:
		return nil, fmt.Errorf("unknown param kind %d", k.Kind)
	}
}

// v2Key splits a flat key into its V2 key and field, with an empty field for a
// scalar.
func v2Key(flatKey string) (key, field string) {
	if i := strings.IndexByte(flatKey, '.'); i >= 0 {
		return flatKey[:i], flatKey[i+1:]
	}
	return flatKey, ""
}

// v2ParamChanges returns flat-key changes as V2 changes, merging the fields of
// one V2 key into one change whose value is an object of them.
func v2ParamChanges(changes []paramChange) ([]paramproposal.ParamChange, error) {
	type group struct {
		subspace, key string
		scalar        json.RawMessage
		fields        map[string]json.RawMessage
	}
	var order []*group
	groups := map[string]*group{}
	for _, c := range changes {
		k, ok := declaredParam(c.Subspace, c.Key)
		if !ok {
			k = chainparams.Key{Subspace: c.Subspace, Key: c.Key}
		}
		var value json.RawMessage
		var err error
		if ok {
			value, err = formatParam(k, new(big.Int).SetBytes(c.Value))
		} else {
			value, err = json.Marshal("0x" + common.Bytes2Hex(c.Value))
		}
		if err != nil {
			return nil, err
		}
		key, field := v2Key(c.Key)
		id := c.Subspace + "/" + key
		g := groups[id]
		if g == nil {
			g = &group{subspace: c.Subspace, key: key, fields: map[string]json.RawMessage{}}
			groups[id] = g
			order = append(order, g)
		}
		if field == "" {
			g.scalar = value
		} else {
			g.fields[field] = value
		}
	}
	out := make([]paramproposal.ParamChange, 0, len(order))
	for _, g := range order {
		value := g.scalar
		if value == nil {
			var err error
			if value, err = json.Marshal(g.fields); err != nil {
				return nil, err
			}
		}
		out = append(out, paramproposal.ParamChange{Subspace: g.subspace, Key: g.key, Value: string(value)})
	}
	return out, nil
}
