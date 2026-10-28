package params

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

const (
	decimals  = 18
	baseDenom = "usei"
)

var (
	decOne = new(big.Int).Exp(big.NewInt(10), big.NewInt(decimals), nil)

	errInvalidValue = errors.New("invalid param value")
)

// jsonString decodes raw as a JSON string.
func jsonString(raw json.RawMessage) (string, error) {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return "", fmt.Errorf("%w: %s is not a JSON string", errInvalidValue, raw)
	}
	return s, nil
}

// parseFixed parses a non-negative decimal with at most scale fractional
// digits into its 10^scale-scaled integer.
func parseFixed(s string, scale int) (*big.Int, error) {
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" || (hasDot && frac == "") || len(frac) > scale || !digits(whole) || !digits(frac) {
		return nil, fmt.Errorf("%w: %q", errInvalidValue, s)
	}
	v, _ := new(big.Int).SetString(whole+frac+strings.Repeat("0", scale-len(frac)), 10)
	return v, nil
}

// formatFixed renders a 10^scale-scaled integer with scale fractional digits.
func formatFixed(v *big.Int, scale int) string {
	s := v.String()
	if len(s) <= scale {
		s = strings.Repeat("0", scale-len(s)+1) + s
	}
	return s[:len(s)-scale] + "." + s[len(s)-scale:]
}

func digits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// durationUnits are the units a Duration may be written in, by the number of
// fractional digits each allows in nanoseconds.
var durationUnits = map[string]int{"": 0, "ns": 0, "ms": 6, "s": 9}

// parseDuration reads a Duration as amino's string of nanoseconds, or as a
// decimal followed by "s", "ms" or "ns" that is a whole number of nanoseconds.
func parseDuration(raw json.RawMessage) (*big.Int, error) {
	s, err := jsonString(raw)
	if err != nil {
		return nil, err
	}
	number := strings.TrimRight(s, "smn")
	scale, ok := durationUnits[s[len(number):]]
	if !ok {
		return nil, fmt.Errorf("%w: duration %q", errInvalidValue, s)
	}
	return parseFixed(number, scale)
}

// formatDuration renders nanoseconds as amino's string of nanoseconds.
func formatDuration(nanos *big.Int) any {
	return nanos.String()
}

func parseDec(raw json.RawMessage) (*big.Int, error) {
	s, err := jsonString(raw)
	if err != nil {
		return nil, err
	}
	return parseFixed(s, decimals)
}

func formatDec(v *big.Int) any {
	return formatFixed(v, decimals)
}

// coin is an amino Coin.
type coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

// parseCoins reads amino Coins holding at most one coin, of usei, as its
// amount. Empty Coins are zero.
func parseCoins(raw json.RawMessage) (*big.Int, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var cs []coin
	if len(raw) == 0 || raw[0] != '[' || dec.Decode(&cs) != nil || dec.More() {
		return nil, fmt.Errorf("%w: %s is not a coin list", errInvalidValue, raw)
	}
	switch {
	case len(cs) == 0:
		return new(big.Int), nil
	case len(cs) > 1 || cs[0].Denom != baseDenom:
		return nil, fmt.Errorf("%w: only %s is accepted", errInvalidValue, baseDenom)
	case cs[0].Amount == "" || !digits(cs[0].Amount):
		return nil, fmt.Errorf("%w: amount %q", errInvalidValue, cs[0].Amount)
	}
	v, _ := new(big.Int).SetString(cs[0].Amount, 10)
	return v, nil
}

func formatCoins(usei *big.Int) any {
	if usei.Sign() == 0 {
		return []coin{}
	}
	return []coin{{Denom: baseDenom, Amount: usei.String()}}
}

// objectFields decodes raw as a JSON object, rejecting duplicate names.
func objectFields(raw json.RawMessage) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("%w: %s is not a JSON object", errInvalidValue, raw)
	}
	out := map[string]json.RawMessage{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errInvalidValue, err)
		}
		name := t.(string) // object keys are always strings
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("%w: duplicate field %q", errInvalidValue, name)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("%w: %w", errInvalidValue, err)
		}
		out[name] = v
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidValue, err)
	}
	if _, err := dec.Token(); err == nil {
		return nil, fmt.Errorf("%w: data after the object", errInvalidValue)
	}
	return out, nil
}
