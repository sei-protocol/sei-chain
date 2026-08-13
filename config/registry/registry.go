package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Mode is a node's declared role, and the input a section's baseline varies on.
//
// Declared here rather than imported from app/params so this package stays a leaf that any
// feature package can import. TestModesMatchTheNodeModes holds the two sets against each
// other, so a mode added there fails here rather than silently having no baseline.
type Mode string

// The modes a baseline may vary on.
const (
	ModeValidator Mode = "validator"
	ModeFull      Mode = "full"
	ModeSeed      Mode = "seed"
	ModeArchive   Mode = "archive"
)

// Modes returns every mode a baseline is asked for, in a fixed order.
func Modes() []Mode { return []Mode{ModeValidator, ModeFull, ModeSeed, ModeArchive} }

// Precedence is the declared order in which layers win, lowest to highest.
//
// The legacy path has no equivalent. Its order emerges from which viper instance a caller
// asked, which is why two orders are observable across the key set. Stating it as data is what
// lets a diagnostic tell a node operator that their environment variable beat their file.
var Precedence = []string{"default", "file", "env", "flag"}

// Section is one registered configuration section.
type Section struct {
	// Name identifies the section. It is what a lookup, a report and a recorded file are keyed by, and
	// for most sections it is also the first segment of every key.
	Name string
	// Prefix is the first segment of every key this section declares, and is empty for a section whose
	// keys sit at the root of the file with no section of their own.
	//
	// Separate from Name because the two are different jobs. A node-wide setting such as the pruning
	// strategy is written at the top of app.toml and read as "pruning", so it has no segment to take a
	// name from, and it still needs one to be looked up and reported under.
	Prefix string
	// Keys are the dotted paths this section declares, sorted.
	Keys []string
	// Defaults returns the section's baseline for a mode.
	Defaults func(Mode) any
	// Validate asks the section whether a set of resolved values is usable, and is nil when the
	// section's type states no rules of its own.
	Validate func(map[string]any) error
}

// Defect is a registration this package cannot use.
//
// Recorded rather than panicked. A panic here runs during package initialisation of a package
// every feature imports, so it would take down every seid invocation including --help, and it
// converts a compile-time-fixable mistake into a fleet-wide incident. CheckRegistrations is
// what turns these into a failing test.
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

// RegisterSection records a section's struct and its per-mode baseline.
//
// The dotted key for every field is derived from the section name and the field's mapstructure
// tag. Nothing here reads a hand-typed key string, which is the property that makes template
// and reader drift impossible rather than test-guarded.
//
// A section whose type has a Validate method also gains a check over its resolved values, which is
// where an enum's members and a number's range are stated. A section without one states no rules and
// is not asked.
//
// It never panics. A registration this package cannot use is recorded as a Defect and the
// section is not registered.
func RegisterSection(name string, proto any, defaults func(Mode) any) {
	record(name, name, proto, defaults)
}

// RegisterRootKeys records a section whose keys sit at the root of the file, with no section of their own.
//
// name identifies the section for lookups and reports and is not part of any key. Everything else matches
// RegisterSection: the keys come from the mapstructure tags, and the tags are the only spelling.
//
// This exists because some settings are node-wide and are written at the top of a file rather than inside
// a table. Giving them a section would rename them, and a renamed key is one an operator's existing file
// no longer reaches.
func RegisterRootKeys(name string, proto any, defaults func(Mode) any) {
	record(name, "", proto, defaults)
}

// record is the one path both registrations take.
func record(name, prefix string, proto any, defaults func(Mode) any) {
	keys, err := deriveKeys(name, prefix, proto)

	mu.Lock()
	defer mu.Unlock()
	switch {
	case err != nil:
		defects = append(defects, Defect{Section: name, Err: err})
	case defaults == nil:
		defects = append(defects, Defect{Section: name, Err: fmt.Errorf("no baseline function")})
	default:
		if _, dup := sections[name]; dup {
			defects = append(defects, Defect{Section: name, Err: fmt.Errorf("section registered twice")})
			return
		}
		if err := refuseOverlap(name, prefix, keys); err != nil {
			defects = append(defects, Defect{Section: name, Err: err})
			return
		}
		sections[name] = Section{
			Name:     name,
			Prefix:   prefix,
			Keys:     keys,
			Defaults: defaults,
			// Detected from the section's own type rather than declared separately, so a section
			// that grows a Validate method is checked from then on with nothing to remember.
			Validate: validatorFor(proto),
		}
	}
}

