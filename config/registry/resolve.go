package registry

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Layer is one configuration source's contribution, before precedence is applied.
//
// Source names which layer this is, and must appear in Precedence. Values holds that source's own
// keys and nothing else: a layer that folded in a second source would make provenance unrecoverable,
// which is the property that lets a diagnostic tell a node operator their environment variable beat
// their file.
type Layer struct {
	Source string
	Values map[string]any
}

// Resolution is one key's resolved value and where it came from.
//
// From is the whole point of resolving through named layers rather than merging maps. The legacy
// path cannot answer it: its layers are combined inside one viper before anything observes them, so
// there is no point at which a value's source is recoverable, and that is why it can never tell an
// operator which source won.
type Resolution struct {
	// Key is the dotted path.
	Key string
	// Value is what resolved.
	Value any
	// From is the Source of the layer that won, or "default" for a baseline.
	From string
}

// Resolved is every declared key's resolution, keyed by dotted path.
type Resolved struct {
	// Keys carries one resolution per declared key.
	Keys map[string]Resolution
	// Unknown are keys a layer carried that no section declares, sorted.
	//
	// Reported rather than an error, because what to do about one is the caller's decision: a
	// generate path may want to refuse, while a boot on an operator's existing file must not.
	Unknown []string
}

// From returns the resolution for a key.
func (r Resolved) From(key string) (Resolution, bool) {
	res, ok := r.Keys[key]
	return res, ok
}

