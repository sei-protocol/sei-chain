package registry

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Resolved is every declared key's value, plus what a caller has to be told about how it got there.
type Resolved struct {
	// Values carries one value per declared key.
	//
	// A key's Go type depends on which source answered it, and a caller that type-asserts has to expect
	// all three. A default arrives as the field's own type, so a duration is a duration and a list is a
	// list. A file arrives as whatever the file format decodes to, so the same duration is text and the
	// same list is a list of untyped elements. An environment variable arrives as one string, always. This
	// resolves values and does not convert them, so the reader that owns a key remains the thing that
	// turns any of the three into what that key means.
	//
	// A value is the caller's to write into. Nothing here shares storage with a section's own default.
	Values map[string]any
	// Overrides are the declared keys something other than this node's defaults supplied, sorted.
	//
	// The keys an operator has taken responsibility for, as distinct from the ones tracking the
	// binary's judgement. This is what a diff renders.
	Overrides []string
	// Ignored are declared keys an environment variable was set for and could not supply, sorted.
	//
	// Separate from Unknown because the two are different mistakes. An unknown key is one nothing reads.
	// An ignored one is read, and the operator reached for the one channel that cannot carry it, so the
	// value they wrote elsewhere is what applies. Ignored carries the keys; the reason is the same for all of them, because it is a fact about
	// the channel rather than about any section.
	Ignored []string
	// Unknown are keys a source carried that no section declares, sorted.
	//
	// Reported rather than an error, because what to do about one is the caller's decision: a
	// generate path may want to refuse, while a boot on an operator's existing file must not.
	Unknown []string
}