// refuseOverlap rejects a registration whose keys cannot coexist with what is already registered.
//
// Two shapes of overlap, and neither was possible before a section could declare a key at the root. A key
// two sections both declare has one baseline resolved over the other, and which one depends on map order.
// And a root key that is also a section's name cannot be written at all: a file holding both a value for
// it and a table under it is not valid TOML, so one of the two would be unreachable.
//
// Called with the lock held.
func refuseOverlap(name, prefix string, keys []string) error {
	declared := map[string]string{}
	names := map[string]string{}
	for _, s := range sections {
		for _, k := range s.Keys {
			declared[k] = s.Name
		}
		if s.Prefix != "" {
			names[s.Prefix] = s.Name
		}
	}

	for _, key := range keys {
		if owner, taken := declared[key]; taken {
			return fmt.Errorf("%s declares %q and so does %s; one baseline would resolve over the other "+
				"and which one wins depends on the order sections are walked", name, key, owner)
		}
		if prefix == "" {
			if owner, taken := names[key]; taken {
				return fmt.Errorf("%s declares %q at the root of the file and %s is a section of that "+
					"name; a file cannot hold both a value for %q and a table under it, so one of them "+
					"would be unreachable", name, key, owner, key)
			}
		}
	}
	if prefix != "" {
		for _, s := range sections {
			if s.Prefix != "" {
				continue
			}
			for _, k := range s.Keys {
				if k == prefix {
					return fmt.Errorf("%s is a section named %q and %s declares %q at the root of the "+
						"file; a file cannot hold both a table and a value under that name",
						name, prefix, s.Name, k)
				}
			}
		}
	}
	return nil
}

// Sections returns every registered section, sorted by name.
func Sections() []Section {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Section, 0, len(sections))
	for _, s := range sections {
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

// Surface renders every registration as text, one fact per line.
//
// This is what Fingerprint hashes, and having it as text is the difference between a check that says
// something changed and one that says what. A recorded hash fails CI with no way to see the change; a
// recorded surface fails with the key, the section or the default that moved sitting in the diff.
//
// Experimental keys are absent by construction, since they are not registered here.
func Surface() string {
	var b strings.Builder
	for _, s := range Sections() {
		fmt.Fprintf(&b, "section:%s\n", s.Name)
		if s.Prefix == "" {
			// Worth saying, because a section whose keys carry no prefix is the one shape where the name
			// and the keys are unrelated, and a reader of this file cannot infer it from the keys.
			fmt.Fprintf(&b, "root-keys:%s\n", s.Name)
		}
		for _, k := range s.Keys {
			fmt.Fprintf(&b, "key:%s\n", k)
		}
		// The baseline is part of the shape: a changed default is a changed contract for every
		// node that never wrote the key. Rendered per mode, since a baseline may vary by mode.
		for _, m := range Modes() {
			fmt.Fprintf(&b, "default:%s:%s:%#v\n", s.Name, m, s.Defaults(m))
		}
	}
	return b.String()
}

// Fingerprint hashes every registration, so a key added, renamed or retyped changes it.
//
// This is what makes forgetting a schema bump impossible: CI compares the fingerprint against a
// recorded one and fails until the bump and its migration land in the same change.
func Fingerprint() string {
	sum := sha256.Sum256([]byte(Surface()))
	return hex.EncodeToString(sum[:])
}

// deriveKeys walks a section's struct and returns the dotted keys it declares.
//
// An untagged field is an error rather than a fallback to the field's name, and that is the one
// decision in this file worth reading twice. The binder the node uses today does fall back, which
// is why an embedded srvconfig.Config with no tag put whole cosmos sections under a type-name
// prefix nothing writes, and why MemIAVLConfig's leaves sit outside state-commit.flatkv.*. Ninety-two
// operator-facing keys reach their field only through a spelling the tags do not produce, and a
// silent fallback is what made that invisible. Refusing to guess is what keeps the tag
// authoritative.
func deriveKeys(name, prefix string, proto any) ([]string, error) {
	if name == "" {
		return nil, fmt.Errorf("section name is empty")
	}
	if name != strings.ToLower(name) {
		return nil, fmt.Errorf("section name %q is not lower case; configuration sources "+
			"enumerate lower-cased, so a key under it would never match a written one", name)
	}
	if proto == nil {
		return nil, fmt.Errorf("no struct")
	}
	t := reflect.TypeOf(proto)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%s is not a struct", t.Kind())
	}

	var keys []string
	if err := walk(t, prefix, &keys); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("declares no keys")
	}
	sort.Strings(keys)
	return keys, nil
}

