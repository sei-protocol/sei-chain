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
	// Name identifies the section. A lookup, a report and a defect are keyed by it, and for most
	// sections it is also the first segment of every key.
	Name string
	// Prefix is the first segment of every key this section declares, and is empty for a section whose
	// keys sit at the root of the file with no section of their own.
	//
	// Separate from Name because the two do different jobs. A node-wide setting such as the pruning
	// strategy is written at the top of app.toml and read as "pruning", so it has no segment to take a
	// name from, and it still needs one to be looked up and reported under.
	Prefix string
	// Keys are the dotted paths this section declares, sorted.
	Keys []string
	// Excluded are dotted paths the struct carries that this section deliberately does not declare,
	// sorted.
	//
	// Kept rather than discarded because both walks have to agree. The type walk decides what is declared
	// and the value walk decides what is stated, and a path dropped from one and not the other makes a
	// section that either declares a key nothing answers or answers a key it never declared.
	Excluded []string
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
	record(name, name, prototype, defaults, nil)
}

// RegisterSectionExcluding records a section, leaving out paths the struct carries that are not settings.
//
// Each excluding path is relative to the section, so "max-outbound-connections" rather than the dotted key
// it becomes. A path matching nothing the struct declares is refused, because an exclusion covering
// nothing reads as though it covered something.
//
// Two kinds of field earn this. One a reader refuses outright, where writing the key stops the node, so
// declaring it would put a setting in the space whose only effect is an outage. And one whose absence is
// itself the setting, where any default would be this package inventing one.
func RegisterSectionExcluding(name string, prototype any, defaults func(Mode) any, excluding ...string) {
	record(name, name, prototype, defaults, excluding)
}

// RegisterRootKeys records a section whose keys sit at the root of the file, with no section of their own.
//
// name identifies the section for lookups and reports and is not part of any key. Everything else matches
// RegisterSection: the keys come from the mapstructure tags, and the tags are the only spelling.
//
// Some settings are node-wide and are written at the top of a file rather than inside a table. Giving them
// a section would rename them, and a renamed key is one an operator's existing file no longer reaches.
func RegisterRootKeys(name string, prototype any, defaults func(Mode) any) {
	record(name, "", prototype, defaults, nil)
}

// RegisterRootKeysExcluding records a root-key section, leaving out paths that are not settings.
//
// RegisterSectionExcluding says which fields earn an exclusion and how a path is spelled.
func RegisterRootKeysExcluding(name string, prototype any, defaults func(Mode) any, excluding ...string) {
	record(name, "", prototype, defaults, excluding)
}

// record is the one path both registrations take.
func record(name, prefix string, prototype any, defaults func(Mode) any, excluding []string) {
	found, err := deriveKeys(name, prefix, prototype)
	keys, excluded := found.keys, []string(nil)
	if err == nil {
		keys, excluded, err = withoutExcluded(prefix, keys, excluding)
	}
	if err == nil {
		err = refuseDeclaredInterfaces(keys, found.interfaces)
	}

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
		sections[name] = Section{
			Name: name, Prefix: prefix, Keys: keys, Excluded: excluded, Defaults: defaults,
		}
	}
}