// Sources are a node's configuration sources other than its defaults, which Resolve derives itself.
//
// A zero field contributes nothing, which is how a caller resolves deliberately without a source.
//
// Named fields rather than positional parameters, because File and Flags are the same type: passed
// positionally, swapping the two compiles and silently inverts the precedence for every key both
// supply. LookupEnv is a function rather than a map because an environment cannot be enumerated for a
// prefix, since a variable is only findable if you already know its name.
type Sources struct {
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

// Resolve reduces a node's configuration sources to one value per declared key.
//
// The precedence is stated once, in this function, and a caller cannot reorder its way to a different
// answer. That is the difference between a declared precedence and an emergent one: the legacy path's
// answer depends on which viper instance a caller asked, which is why two different orders are
// observable across its key set.
//
// Every declared key resolves, because the defaults are a source like any other. A key no source
// mentions therefore carries its mode's default rather than being absent, which is what makes an
// absent key track the binary's judgement instead of a zero value.
//
// That is a property of the whole answer, so a section whose defaults cannot supply it refuses the
// resolution rather than returning one with a hole in it. A caller handed an error has no resolved
// values; a caller handed a Resolved has one for every declared key.
func Resolve(mode Mode, from Sources) (Resolved, error) {
	var out Resolved

	// Refused before anything is resolved, because a section's defaults answer per mode and a mode this
	// package does not know reaches whatever each section does with an argument it cannot match. What that
	// is varies by section and none of them is a decision anyone made: the upstream mode rules answer for
	// an unrecognised mode as though it were a full node, so an empty string, a capitalised name or one
	// with a trailing space resolves the interfaces a full node serves onto whatever asked.
	if !known(mode) {
		return out, fmt.Errorf("%q is not a mode this binary declares defaults for; the modes are %v",
			mode, Modes())
	}

	// One snapshot, read once and passed everywhere below. Every part of the answer has to describe the
	// same registry: asking again leaves a window a concurrent registration fits through, and a section
	// arriving in that window is declared by one part of the answer and not by another.
	registered := Sections()
	defaults, err := defaultValues(mode, registered)
	if err != nil {
		return out, err
	}
	declared := declaredKeys(registered)
	undeliverable := keysNoVariableCanCarry(defaults)

	out.Values = make(map[string]any, len(declared))
	for key, v := range defaults {
		out.Values[key] = v
	}

	overrides := map[string]bool{}
	unknown := map[string]bool{}
	// Lowest precedence first, so a later source overwrites an earlier one. The one statement of the
	// order, which is why nothing exports it.
	fromEnv, ignored := envValues(declared, undeliverable, from.LookupEnv)
	out.Ignored = ignored
	for _, values := range []map[string]any{
		fileValues(from.File),
		fromEnv,
		from.Flags,
	} {
		for key, v := range values {
			if !declared[key] {
				// A key nothing declares cannot be resolved into anything, and silently dropping it is
				// how an operator's typo becomes invisible.
				unknown[key] = true
				continue
			}
			out.Values[key] = v
			overrides[key] = true
		}
	}

	out.Overrides = sortedKeys(overrides)
	out.Unknown = sortedKeys(unknown)
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

// defaultValues renders every section's defaults for a mode into one set of keys.
//
// This is a second walk, not the one that derived the keys. The two share tagOf and isLeaf and
// nothing else, so they can and did disagree: the type walk unwraps a pointer to derive a subtree's
// keys and the value walk skips a nil one. matchesDeclaration is what holds them together, by
// refusing a rendered default that does not state one value per declared key.
func defaultValues(mode Mode, registered []Section) (map[string]any, error) {
	out := map[string]any{}
	for _, s := range registered {
		values, err := sectionValues(s.Prefix, s.Defaults(mode))
		if err != nil {
			return out, fmt.Errorf("section %q default for mode %q: %w", s.Name, mode, err)
		}
		// The same paths the declaration left out. Both walks read the one struct, so a path dropped from
		// the declared side and kept here would be a value under a key nothing declares.
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
		tag, squash, skip, err := tagOf(f, prefix)
		if err != nil {
			return err
		}
		if skip {
			// Skipped on the type side too, so the declared keys and the rendered defaults describe the
			// same set of fields and matchesDeclaration has nothing to disagree about.
			continue
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

// detach returns a value that shares no storage with the one it was given.
//
// A section's default is usually a package-level variable, so handing out a list or a map field hands out
// the storage behind it. One caller sorting what it was given rewrites the process-wide default and every
// later resolution carries the sorted version. That happened here, to two deny lists.
//
// Copied all the way down rather than one level. A list of lists, or a map of lists, shares its inner
// storage through a copy of the outer, so a caller sorting an inner list reaches the default just as
// directly. Nothing declared today has that shape, and the copy runs once per boot over a hundred or so
// keys, so the cost of being thorough here is not measurable and the cost of not being is a defect nobody
// finds twice.
//
// This covers what a section's defaults answer. It is not the guarantee for a value a source supplied: the
// file source hands out its own copies, recursively, where it reads them, and the flag source carries only
// text. A source that grew a channel handing out live storage would have to do the same at its own edge,
// because only the source knows what else holds it.
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

// envValues reads the keys an environment supplies, from the caller's declared set.
//
// Driven by the declared set rather than by the environment, which is also what makes it complete:
// every declared key has exactly one canonical spelling and this asks for all of them.
//
// declared is passed in rather than read here, so this shares Resolve's snapshot. Reading the registry
// again would ask for a key the caller's declared set does not hold, and the answer would come back
// only to be reported as one no section declares.
// keysNoVariableCanCarry returns the declared keys an environment variable cannot supply, with the reason.
//
// One rule rather than a list each section keeps. A variable holds one string per name: that is a value for
// anything read as a single word or number, and by long convention a list of those written with commas
// between them. Nothing conventional puts a structure inside one variable, so a key whose value is a list of
// anything other than single words is not offered this channel.
//
// Derived from what the key resolves to rather than declared beside the section that owns it. A section
// naming its own exceptions is a list somebody keeps in step with the reader, and the first one forgotten is
// a variable that resolves to a string, lands above the file because the environment outranks it, and
// reaches a reader that wanted rows. Deriving it cannot be forgotten, and the reason is the same sentence
// for every key it covers, because it is a fact about the channel and not about the section.
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

// oneVariableCanCarry reports whether a value's shape is one an environment variable can hold.
//
// A single word or number, or a list of those. An element type that is itself a list, a map, or unconstrained
// is not one: a comma-separated string cannot be trusted to become it, and a reader that asks for the exact
// shape gets a string instead and stops the node.
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

func envValues(declared map[string]bool, undeliverable map[string]string,
	lookup func(string) (string, bool)) (map[string]any, []string) {
	if lookup == nil {
		return nil, nil
	}
	out := map[string]any{}
	var ignored []string
	for key := range declared {
		// A key no variable can carry is left to the sources that can. Resolving it would put a string
		// at the top of the order for a reader that takes the exact type, and installing that stops the
		// node. What an operator loses is the channel; what they keep is a node that boots.
		//
		// The variable is still read, and the value still discarded. Asking is what turns this from a
		// silent skip into something a caller can report: a reason nothing can attach to an operator's
		// own action is a reason nobody is ever told.
		if _, refused := undeliverable[key]; refused {
			if v, set := lookup(EnvName(key)); set && v != "" {
				ignored = append(ignored, key)
			}
			continue
		}
		// An empty value is treated as unset. A variable exported empty is far more often a shell
		// artefact than a deliberate empty string, and the two are indistinguishable here. The cost is
		// that clearing a key by exporting it empty reads as touching nothing, and Overrides will not
		// mention it.
		if v, ok := lookup(EnvName(key)); ok && v != "" {
			out[key] = v
		}
	}
	sort.Strings(ignored)
	return out, ignored
}

// fileValues normalises a configuration file's keys to lower case.
//
// A source enumerates lower-cased while a file may not be written that way, and a key that differed
// only in case would resolve as unknown while the operator's value went nowhere.
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
