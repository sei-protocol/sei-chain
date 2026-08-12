package configcli

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
)

// Source is an existing node's resolved configuration, read by key.
//
// Declared here rather than imported, so this package does not depend on whichever type happens to
// carry a node's configuration today. Anything that enumerates its keys and answers for one
// satisfies it.
type Source interface {
	AllKeys() []string
	Get(string) any
}

// Adoption is the result of carrying an existing node's configuration into a sei.toml.
type Adoption struct {
	// File is the document produced. Not written anywhere until the caller saves it.
	File *seitoml.File
	// Carried are the keys whose value came from the existing configuration, sorted.
	Carried []string
	// Baselined are declared keys the existing configuration had no value for, sorted.
	Baselined []string
	// Environment are keys an environment variable supplies, sorted. Reported, never written.
	Environment []string
	// Unconvertible are keys whose existing value could not be read as the declared type, sorted.
	// Each keeps its baseline in the file, so nothing silently becomes a value nobody chose.
	Unconvertible []Rejection
}

// Rejection is one existing value that could not be carried over.
type Rejection struct {
	// Key is the dotted key.
	Key string
	// Value is what the existing configuration held.
	Value any
	// Reason says why it could not be read as the declared type.
	Reason string
}

// Adopt builds a sei.toml from a node's existing configuration.
//
// The existing values are what the file is built from, not the binary's baselines, so a node that
// has been running keeps what it was running rather than being silently moved onto defaults. That is
// the whole difference between adopting and generating.
//
// Environment variables are reported and never folded into the file. They sit above the file in the
// resolution order, so writing one in would change nothing while it is still set and would silently
// change the node's behaviour the day somebody unsets it.
//
// A value that cannot be read as its declared type keeps the baseline and is reported. Writing it
// anyway would produce a file the node refuses at its next boot, and quietly dropping it would move
// the node onto a default nobody chose.
func Adopt(existing Source, lookupEnv func(string) (string, bool), mode registry.Mode) (Adoption, error) {
	if existing == nil {
		return Adoption{}, fmt.Errorf("no existing configuration to adopt from")
	}
	if err := knownMode(mode); err != nil {
		return Adoption{}, err
	}
	resolved, err := registry.Resolve(mode)
	if err != nil {
		return Adoption{}, fmt.Errorf("resolve the baselines for mode %q: %w", mode, err)
	}
	if len(resolved.Keys) == 0 {
		return Adoption{}, fmt.Errorf("no section has registered a key, so there is nothing to adopt")
	}

	file, err := seitoml.New()
	if err != nil {
		return Adoption{}, err
	}
	out := Adoption{File: file}

	for _, key := range sortedKeys(resolved) {
		baseline := resolved.Keys[key].Value
		if err := file.Set(key, adoptedValue(existing, lookupEnv, key, baseline, &out)); err != nil {
			return Adoption{}, fmt.Errorf("write %s: %w", key, err)
		}
	}

	file.SetPreamble(adoptionPreamble(mode, out))
	sort.Strings(out.Carried)
	sort.Strings(out.Baselined)
	sort.Strings(out.Environment)
	sort.Slice(out.Unconvertible, func(i, j int) bool {
		return out.Unconvertible[i].Key < out.Unconvertible[j].Key
	})
	return out, nil
}

// adoptedValue decides one key's value and records where it came from.
func adoptedValue(
	existing Source,
	lookupEnv func(string) (string, bool),
	key string,
	baseline any,
	out *Adoption,
) any {
	// The environment is recorded first and separately, because a variable is set whether or not the
	// existing files also carry the key, and it is the one that wins today.
	if lookupEnv != nil {
		if _, set := lookupEnv(registry.EnvName(key)); set {
			out.Environment = append(out.Environment, key)
		}
	}

	raw := existing.Get(key)
	if raw == nil {
		out.Baselined = append(out.Baselined, key)
		return baseline
	}
	converted, err := coerce(raw, reflect.TypeOf(baseline))
	if err != nil {
		out.Unconvertible = append(out.Unconvertible, Rejection{Key: key, Value: raw, Reason: err.Error()})
		return baseline
	}
	out.Carried = append(out.Carried, key)
	return converted
}