// walk appends the dotted keys a struct declares under prefix.
func walk(t reflect.Type, prefix string, keys *[]string) error {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
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
			if err := walk(ft, prefix, keys); err != nil {
				return err
			}
			continue
		}

		path := join(prefix, tag)
		if ft.Kind() == reflect.Struct && !isLeaf(ft) {
			if err := walk(ft, path, keys); err != nil {
				return err
			}
			continue
		}
		*keys = append(*keys, path)
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

// tagOf returns a field's mapstructure name, or reports that the field cannot be addressed.
func tagOf(f reflect.StructField, prefix string) (name string, squash, skip bool, err error) {
	tag, ok := f.Tag.Lookup("mapstructure")
	if !ok {
		return "", false, false, fmt.Errorf("%s.%s has no mapstructure tag; a key derived from a field "+
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
			return "", false, false, fmt.Errorf("%s.%s is squashed and also names %q; one or the other",
				prefix, f.Name, name)
		}
		return "", true, false, nil
	}
	// A dash is how mapstructure says a field is not populated from configuration at all. Such a field
	// has no key, so it contributes none, and treating it as a defect would refuse every struct that
	// carries a value derived somewhere else.
	if name == "-" {
		return "", false, true, nil
	}
	if name == "" {
		return "", false, false, fmt.Errorf("%s.%s has an empty mapstructure name", prefix, f.Name)
	}
	if name != strings.ToLower(name) {
		return "", false, false, fmt.Errorf("%s.%s names %q, which is not lower case; a configuration "+
			"source enumerates lower-cased, so this key would never match a written one",
			prefix, f.Name, name)
	}
	return name, false, false, nil
}

// isLeaf reports whether a struct type is a value rather than a group of keys.
//
// time.Time and its like carry exported fields but are decoded whole, so walking into them
// would invent keys no operator writes.
func isLeaf(t reflect.Type) bool {
	switch t.String() {
	case "time.Time", "time.Duration", "big.Int":
		return true
	}
	return false
}

// Reset clears the registry. For tests only, so one test's registrations cannot leak into
// another's fingerprint.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	sections = map[string]Section{}
	defects = nil
}

// EnvPrefix is the environment namespace for every derived key.
//
// A constant rather than path.Base(os.Executable()), which is what the legacy path uses. That
// derivation means renaming the binary moves the entire environment namespace, and it is why
// the legacy path answers to three environment universes at once.
const EnvPrefix = "SEID"

// EnvName returns the one environment variable that delivers a key.
//
// Derived from the key rather than declared, so a section cannot carry a spelling its
// environment variable does not match. Dots and hyphens both become underscores, which is the
// replacer the boot installs, so two keys differing only in that punctuation collide; a check
// over the registry is what catches that rather than a reader discovering it.
func EnvName(key string) string {
	return EnvPrefix + "_" + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(key))
}
