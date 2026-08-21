package registry

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Mode is a node's declared role, and the input a section's default varies on.
//
// Declared here rather than imported from app/params so this package stays a leaf that any
// feature package can import. TestModesMatchTheNodeModes holds the two sets against each
// other, so a mode added there fails here rather than silently having no default.
type Mode string

// The modes a default may vary on.
const (
	ModeValidator Mode = "validator"
	ModeFull      Mode = "full"
	ModeSeed      Mode = "seed"
	ModeArchive   Mode = "archive"
)

// Modes returns every mode a default is asked for, in a fixed order.
func Modes() []Mode { return []Mode{ModeValidator, ModeFull, ModeSeed, ModeArchive} }

// IsFullnodeMode reports whether a node of this kind serves queries to callers other than itself.
//
// Stated here because more than one package needs it and they sit on opposite sides of an import edge.
// The package that owns the node a binary was started as also owns the type that describes it, and a
// section's own package needs the same fact to state a default that varies on it while being imported by
// that package rather than importing it.
//
// An archive node counts. It serves queries, which is the property this names, and it is the mode most
// easily forgotten when the rule is written out by hand.
func IsFullnodeMode(mode Mode) bool { return mode == ModeFull || mode == ModeArchive }

// Section is one registered configuration section.
type Section struct {
	// Name is the section's own segment, and the first segment of every key it declares.
	Name string
	// Keys are the dotted paths this section declares, sorted.
	Keys []string
	// Defaults returns the section's default for a mode.
	Defaults func(Mode) any
}

// Defect is a registration this package cannot use.
//
// Recorded rather than panicked. A panic here runs during package initialisation of a package
// every feature imports, so it would take down every seid invocation including --help, and it
// converts a compile-time-fixable mistake into a fleet-wide incident. Defects is what a package's
// own test reads to turn these into a failure.
type Defect struct {
	// Section is the name the registration was made under, empty if that was the problem.
	Section string
	// Err says what is wrong.
	Err error
}

var (
	mu       sync.RWMutex
	sections = map[string]Section{}
	defects  []Defect
)

// RegisterSection records a section's struct and its per-mode default.
//
// prototype is read for its fields and their tags and never for its values, which is why it is the
// reader's own struct rather than a copy: a second struct would be a second statement of the same
// key set, and the two would disagree the first time somebody edited one.
//
// The dotted key for every field is derived from the section name and the field's mapstructure
// tag. Nothing here reads a hand-typed key string, which is the property that makes template
// and reader drift impossible rather than test-guarded.
//
// It never panics. A registration this package cannot use is recorded as a Defect and the
// section is not registered.
func RegisterSection(name string, prototype any, defaults func(Mode) any) {
	keys, err := deriveKeys(name, prototype)

	mu.Lock()
	defer mu.Unlock()
	switch {
	case err != nil:
		defects = append(defects, Defect{Section: name, Err: err})
	case defaults == nil:
		defects = append(defects, Defect{Section: name, Err: fmt.Errorf("no defaults function")})
	default:
		if _, dup := sections[name]; dup {
			defects = append(defects, Defect{Section: name, Err: fmt.Errorf("section registered twice")})
			return
		}
		if err := envNamesAreDistinct(keys); err != nil {
			defects = append(defects, Defect{Section: name, Err: err})
			return
		}
		sections[name] = Section{Name: name, Keys: keys, Defaults: defaults}
	}
}

// envNamesAreDistinct refuses keys that share one environment spelling. Callers hold mu.
//
// Dots and hyphens both become underscores, so two keys differing only in that punctuation answer to
// one variable and one of them can never be set from the environment. Checked against the sections
// already registered too, because the collision does not have to be inside one section.
func envNamesAreDistinct(adding []string) error {
	// Only the keys arriving are checked. Every registration passes through here, so the keys already
	// registered are already known distinct from each other.
	spellings := map[string]string{}
	for _, s := range sections {
		for _, key := range s.Keys {
			spellings[EnvName(key)] = key
		}
	}
	for _, key := range adding {
		env := EnvName(key)
		if other, taken := spellings[env]; taken {
			return fmt.Errorf("%q and %q both answer to %s, because a dot and a hyphen are the same "+
				"character to the environment, so one of them can never be set from it", other, key, env)
		}
		spellings[env] = key
	}
	return nil
}