// adoptionPreamble is what the adopted file says about itself.
func adoptionPreamble(mode registry.Mode, out Adoption) []string {
	lines := []string{
		fmt.Sprintf(" Adopted from this node's existing configuration, for %q mode.", mode),
		"",
		fmt.Sprintf(" %d setting(s) were carried over from the files this node was already running,",
			len(out.Carried)),
		fmt.Sprintf(" and %d took this binary's default because the old configuration had no value.",
			len(out.Baselined)),
		"",
		" Every key below is a written value, which this binary treats as your decision and never",
		" rewrites, so this node keeps them across an upgrade.",
	}
	if len(out.Environment) > 0 {
		lines = append(lines,
			"",
			fmt.Sprintf(" %d setting(s) are supplied by environment variables and were deliberately",
				len(out.Environment)),
			" not written here. An environment variable overrides this file, so writing it in would",
			" change nothing now and would change how this node runs the day it is unset.")
	}
	return lines
}

// coerce reads an existing configuration value as the declared type.
//
// Existing values arrive from a file or a flag binding, so a number may be a string and a duration
// may be text. Converting through the declared type is what keeps an adopted file readable by the
// node, and refusing is what keeps a value nobody can read out of it.
func coerce(raw any, want reflect.Type) (any, error) {
	if want == nil {
		return nil, fmt.Errorf("the key has no declared type")
	}
	// A value already of the declared type is taken as it stands.
	if reflect.TypeOf(raw) == want {
		return raw, nil
	}
	if want == reflect.TypeOf(time.Duration(0)) {
		return coerceDuration(raw)
	}
	switch want.Kind() {
	case reflect.Bool:
		if b, ok := raw.(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("%#v is not true or false", raw)
	case reflect.String:
		if s, ok := raw.(string); ok {
			return s, nil
		}
		return nil, fmt.Errorf("%#v is not text", raw)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return coerceWholeNumber(raw)
	case reflect.Float32, reflect.Float64:
		return coerceFloat(raw)
	case reflect.Slice:
		if want.Elem().Kind() != reflect.String {
			return nil, fmt.Errorf("a list of %s cannot be carried over", want.Elem())
		}
		return coerceStringList(raw)
	default:
		return nil, fmt.Errorf("a %s cannot be carried over", want)
	}
}

// coerceDuration reads a duration, refusing a bare number.
//
// A unit-less number is the coercion the legacy path performs silently, reading it as nanoseconds,
// which turns an intended 30 seconds into 30 billionths of one.
func coerceDuration(raw any) (any, error) {
	s, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("%#v is not a duration; a duration needs a unit, as in 30s", raw)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, fmt.Errorf("%q is not a duration; a duration needs a unit, as in 30s", s)
	}
	return d, nil
}

// coerceWholeNumber reads a whole number, refusing anything with a fractional part.
func coerceWholeNumber(raw any) (any, error) {
	switch n := raw.(type) {
	case int:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case uint64:
		return n, nil
	case string:
		v, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a whole number", n)
		}
		return v, nil
	case float64:
		if n != float64(int64(n)) {
			return nil, fmt.Errorf("%v has a fractional part and the key is a whole number", n)
		}
		return int64(n), nil
	default:
		return nil, fmt.Errorf("%#v is not a whole number", raw)
	}
}

// coerceFloat reads a number.
func coerceFloat(raw any) (any, error) {
	switch n := raw.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case string:
		v, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", n)
		}
		return v, nil
	default:
		return nil, fmt.Errorf("%#v is not a number", raw)
	}
}

// coerceStringList reads a list of strings.
func coerceStringList(raw any) (any, error) {
	switch v := raw.(type) {
	case []string:
		return v, nil
	case string:
		return splitList(v), nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("the list holds %#v, which is not text", item)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%#v is not a list of text", raw)
	}
}

// Report renders an adoption for an operator, most actionable first.
func (a Adoption) Report() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Carried %d setting(s) over and took this binary's default for %d.\n",
		len(a.Carried), len(a.Baselined)))

	if len(a.Unconvertible) > 0 {
		b.WriteString(fmt.Sprintf("\n%d existing value(s) could not be read as the setting's type. "+
			"Each kept this binary's default, so check them before starting the node:\n",
			len(a.Unconvertible)))
		for _, r := range a.Unconvertible {
			b.WriteString(fmt.Sprintf("  %s held %#v: %s\n", r.Key, r.Value, r.Reason))
		}
	}
	if len(a.Environment) > 0 {
		b.WriteString(fmt.Sprintf("\n%d setting(s) come from environment variables and were not "+
			"written to the file. They override it, so the file cannot record them:\n",
			len(a.Environment)))
		for _, k := range a.Environment {
			b.WriteString(fmt.Sprintf("  %s (%s)\n", k, registry.EnvName(k)))
		}
	}
	return b.String()
}
