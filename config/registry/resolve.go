package registry

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Layer is one configuration source's contribution, before precedence is applied.
//
// Source names which layer this is. Values holds that source's own keys and nothing else: a layer that folded in a second source would make provenance unrecoverable,
// which is the property that lets a diagnostic tell a node operator their environment variable beat
// their file.
type Layer struct {
	Source Source
	Values map[string]any
}

// Resolution is one key's resolved value and where it came from.
//
// From is the whole point of resolving through named layers rather than merging maps. The legacy
// path cannot answer it: its layers are combined inside one viper before anything observes them, so
// there is no point at which a value's source is recoverable, and that is why it can never tell an
// operator which source won.
type Resolution struct {
	// Value is what resolved.
	Value any
	// From is the source of the layer that won.
	From Source
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

// Overrides returns the keys whose value came from something other than a default, sorted.
//
// This is what a diff renders: the keys an operator has actually taken responsibility for, as
// distinct from the ones tracking the binary's judgement.
func (r Resolved) Overrides() []string {
	var out []string
	for k, res := range r.Keys {
		if res.From != SourceDefault {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Resolve reduces layers to one value per declared key, in the declared order.
//
// The order is Source's own declaration order, not the order layers are passed, so a caller cannot
// change the outcome by reordering its arguments. That is the difference between a declared
// precedence and an emergent one: the legacy path's answer depends on which viper instance a caller
// asked, which is why two different orders are observable across its key set.
//
// Every declared key resolves, because the default is a layer like any other. A key no layer
// mentions therefore carries its mode's default rather than being absent, which is what makes an
// absent key track the binary's judgement instead of a zero value.
//
// That is a property of the whole answer, so a section whose default cannot supply it refuses the
// resolution rather than returning one with a hole in it. A caller handed an error has no resolved
// values; a caller handed a Resolved has one for every declared key.
func Resolve(mode Mode, layers ...Layer) (Resolved, error) {
	out := Resolved{Keys: map[string]Resolution{}}

	// One snapshot for both, because they have to describe the same registry. Rendering the defaults
	// and then asking for the declared set separately leaves a window a concurrent registration fits
	// through, and a key declared in that window resolves to nothing.
	registered := Sections()
	defaults, err := defaultLayer(mode, registered)
	if err != nil {
		return out, err
	}
	declared := declaredKeys(registered)

	// A Source no constant names is an error rather than a silently ignored layer, since a layer that
	// never contributes is worse than one that fails loudly: nothing downstream could tell it had been
	// dropped.
	bySource := map[Source]Layer{SourceDefault: defaults}
	for _, l := range layers {
		if l.Source == SourceDefault {
			return out, fmt.Errorf("a layer names the reserved source %s; the default is derived from "+
				"the registry, not supplied", SourceDefault)
		}
		if !l.Source.declared() {
			return out, fmt.Errorf("layer %s names no declared source, so it has no defined priority and "+
				"the resolver would have to invent one", l.Source)
		}
		if _, dup := bySource[l.Source]; dup {
			return out, fmt.Errorf("two layers name source %s; one of them would silently lose", l.Source)
		}
		bySource[l.Source] = l
	}

	unknown := map[string]bool{}
	for _, source := range Sources() {
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
			out.Keys[key] = Resolution{Value: v, From: source}
		}
	}

	for k := range unknown {
		out.Unknown = append(out.Unknown, k)
	}
	sort.Strings(out.Unknown)
	return out, nil
}

// declaredKeys is the set every layer's keys are checked against, taken from one snapshot.
func declaredKeys(registered []Section) map[string]bool {
	out := map[string]bool{}
	for _, s := range registered {
		for _, k := range s.Keys {
			out[k] = true
		}
	}
	return out
}

// defaultLayer renders every section's default for a mode into one layer.
//
// This is a second walk, not the one that derived the keys. The two share tagOf and isLeaf and
// nothing else, so they can and did disagree: the type walk unwraps a pointer to derive a subtree's
// keys and the value walk skips a nil one. matchesDeclaration is what holds them together, by
// refusing a rendered default that does not state one value per declared key.
func defaultLayer(mode Mode, registered []Section) (Layer, error) {
	out := Layer{Source: SourceDefault, Values: map[string]any{}}
	for _, s := range registered {
		values, err := sectionValues(s.Name, s.Defaults(mode))
		if err != nil {
			return out, fmt.Errorf("section %q default for mode %q: %w", s.Name, mode, err)
		}
		if err := matchesDeclaration(s.Keys, values); err != nil {
			return out, fmt.Errorf("section %q default for mode %q: %w", s.Name, mode, err)
		}
		for k, v := range values {
			out.Values[k] = v
		}
	}
	return out, nil
}

// matchesDeclaration refuses a rendered default that does not state one value for each declared key.
//
// A declared key its default omits cannot resolve, and neither way of filling the hole is honest. A
// zero value claims a judgement the binary never made, and dropping the key from the declared set
// makes an operator's written value indistinguishable from a typo. An optional subtree left nil is
// how a default arrives short, because the type walk unwraps a pointer to derive its keys and the
// value walk skips a nil one rather than claiming defaults the section does not have.
//
// A default carrying a key the section does not declare means it is not the registered struct's
// type, which is the general case the missing keys are one instance of.
func matchesDeclaration(declared []string, values map[string]any) error {
	stated := make(map[string]bool, len(declared))
	var missing, extra []string
	for _, k := range declared {
		stated[k] = true
		if _, ok := values[k]; !ok {
			missing = append(missing, k)
		}
	}
	for k := range values {
		if !stated[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	switch {
	case len(missing) > 0 && len(extra) > 0:
		return fmt.Errorf("its default states %v while the section declares %v, so the default is not "+
			"the registered struct's type", extra, missing)
	case len(missing) > 0:
		return fmt.Errorf("declares %v and its default states no value for them; a section states a "+
			"value for every key it declares, or declares against a struct without the field", missing)
	case len(extra) > 0:
		return fmt.Errorf("its default states %v, which the section does not declare, so the default "+
			"carries fields the registered struct does not", extra)
	}
	return nil
}

// sectionValues walks a section's struct instance and returns its per-key values.
//
// deriveKeys walks a type and this walks a value, over the same tag rules. They are separate
// recursions, so agreement is checked rather than structural; see matchesDeclaration.
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
			// The guard walk has on the type side, so a default's own struct cannot hide a tagged field
			// the declaration would have refused.
			if _, tagged := f.Tag.Lookup("mapstructure"); tagged {
				return fmt.Errorf("%s.%s is unexported and carries a mapstructure tag; nothing can write "+
					"to it, so the tag names a key that reaches no field", prefix, f.Name)
			}
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
			// The guard walk has on the type side. Without it a default whose type squashes a
			// non-struct reaches NumField on a string, and Resolve panics in a package whose whole
			// posture is that a bad registration never does.
			if fv.Kind() != reflect.Struct {
				return fmt.Errorf("%s.%s is squashed but is a %s, not a struct", prefix, f.Name, fv.Kind())
			}
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
	out := Layer{Source: SourceEnv, Values: map[string]any{}}
	for _, key := range Keys() {
		// An empty value is treated as unset. A variable exported empty is far more often a shell
		// artefact than a deliberate empty string, and the two are indistinguishable here. The cost is
		// that clearing a key by exporting it empty reads as touching nothing, and Overrides will not
		// mention it.
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
	out := Layer{Source: SourceFile, Values: make(map[string]any, len(values))}
	for k, v := range values {
		out.Values[strings.ToLower(k)] = v
	}
	return out
}