// Overrides returns the keys whose value came from something other than a baseline, sorted.
//
// This is what a diff renders: the keys an operator has actually taken responsibility for, as
// distinct from the ones tracking the binary's judgement.
func (r Resolved) Overrides() []string {
	var out []string
	for k, res := range r.Keys {
		if res.From != defaultLayer {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// defaultLayer is the Source name of the implicit baseline layer, and the first entry in Precedence.
const defaultLayer = "default"

// Resolve reduces layers to one value per declared key, in the declared order.
//
// The order comes from Precedence, not from the order layers are passed, so a caller cannot change
// the outcome by reordering its arguments. That is the difference between a declared precedence and
// an emergent one: the legacy path's answer depends on which viper instance a caller asked, which is
// why two different orders are observable across its key set.
//
// Every declared key resolves, because the baseline is a layer like any other. A key no layer
// mentions therefore carries its mode's baseline rather than being absent, which is what makes an
// absent key track the binary's judgement instead of a zero value.
func Resolve(mode Mode, layers ...Layer) (Resolved, error) {
	out := Resolved{Keys: map[string]Resolution{}}

	baseline, err := baselineLayer(mode)
	if err != nil {
		return out, err
	}
	declared := declaredKeys()

	// Ordered by Precedence rather than by argument order. An unknown Source is an error rather
	// than a silently ignored layer, since a layer that never contributes is worse than one that
	// fails loudly: nothing downstream could tell it had been dropped.
	bySource := map[string]Layer{defaultLayer: baseline}
	for _, l := range layers {
		if l.Source == defaultLayer {
			return out, fmt.Errorf("a layer names the reserved source %q; the baseline is derived from "+
				"the registry, not supplied", defaultLayer)
		}
		if !known(l.Source) {
			return out, fmt.Errorf("layer %q is not in Precedence %v, so it has no defined priority and "+
				"the resolver would have to invent one", l.Source, Precedence)
		}
		if _, dup := bySource[l.Source]; dup {
			return out, fmt.Errorf("two layers name source %q; one of them would silently lose", l.Source)
		}
		bySource[l.Source] = l
	}

	unknown := map[string]bool{}
	for _, source := range Precedence {
		l, ok := bySource[source]
		if !ok {
			continue
		}
		for key, v := range l.Values {
			if !declared[key] {
				// A key nothing declares cannot be resolved into anything, and silently dropping it
				// is how an operator's typo becomes invisible.
				unknown[key] = true
				continue
			}
			out.Keys[key] = Resolution{Key: key, Value: v, From: source}
		}
	}

	for k := range unknown {
		out.Unknown = append(out.Unknown, k)
	}
	sort.Strings(out.Unknown)
	return out, nil
}

// known reports whether a source has a declared priority.
func known(source string) bool {
	for _, s := range Precedence {
		if s == source {
			return true
		}
	}
	return false
}

// declaredKeys is the set every layer's keys are checked against.
func declaredKeys() map[string]bool {
	keys := Keys()
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

// baselineLayer renders every section's baseline for a mode into one layer.
//
// Derived from the same walk that derives the keys, so a baseline cannot carry a key the registry
// does not know or miss one it does. Two walks would let a section's declared keys and its declared
// defaults disagree, which is a shape of drift nothing downstream could see.
func baselineLayer(mode Mode) (Layer, error) {
	out := Layer{Source: defaultLayer, Values: map[string]any{}}
	for _, s := range Sections() {
		if s.Defaults == nil {
			return out, fmt.Errorf("section %q has no baseline function", s.Name)
		}
		values, err := sectionValues(s.Name, s.Defaults(mode))
		if err != nil {
			return out, fmt.Errorf("section %q baseline for mode %q: %w", s.Name, mode, err)
		}
		for k, v := range values {
			out.Values[k] = v
		}
	}
	return out, nil
}

// sectionValues walks a section's struct instance and returns its per-key values.
//
// The mirror of deriveKeys: same traversal, same tag rules, values instead of names. Sharing the
// traversal is what guarantees the two agree.
func sectionValues(section string, cfg any) (map[string]any, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no value")
	}
	v := reflect.ValueOf(cfg)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, fmt.Errorf("nil %s", v.Type())
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%s is not a struct", v.Kind())
	}
	out := map[string]any{}
	if err := walkValues(v, section, out); err != nil {
		return nil, err
	}
	return out, nil
}

// walkValues collects the values a struct declares under prefix.
func walkValues(v reflect.Value, prefix string, out map[string]any) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag, squash, err := tagOf(f, prefix)
		if err != nil {
			return err
		}

		fv := v.Field(i)
		for fv.Kind() == reflect.Ptr {
			if fv.IsNil() {
				// A nil pointer contributes nothing rather than a zero value, so a section with an
				// unset optional subtree does not claim defaults it does not have.
				fv = reflect.Value{}
				break
			}
			fv = fv.Elem()
		}
		if !fv.IsValid() {
			continue
		}

		if squash {
			if err := walkValues(fv, prefix, out); err != nil {
				return err
			}
			continue
		}
		path := prefix + "." + tag
		if fv.Kind() == reflect.Struct && !isLeaf(fv.Type()) {
			if err := walkValues(fv, path, out); err != nil {
				return err
			}
			continue
		}
		out[path] = fv.Interface()
	}
	return nil
}

// EnvLayer reads the declared keys an environment supplies, as one layer.
//
// Driven by the declared set rather than by the environment, because the environment cannot be
// enumerated for a prefix: a variable is only findable if you already know its name. That direction
// is also what makes this layer complete, since every declared key has exactly one canonical
// spelling and this asks for all of them.
func EnvLayer(lookup func(string) (string, bool)) Layer {
	out := Layer{Source: "env", Values: map[string]any{}}
	for _, key := range Keys() {
		if v, ok := lookup(EnvName(key)); ok && v != "" {
			out.Values[key] = v
		}
	}
	return out
}

// FileLayer takes the keys a configuration file supplied, normalised to lower case.
//
// Normalised here because a source enumerates lower-cased while a file may not be written that way,
// and a key that differed only in case would resolve as unknown while the operator's value went
// nowhere.
func FileLayer(values map[string]any) Layer {
	out := Layer{Source: "file", Values: make(map[string]any, len(values))}
	for k, v := range values {
		out.Values[strings.ToLower(k)] = v
	}
	return out
}