// Sections returns every registered section, sorted by name.
func Sections() []Section {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Section, 0, len(sections))
	for _, s := range sections {
		s.Keys = append([]string(nil), s.Keys...)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Lookup returns a registered section.
func Lookup(name string) (Section, bool) {
	mu.RLock()
	defer mu.RUnlock()
	s, ok := sections[name]
	// Copied, so a caller sorting or writing into Keys cannot reach the registry's own storage from
	// outside the mutex. Defects copies for the same reason.
	s.Keys = append([]string(nil), s.Keys...)
	return s, ok
}

// Defects returns every registration this package could not use.
func Defects() []Defect {
	mu.RLock()
	defer mu.RUnlock()
	return append([]Defect(nil), defects...)
}

// Keys returns every declared key across every section, sorted.
func Keys() []string {
	all := Sections()
	out := make([]string, 0, len(all)*4)
	for _, s := range all {
		out = append(out, s.Keys...)
	}
	sort.Strings(out)
	return out
}

// deriveKeys walks a section's struct and returns the dotted keys it declares.
//
// An untagged field is an error rather than a fallback to the field's name, and that is the one
// decision in this file worth reading twice. mapstructure, which is what binds a value to a field on
// the path this replaces, does fall back. That is why an embedded srvconfig.Config with no tag put
// whole cosmos sections under a type-name prefix nothing writes, and why MemIAVLConfig's leaves sit
// outside state-commit.flatkv.*. Ninety-two operator-facing keys reach their field only through a
// spelling the tags do not produce, and a silent fallback is what made that invisible. Refusing to
// guess is what keeps the tag authoritative.
func deriveKeys(section string, prototype any) ([]string, error) {
	if section == "" {
		return nil, fmt.Errorf("section name is empty")
	}
	if section != strings.ToLower(section) {
		return nil, fmt.Errorf("section name %q is not lower case; configuration sources "+
			"enumerate lower-cased, so a key under it would never match a written one", section)
	}
	if bad, found := unaddressableChar(section); found {
		return nil, fmt.Errorf("section name %q carries %q, and a section is one segment. A dotted name "+
			"declares keys inside another section's subtree, where the two sections' defaults land in "+
			"one map and whichever renders last silently wins; a space cannot be written in an "+
			"environment variable name at all", section, bad)
	}
	if prototype == nil {
		return nil, fmt.Errorf("no struct")
	}
	t := reflect.TypeOf(prototype)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%s is not a struct", t.Kind())
	}

	var keys []string
	if err := walk(t, section, &keys, map[reflect.Type]bool{}); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("declares no keys")
	}
	sort.Strings(keys)
	// A path two fields both produce leaves one of them unreachable, and which one is not
	// observable: the value walk writes them into one map. That is the unaddressable-key failure
	// this package exists to refuse, so it cannot be allowed to arrive through the package itself.
	for i := 1; i < len(keys); i++ {
		if keys[i] == keys[i-1] {
			return nil, fmt.Errorf("two fields both declare %q, so one of them is unreachable and "+
				"which one is not observable", keys[i])
		}
	}
	return keys, nil
}

// walk appends the dotted keys a struct declares under prefix.
//
// open carries the struct types on the current path, so a self-referential one is refused rather than
// recursed into. A stack overflow cannot be recovered into a Defect, so this is the one refusal that
// has to happen before the recursion rather than after it.
func walk(t reflect.Type, prefix string, keys *[]string, open map[reflect.Type]bool) error {
	if open[t] {
		return fmt.Errorf("%s is %s, which contains itself; a key space derived from it has no end",
			prefix, t)
	}
	open[t] = true
	defer delete(open, t)

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			// An unexported field with no tag is a struct's private state and declares nothing. One
			// carrying a tag is a contradiction: reflection cannot write to it, so the tag names a key
			// that reaches nothing, and an embedded unexported type with ,squash loses its whole subtree
			// this way with no other sign.
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

		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}

		// A squashed field promotes its own fields to this level, which is how a section carries
		// a shared base without adding a segment.
		if squash {
			if ft.Kind() != reflect.Struct {
				return fmt.Errorf("%s.%s is squashed but is a %s, not a struct", prefix, f.Name, ft.Kind())
			}
			if err := walkSubtree(ft, prefix, prefix+"."+f.Name, keys, open); err != nil {
				return err
			}
			continue
		}

		path := prefix + "." + tag
		if ft.Kind() == reflect.Struct && !isLeaf(ft) {
			if err := walkSubtree(ft, path, prefix+"."+f.Name, keys, open); err != nil {
				return err
			}
			continue
		}
		*keys = append(*keys, path)
	}
	return nil
}

