package registry

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Resolved is every declared key's value, plus what a caller has to be told about how it got there.
type Resolved struct {
	// Values carries one value per declared key, unconverted: a default has the field's type, a file
	// value the file format's type, and an environment value is always a string. Values share no storage
	// with a section's defaults.
	Values map[string]any
	// Overrides are the declared keys a source other than the defaults supplied, sorted.
	Overrides []string
	// Ignored maps each declared key an environment variable was set for but cannot carry to the reason.
	Ignored map[string]string
	// Refused are the registry's defects. A refused section is absent from Values, so a file value for
	// one of its keys lands in UnknownInFile. Whether to fail on one is the caller's decision.
	Refused []Defect
	// UnknownInFile are keys the file carried that no section declares, sorted.
	UnknownInFile []string
	// UnknownFromFlags are flag names the caller passed that match no declared key, sorted. Kept apart
	// from UnknownInFile because a caller passing its whole flag set routinely includes non-settings.
	UnknownFromFlags []string
}

// Sources are a node's configuration sources other than its defaults. A zero field contributes nothing.
type Sources struct {
	// File is a flat map keyed by whole dotted paths, matched case-insensitively. Resolve refuses a
	// nested map.
	File      map[string]any
	LookupEnv func(string) (string, bool)
	Flags     map[string]any
}

// known reports whether this package declares defaults for a mode.
func known(mode Mode) bool {
	for _, m := range Modes() {
		if m == mode {
			return true
		}
	}
	return false
}

// Resolve reduces a node's configuration sources to one value per declared key, in rising precedence:
// the mode's defaults, File, LookupEnv, Flags. It returns a value for every declared key or an error.
func Resolve(mode Mode, from Sources) (Resolved, error) {
	var out Resolved

	// Sections' defaults do not agree on what an unknown mode means, so refuse it up front.
	if !known(mode) {
		return out, fmt.Errorf("%q is not a mode this binary declares defaults for; the modes are %v",
			mode, Modes())
	}

	// Every part of the answer comes from this one snapshot, so it describes a single registry.
	registered, refused, _ := snapshot()
	out.Refused = refused

	defaults, err := defaultValues(mode, registered)
	if err != nil {
		return out, err
	}
	declared := declaredKeys(registered)
	if err := refuseANestedFile(from.File, declared); err != nil {
		return out, err
	}
	undeliverable := keysNoVariableCanCarry(defaults)

	out.Values = make(map[string]any, len(declared))
	for key, v := range defaults {
		out.Values[key] = v
	}

	overrides := map[string]bool{}
	unknownInFile := map[string]bool{}
	unknownFromFlags := map[string]bool{}

	fromEnv, ignored := envValues(declared, undeliverable, from.LookupEnv)
	out.Ignored = ignored

	// resolveFrom writes one source's values over what is resolved so far, and collects the keys it
	// carried that no section declares.
	resolveFrom := func(values map[string]any, undeclared map[string]bool) {
		for key, v := range values {
			if !declared[key] {
				undeclared[key] = true
				continue
			}
			out.Values[key] = v
			overrides[key] = true
		}
	}

	// Lowest precedence first. The environment is only asked for declared keys, so it has no unknowns.
	resolveFrom(fileValues(from.File), unknownInFile)
	resolveFrom(fromEnv, map[string]bool{})
	resolveFrom(from.Flags, unknownFromFlags)

	out.Overrides = sortedKeys(overrides)
	out.UnknownInFile = sortedKeys(unknownInFile)
	out.UnknownFromFlags = sortedKeys(unknownFromFlags)
	return out, nil
}