// refuseDeclaredInterfaces refuses a declared path whose field holds an interface.
//
// What a decoder writes into an interface depends on what the field already holds rather than on the
// field's type, so two structs of one type can accept and refuse the same written value. A caller that
// rehearses a decode into a copy to learn whether the real one will succeed gets an answer about the copy,
// and the two differ exactly where their existing values do.
//
// Checked against the declared paths and not the struct's fields, because a section may exclude such a
// field. An excluded path is not declared, and how a path nobody can write decodes is not a property worth
// refusing.
func refuseDeclaredInterfaces(keys, interfaces []string) error {
	if len(interfaces) == 0 {
		return nil
	}
	declared := make(map[string]bool, len(keys))
	for _, key := range keys {
		declared[key] = true
	}
	var bad []string
	for _, key := range interfaces {
		if declared[key] {
			bad = append(bad, key)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("%v name fields holding an interface, so what a written value decodes to "+
			"depends on what the field already holds and not on the field's type", bad)
	}
	return nil
}

// withoutExcluded splits derived paths into the ones a section declares and the ones it does not.
//
// An exclusion is spelled relative to the section, so this is where it becomes the dotted key both walks
// compare against. One matching no derived path is refused: the field it named was renamed or removed, and
// an exclusion for a field that is gone stops excluding anything while still reading as a deliberate
// omission.
func withoutExcluded(prefix string, derived, excluding []string) (keys, excluded []string, err error) {
	if len(excluding) == 0 {
		return derived, nil, nil
	}
	drop := make(map[string]bool, len(excluding))
	for _, rel := range excluding {
		key := rel
		if prefix != "" {
			key = prefix + "." + rel
		}
		drop[key] = true
	}
	for _, key := range derived {
		if drop[key] {
			excluded = append(excluded, key)
			delete(drop, key)
			continue
		}
		keys = append(keys, key)
	}
	if len(drop) > 0 {
		missing := make([]string, 0, len(drop))
		for key := range drop {
			missing = append(missing, key)
		}
		sort.Strings(missing)
		return nil, nil, fmt.Errorf("%v is excluded and the struct declares no such key, so the "+
			"exclusion covers nothing", missing)
	}
	return keys, excluded, nil
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
		other, taken := spellings[env]
		switch {
		case taken && other == key:
			// Two sections declaring one key, which a prefix made impossible and a key at the root of the
			// file does not. One section's default renders over the other's and which one depends on the
			// order the sections are walked, so the value a node runs is decided by nothing an operator
			// or a reviewer can see. Named as the one key it is, because the spelling reason below is not
			// the reason here.
			return fmt.Errorf("%q is declared by two sections; one default renders over the other and "+
				"which one wins depends on the order the sections are walked", key)
		case taken:
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
func deriveKeys(name, prefix string, prototype any) (derived, error) {
	if name == "" {
		return derived{}, fmt.Errorf("section name is empty")
	}
	if name != strings.ToLower(name) {
		return derived{}, fmt.Errorf("section name %q is not lower case; configuration sources "+
			"enumerate lower-cased, so a key under it would never match a written one", name)
	}
	if bad, found := unaddressableChar(name); found {
		return derived{}, fmt.Errorf("section name %q carries %q, and a section is one segment. A dotted name "+
			"declares keys inside another section's subtree, where the two sections' defaults land in "+
			"one map and whichever renders last silently wins; a space cannot be written in an "+
			"environment variable name at all", name, bad)
	}
	if prototype == nil {
		return derived{}, fmt.Errorf("no struct")
	}
	t := reflect.TypeOf(prototype)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return derived{}, fmt.Errorf("%s is not a struct", t.Kind())
	}

	var found derived
	if err := walk(t, prefix, &found, map[reflect.Type]bool{}); err != nil {
		return derived{}, err
	}
	keys := found.keys
	if len(keys) == 0 {
		return derived{}, fmt.Errorf("declares no keys")
	}
	sort.Strings(keys)
	sort.Strings(found.interfaces)
	// A path two fields both produce leaves one of them unreachable, and which one is not
	// observable: the value walk writes them into one map. That is the unaddressable-key failure
	// this package exists to refuse, so it cannot be allowed to arrive through the package itself.
	for i := 1; i < len(keys); i++ {
		if keys[i] == keys[i-1] {
			return derived{}, fmt.Errorf("two fields both declare %q, so one of them is unreachable and "+
				"which one is not observable", keys[i])
		}
	}
	found.keys = keys
	return found, nil
}

// derived is what one walk of a section's type collects.
//
// Two lists rather than one, because a path whose field holds an interface is not refused where it is
// found. A section may exclude it, and an excluded path is not declared, so nothing about how it decodes
// matters. The refusal belongs after the exclusions are known.
type derived struct {
	keys []string
	// interfaces are paths whose field holds an interface, sorted with the keys they appear among.
	interfaces []string
}

// walk appends the dotted keys a struct declares under prefix.
//
// open carries the struct types on the current path, so a self-referential one is refused rather than
// recursed into. A stack overflow cannot be recovered into a Defect, so this is the one refusal that
// has to happen before the recursion rather than after it.
func walk(t reflect.Type, prefix string, found *derived, open map[reflect.Type]bool) error {
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

		tag, squash, skip, err := tagOf(f, prefix)
		if err != nil {
			return err
		}
		if skip {
			continue
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
			if err := walkSubtree(ft, prefix, join(prefix, f.Name), found, open); err != nil {
				return err
			}
			continue
		}

		path := join(prefix, tag)
		if ft.Kind() == reflect.Struct && !isLeaf(ft) {
			if err := walkSubtree(ft, path, join(prefix, f.Name), found, open); err != nil {
				return err
			}
			continue
		}
		found.keys = append(found.keys, path)
		// Recorded rather than refused. An excluded path is not declared, so how it decodes never
		// matters; record decides, once the exclusions are known.
		if ft.Kind() == reflect.Interface {
			found.interfaces = append(found.interfaces, path)
		}
	}
	return nil
}

// join appends a key segment to a prefix, and returns the segment alone when there is no prefix.
func join(prefix, segment string) string {
	if prefix == "" {
		return segment
	}
	return prefix + "." + segment
}

// walkSubtree appends the keys a struct-typed field declares, and refuses one that declares none.
//
// A struct configuration cannot reach is a setting an operator writes into nothing. A defined type
// over a leaf, an empty struct, and a struct whose every field is unexported all arrive here having
// contributed nothing, and both walks agree about it, so no later check can see the loss.
func walkSubtree(t reflect.Type, path, field string, found *derived, open map[reflect.Type]bool) error {
	before := len(found.keys)
	if err := walk(t, path, found, open); err != nil {
		return err
	}
	if len(found.keys) == before {
		return fmt.Errorf("%s is a %s that declares no key, so configuration cannot reach it", field, t)
	}
	return nil
}

// tagOf returns a field's mapstructure name, or reports that the field cannot be addressed.
func tagOf(f reflect.StructField, prefix string) (name string, squash, skip bool, err error) {
	tag, ok := f.Tag.Lookup("mapstructure")
	if !ok {
		return "", false, false, fmt.Errorf("%s.%s has no mapstructure tag; a key derived from a field "+
			"name is a key no operator writes, which is how ninety-two legacy keys became "+
			"unreachable through their tags", prefix, f.Name)
	}

	name, squash, remain := parseTag(tag)
	if remain {
		return "", false, true, nil
	}
	if squash {
		if name != "" {
			return "", false, false, fmt.Errorf("%s.%s is squashed and also names %q; one or the other",
				prefix, f.Name, name)
		}
		return "", true, false, nil
	}
	if assignedOutsideConfiguration(name) {
		return "", false, true, nil
	}
	if name == "" {
		return "", false, false, fmt.Errorf("%s.%s has an empty mapstructure name", prefix, f.Name)
	}
	if bad, found := unaddressableChar(name); found {
		return "", false, false, fmt.Errorf("%s.%s names %q, which carries %q. A dot makes the field claim a "+
			"subtree the struct does not have, and neither a dot nor a space survives a round trip "+
			"through a configuration source", prefix, f.Name, name, bad)
	}
	if neverMatchesAWrittenKey(name) {
		return "", false, false, fmt.Errorf("%s.%s names %q, which is not lower case; a configuration "+
			"source enumerates lower-cased, so this key would never match a written one",
			prefix, f.Name, name)
	}
	return name, false, false, nil
}

// parseTag splits a mapstructure tag into the name it gives a field and the options that change what the
// field is.
//
// A squashed field contributes its own fields at this level rather than a segment of its own. A remaining
// field is where the decode puts what it matched no field for, so it declares no key: what lands in it is
// what an operator misspelled, and giving it a key would offer the collector itself as a setting.
func parseTag(tag string) (name string, squash, remain bool) {
	parts := strings.Split(tag, ",")
	for _, opt := range parts[1:] {
		switch opt {
		case "squash":
			squash = true
		case "remain":
			remain = true
		}
	}
	return parts[0], squash, remain
}

// assignedOutsideConfiguration reports whether a tag excludes its field from configuration.
//
// Something else in the program assigns such a field, so no reader resolves a key for it, and declaring
// one would put a key in the space that reaches no field. An untagged field is refused for the same
// reason read from the other end: it would declare a key derived from a field name, which is a key no
// operator writes. The two look alike in a diff and mean opposite things.
func assignedOutsideConfiguration(name string) bool {
	return name == "-"
}

// neverMatchesAWrittenKey reports whether a key segment is spelled so that no written value can reach it.
//
// A configuration source enumerates its keys lower-cased, so a segment carrying an upper-case letter is
// one an operator can write and nothing answers.
func neverMatchesAWrittenKey(name string) bool {
	return name != strings.ToLower(name)
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
	decodedNotLookedUp = map[string]string{}
	defer mu.Unlock()
	sections = map[string]Section{}
	defects = nil
	envCannotDeliver = map[string]string{}
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