// walkSubtree appends the keys a struct-typed field declares, and refuses one that declares none.
//
// A struct configuration cannot reach is a setting an operator writes into nothing. A defined type
// over a leaf, an empty struct, and a struct whose every field is unexported all arrive here having
// contributed nothing, and both walks agree about it, so no later check can see the loss.
func walkSubtree(t reflect.Type, path, field string, keys *[]string, open map[reflect.Type]bool) error {
	before := len(*keys)
	if err := walk(t, path, keys, open); err != nil {
		return err
	}
	if len(*keys) == before {
		return fmt.Errorf("%s is a %s that declares no key, so configuration cannot reach it", field, t)
	}
	return nil
}

// tagOf returns a field's mapstructure name, or reports that the field cannot be addressed.
func tagOf(f reflect.StructField, prefix string) (name string, squash bool, err error) {
	tag, ok := f.Tag.Lookup("mapstructure")
	if !ok {
		return "", false, fmt.Errorf("%s.%s has no mapstructure tag; a key derived from a field "+
			"name is a key no operator writes, which is how ninety-two legacy keys became "+
			"unreachable through their tags", prefix, f.Name)
	}

	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, opt := range parts[1:] {
		if opt == "squash" {
			squash = true
		}
	}
	if squash {
		if name != "" {
			return "", false, fmt.Errorf("%s.%s is squashed and also names %q; one or the other",
				prefix, f.Name, name)
		}
		return "", true, nil
	}
	if name == "" || name == "-" {
		return "", false, fmt.Errorf("%s.%s has an empty mapstructure name", prefix, f.Name)
	}
	if bad, found := unaddressableChar(name); found {
		return "", false, fmt.Errorf("%s.%s names %q, which carries %q. A dot makes the field claim a "+
			"subtree the struct does not have, and neither a dot nor a space survives a round trip "+
			"through a configuration source", prefix, f.Name, name, bad)
	}
	if name != strings.ToLower(name) {
		return "", false, fmt.Errorf("%s.%s names %q, which is not lower case; a configuration "+
			"source enumerates lower-cased, so this key would never match a written one",
			prefix, f.Name, name)
	}
	return name, false, nil
}

// unaddressableChar returns the first character in a key segment that no configuration source can
// carry, and whether there was one.
//
// A dot separates segments, so one inside a segment names a level that does not exist. A space
// survives neither a file, an environment variable name, nor a flag.
//
// One function for both places a segment enters, a section name and a field tag, because the two
// answer to the same sources. Stated separately they drifted, and a section name accepted a space the
// field rule refused.
func unaddressableChar(segment string) (string, bool) {
	if i := strings.IndexAny(segment, ". "); i >= 0 {
		return segment[i : i+1], true
	}
	return "", false
}

// isLeaf reports whether a struct type is a value rather than a group of keys.
//
// time.Time and its like carry exported fields but are decoded whole, so walking into them
// would invent keys no operator writes.
func isLeaf(t reflect.Type) bool {
	switch t.String() {
	case "time.Time", "big.Int":
		return true
	}
	return false
}

// Reset clears the registry. For tests only, so one test's registrations cannot leak into
// another's declared set.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	sections = map[string]Section{}
	defects = nil
}

// envPrefix is the environment namespace for every derived key.
//
// A constant rather than path.Base(os.Executable()), which is what the legacy path uses. That
// derivation means renaming the binary moves the entire environment namespace, and it is why
// the legacy path answers to three environment universes at once.
const envPrefix = "SEID"

// EnvName returns the one environment variable that delivers a key.
//
// Derived from the key rather than declared, so a section cannot carry a spelling its
// environment variable does not match. Dots and hyphens both become underscores, which is the
// replacer the boot installs, so two keys differing only in that punctuation collide; a check
// over the registry is what catches that rather than a reader discovering it.
func EnvName(key string) string {
	return envPrefix + "_" + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(key))
}
