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
// it becomes. A path matching nothing the struct declares is refused, because an exclusion covering nothing
// reads as though it covered something.
//
// Two kinds of field earn this. One a reader refuses outright, where writing the key stops the node, so
// declaring it would put a setting in the space whose only effect is an outage. And one whose absence is
// itself the setting, where any default would be this package inventing one.
//
// A field can also carry "-" as its mapstructure name, and that says something different rather than the
// same thing in another place. The tag says the field is not configuration and no reader offers it. An
// exclusion says the opposite about the field and less about this package: the field is configuration, the
// reader that owns its file decodes it, and this key space does not offer it. Tagging one of these paths
// would take the setting away from that reader.
//
// The tag is a statement of intent and not a barrier. The decoder in use honours "-" when it renders a
// struct into a map and not when it decodes a map into a struct, where it takes the name literally, so a
// key spelled "-" reaches every field in the struct tagged that way at once. Nothing offers such a key and
// no operator writes one, which is why this is stated rather than guarded.
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
	keys, err := deriveKeys(name, prefix, prototype)
	var excluded []string
	if err == nil {
		keys, excluded, err = withoutExcluded(prefix, keys, excluding)
	}
	// Refused after the exclusions are known, because deriveKeys can only see a struct that declares
	// nothing and this catches the section whose every declared path was then excluded. Such a section
	// answers no key while still taking its name, so the real registration under that name is later
	// refused as a duplicate, and its defaults are still rendered on every resolution.
	if err == nil && len(keys) == 0 {
		err = fmt.Errorf("every path it declares is excluded (%v), so the section declares nothing", excluded)
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
		if drop[key] {
			return nil, nil, fmt.Errorf("%s is excluded twice, and the second one covers nothing", key)
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

// detached returns a copy of the section that shares no storage with the registry's own.
//
// Every path out of the mutex goes through this, so a slice field added to Section later is copied
// without anyone having to remember to copy it at each accessor. A caller that sorts or writes into
// what it was handed reaches its own storage, and both walks keep reading what registration decided.
func (s Section) detached() Section {
	s.Keys = append([]string(nil), s.Keys...)
	s.Excluded = append([]string(nil), s.Excluded...)
	return s
}

// Sections returns every registered section, sorted by name.
func Sections() []Section {
	registered, _, _ := snapshot()
	return registered
}

// Lookup returns a registered section.
func Lookup(name string) (Section, bool) {
	mu.RLock()
	defer mu.RUnlock()
	s, ok := sections[name]
	return s.detached(), ok
}

// snapshot returns every registered section, every defect, and how each section's values reach their
// reader, read together.
//
// One acquisition for all three, because they are parts of one answer: the sections are the key space, the
// defects say which keys are missing from it, and the delivery declarations say how the rest of them get
// where they are read. Read separately, a registration arriving between two reads is described by one part
// and absent from another, so the answer describes two registries and no part of it holds against the
// others.
func snapshot() ([]Section, []Defect, map[string]string) {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Section, 0, len(sections))
	for _, s := range sections {
		out = append(out, s.detached())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	decoded := make(map[string]string, len(decodedNotLookedUp))
	for name, why := range decodedNotLookedUp {
		decoded[name] = why
	}
	return out, allDefects(), decoded
}

// allDefects returns the recorded defects and the ones derived from the registry's own state.
//
// A declaration naming a section nothing registered is derived at every read rather than recorded when the
// declaration arrives. A section registers itself and declares how it is delivered from two calls in its
// own package's initialisation, and nothing fixes the order between them, so refusing at the moment of
// declaring would refuse a correct pair that happened to declare first.
//
// The caller holds mu.
func allDefects() []Defect {
	out := append([]Defect(nil), defects...)
	names := make([]string, 0, len(decodedNotLookedUp))
	for name := range decodedNotLookedUp {
		if _, registered := sections[name]; !registered {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, Defect{Section: name, Err: fmt.Errorf(
			"declared as decoded rather than looked up (%s) and no section of this name is registered. "+
				"Nothing delivers the section this names, and if the name is a misspelling of a section "+
				"that is registered, that section's keys install into a source its reader never asks",
			decodedNotLookedUp[name])})
	}
	return out
}

// Defects returns every registration this package could not use.
func Defects() []Defect {
	mu.RLock()
	defer mu.RUnlock()
	return allDefects()
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
func deriveKeys(name, prefix string, prototype any) ([]string, error) {
	if name == "" {
		return nil, fmt.Errorf("section name is empty")
	}
	if name != strings.ToLower(name) {
		return nil, fmt.Errorf("section name %q is not lower case; configuration sources "+
			"enumerate lower-cased, so a key under it would never match a written one", name)
	}
	if bad, found := unaddressableChar(name); found {
		return nil, fmt.Errorf("section name %q carries %q, and a section is one segment. A dotted name "+
			"declares keys inside another section's subtree, where the two sections' defaults land in "+
			"one map and whichever renders last silently wins; a space cannot be written in an "+
			"environment variable name at all", name, bad)
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
	// The label is the section name, and the prefix is what builds a key. They differ for a section whose
	// keys sit at the root, where the prefix is empty: a message built from that reads ".HaltHeight has no
	// mapstructure tag", which names no section at all.
	if err := walk(t, label(name, prefix), prefix, &keys, map[reflect.Type]bool{}); err != nil {
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
func walk(t reflect.Type, label, prefix string, keys *[]string, open map[reflect.Type]bool) error {
	if open[t] {
		return fmt.Errorf("%s is %s, which contains itself; a key space derived from it has no end",
			label, t)
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
					"to it, so the tag names a key that reaches no field", label, f.Name)
			}
			continue
		}

		tag, squash, skip, err := tagOf(f, label)
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
				return fmt.Errorf("%s.%s is squashed but is a %s, not a struct", label, f.Name, ft.Kind())
			}
			if err := walkSubtree(ft, label, prefix, join(label, f.Name), keys, open); err != nil {
				return err
			}
			continue
		}

		path := join(prefix, tag)
		if ft.Kind() == reflect.Struct && !isLeaf(ft) {
			if err := walkSubtree(ft, join(label, tag), path, join(label, f.Name), keys, open); err != nil {
				return err
			}
			continue
		}
		*keys = append(*keys, path)
	}
	return nil
}

// label is the name a diagnostic carries for a section.
//
// The section's own name, because a prefix is empty for a section whose keys sit at the root of the file and
// a message built from that names nothing.
func label(name, prefix string) string {
	if prefix == "" {
		return name
	}
	return prefix
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
func walkSubtree(t reflect.Type, label, path, field string, keys *[]string, open map[reflect.Type]bool) error {
	before := len(*keys)
	if err := walk(t, label, path, keys, open); err != nil {
		return err
	}
	if len(*keys) == before {
		return fmt.Errorf("%s is a %s that declares no key, so configuration cannot reach it", field, t)
	}
	return nil
}

// tagOf returns a field's mapstructure name, or reports that the field cannot be addressed.
func tagOf(f reflect.StructField, label string) (name string, squash, skip bool, err error) {
	tag, ok := f.Tag.Lookup("mapstructure")
	if !ok {
		return "", false, false, fmt.Errorf("%s.%s has no mapstructure tag; a key derived from a field "+
			"name is a key no operator writes, which is how ninety-two legacy keys became "+
			"unreachable through their tags", label, f.Name)
	}

	name, squash, remain := parseTag(tag)
	if remain {
		return "", false, true, nil
	}
	if squash {
		if name != "" {
			return "", false, false, fmt.Errorf("%s.%s is squashed and also names %q; one or the other",
				label, f.Name, name)
		}
		return "", true, false, nil
	}
	if assignedOutsideConfiguration(name) {
		return "", false, true, nil
	}
	if name == "" {
		return "", false, false, fmt.Errorf("%s.%s has an empty mapstructure name", label, f.Name)
	}
	if bad, found := unaddressableChar(name); found {
		return "", false, false, fmt.Errorf("%s.%s names %q, which carries %q. A dot makes the field claim a "+
			"subtree the struct does not have, and neither a dot nor a space survives a round trip "+
			"through a configuration source", label, f.Name, name, bad)
	}
	if neverMatchesAWrittenKey(name) {
		return "", false, false, fmt.Errorf("%s.%s names %q, which is not lower case; a configuration "+
			"source enumerates lower-cased, so this key would never match a written one",
			label, f.Name, name)
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
//
// Every piece of registration state, not only the sections. A delivery declaration left behind names a
// section that no longer exists, which is the one thing a fresh registry is supposed to guarantee cannot
// happen.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	sections = map[string]Section{}
	defects = nil
	decodedNotLookedUp = map[string]string{}
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