// sortedKeys returns a set's members in order.
func sortedKeys(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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

// defaultValues renders every section's defaults for a mode into one set of keys, refusing a section
// whose rendered default does not match its declared keys.
func defaultValues(mode Mode, registered []Section) (map[string]any, error) {
	out := map[string]any{}
	for _, s := range registered {
		values, err := sectionValues(s.Prefix, s.Defaults(mode))
		if err != nil {
			return out, fmt.Errorf("section %q default for mode %q: %w", s.Name, mode, err)
		}
		for _, key := range s.Excluded {
			delete(values, key)
		}
		if err := matchesDeclaration(s.Keys, values); err != nil {
			return out, fmt.Errorf("section %q default for mode %q: %w", s.Name, mode, err)
		}
		for k, v := range values {
			out[k] = v
		}
	}
	return out, nil
}

// matchesDeclaration refuses a rendered default that does not state exactly one value per declared key.
// A nil optional subtree is the usual way a default comes up short.
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

// sectionValues walks a section's struct value and returns its per-key values, using the same tag
// rules as deriveKeys.
func sectionValues(section string, cfg any) (map[string]any, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no value")
	}
	v := reflect.ValueOf(cfg)
	for v.Kind() == reflect.Pointer {
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
			if _, tagged := f.Tag.Lookup("mapstructure"); tagged {
				return fmt.Errorf("%s.%s is unexported and carries a mapstructure tag; nothing can write "+
					"to it, so the tag names a key that reaches no field", prefix, f.Name)
			}
			continue
		}
		tag, squash, skip, err := tagOf(f, prefix)
		if err != nil {
			return err
		}
		if skip {
			continue
		}

		fv := v.Field(i)
		for fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				// A nil pointer contributes nothing rather than zero values.
				fv = reflect.Value{}
				break
			}
			fv = fv.Elem()
		}
		if !fv.IsValid() {
			continue
		}

		if squash {
			if fv.Kind() != reflect.Struct {
				return fmt.Errorf("%s.%s is squashed but is a %s, not a struct", prefix, f.Name, fv.Kind())
			}
			if err := walkValues(fv, prefix, out); err != nil {
				return err
			}
			continue
		}
		path := join(prefix, tag)
		if fv.Kind() == reflect.Struct && !isLeaf(fv.Type()) {
			if err := walkValues(fv, path, out); err != nil {
				return err
			}
			continue
		}
		out[path] = detach(fv)
	}
	return nil
}

// detach returns a deep copy of v's slices and maps, so a caller mutating a resolved default cannot
// reach the section's package-level default.
func detach(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Slice:
		if v.IsNil() {
			return v.Interface()
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(reflect.ValueOf(detach(v.Index(i))))
		}
		return out.Interface()
	case reflect.Map:
		if v.IsNil() {
			return v.Interface()
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, key := range v.MapKeys() {
			out.SetMapIndex(key, reflect.ValueOf(detach(v.MapIndex(key))))
		}
		return out.Interface()
	case reflect.Interface:
		if v.IsNil() {
			return v.Interface()
		}
		return detach(v.Elem())
	default:
		return v.Interface()
	}
}

// keysNoVariableCanCarry returns the declared keys whose default's shape an environment variable cannot
// hold, with the reason. See oneVariableCanCarry.
func keysNoVariableCanCarry(defaults map[string]any) map[string]string {
	out := map[string]string{}
	for key, value := range defaults {
		if value == nil {
			continue
		}
		t := reflect.TypeOf(value)
		if oneVariableCanCarry(t) {
			continue
		}
		out[key] = fmt.Sprintf("this setting is a %s, and a variable holds one string: a single value, or "+
			"conventionally a list of single values written with commas between them", t)
	}
	return out
}

// oneVariableCanCarry reports whether an environment variable can hold a value of type t: a single word
// or number, or a list of those.
func oneVariableCanCarry(t reflect.Type) bool {
	if isSingleValue(t.Kind()) {
		return true
	}
	return t.Kind() == reflect.Slice && isSingleValue(t.Elem().Kind())
}

// isSingleValue reports whether a kind is one word or number.
func isSingleValue(k reflect.Kind) bool {
	switch k {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// envValues looks up each declared key's environment variable. It returns the values found, and the
// keys in undeliverable whose variable was set anyway, with the reason it was ignored.
func envValues(declared map[string]bool, undeliverable map[string]string,
	lookup func(string) (string, bool)) (map[string]any, map[string]string) {
	if lookup == nil {
		return nil, nil
	}
	out := map[string]any{}
	ignored := map[string]string{}
	for key := range declared {
		if reason, refused := undeliverable[key]; refused {
			if v, set := lookup(EnvName(key)); set && v != "" {
				ignored[key] = reason
			}
			continue
		}
		// An empty variable is treated as unset.
		if v, ok := lookup(EnvName(key)); ok && v != "" {
			out[key] = v
		}
	}
	if len(ignored) == 0 {
		return out, nil
	}
	return out, ignored
}

// refuseANestedFile refuses a file source holding a table where a declared key's prefix sits, which
// means the caller passed a decoded file without flattening it. A declared map-valued key is allowed.
func refuseANestedFile(values map[string]any, declared map[string]bool) error {
	for key, v := range values {
		if _, isTable := v.(map[string]any); !isTable {
			continue
		}
		lower := strings.ToLower(key)
		if declared[lower] {
			continue
		}
		for d := range declared {
			if strings.HasPrefix(d, lower+".") {
				return fmt.Errorf("the file source holds a table at %q, where this reads one flat map "+
					"of whole dotted keys such as %q", key, d)
			}
		}
	}
	return nil
}

// fileValues normalises a configuration file's keys to lower case, matching how sources enumerate.
func fileValues(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for k, v := range values {
		out[strings.ToLower(k)] = v
	}
	return out
}
