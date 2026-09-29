package registry

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Mode is a node's declared role, and the input a section's default varies on. It mirrors the
// app/params modes so this package stays a leaf; TestModesMatchTheNodeModes keeps the two in sync.
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

// IsFullnodeMode reports whether a node of this kind serves queries to other callers. Archive nodes do.
func IsFullnodeMode(mode Mode) bool { return mode == ModeFull || mode == ModeArchive }

// Section is one registered configuration section.
type Section struct {
	// Name identifies the section in lookups, reports and defects.
	Name string
	// Prefix is the first segment of every key this section declares, empty for root keys.
	Prefix string
	// Keys are the dotted paths this section declares, sorted.
	Keys []string
	// Excluded are dotted paths the struct carries that this section deliberately does not declare,
	// sorted. The value walk skips them too, so declared keys and stated values agree.
	Excluded []string
	// Defaults returns the section's default for a mode.
	Defaults func(Mode) any
}

// Defect is a registration this package cannot use.
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

// RegisterSection records a section's struct and its per-mode default. prototype is read only for
// its fields and mapstructure tags; each key is the section name joined with a field's tag. An
// unusable registration is recorded as a Defect and the section is not registered.
func RegisterSection(name string, prototype any, defaults func(Mode) any) {
	record(name, name, prototype, defaults, nil)
}

// RegisterSectionExcluding records a section, leaving out paths the struct carries that this key space
// does not offer. Each path is relative to the section; one matching no declared key is a Defect.
//
// Unlike a "-" tag, which says a field is not configuration at all, an excluded field is still decoded
// by the reader that owns its file.
func RegisterSectionExcluding(name string, prototype any, defaults func(Mode) any, excluding ...string) {
	record(name, name, prototype, defaults, excluding)
}

// RegisterRootKeys records a section whose keys sit at the top of the file. name labels the section
// and is not part of any key; otherwise it behaves like RegisterSection.
func RegisterRootKeys(name string, prototype any, defaults func(Mode) any) {
	record(name, "", prototype, defaults, nil)
}

// RegisterRootKeysExcluding is RegisterRootKeys with the exclusions of RegisterSectionExcluding.
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
	// deriveKeys already refuses an empty struct; this catches one whose every key was excluded.
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

// withoutExcluded splits derived keys into declared and excluded, refusing an exclusion that matches
// no derived key.
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

// envNamesAreDistinct refuses adding keys whose environment variable is already taken, within the
// new keys or by a registered section. Callers hold mu.
func envNamesAreDistinct(adding []string) error {
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
			// Only reachable through root keys, since a prefix keeps sections apart.
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

// detached returns a copy of the section that shares no storage with the registry's own. Every
// accessor returns sections through it.
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

// snapshot returns the registered sections sorted by name, every defect, and the decoded-section
// declarations, all read under one lock so they describe the same registry.
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

// allDefects returns the recorded defects plus one for each decoded declaration naming an unregistered
// section. The latter is derived on read because registration and declaration order is not fixed.
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

// deriveKeys walks a section's struct and returns the sorted dotted keys it declares. An untagged
// field is an error rather than falling back to the field name as mapstructure does, so the tag is the
// only spelling of a key.
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
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%s is not a struct", t.Kind())
	}

	var keys []string
	if err := walk(t, label(name, prefix), prefix, &keys, map[reflect.Type]bool{}); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("declares no keys")
	}
	sort.Strings(keys)
	for i := 1; i < len(keys); i++ {
		if keys[i] == keys[i-1] {
			return nil, fmt.Errorf("two fields both declare %q, so one of them is unreachable and "+
				"which one is not observable", keys[i])
		}
	}
	return keys, nil
}

// walk appends the dotted keys a struct declares under prefix. open holds the struct types on the
// current path, so a self-referential type is refused instead of recursing forever.
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
			// Reflection cannot write an unexported field, so a tag on one names a key that reaches nothing.
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
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}

		// A squashed field promotes its fields to this level without adding a segment.
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

// label is the name a diagnostic carries for a section: its prefix, or its name for root keys.
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

// parseTag splits a mapstructure tag into its name and its squash and remain options. A remain field
// collects unmatched keys and declares none itself.
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

// assignedOutsideConfiguration reports whether a tag ("-") excludes its field from configuration.
func assignedOutsideConfiguration(name string) bool {
	return name == "-"
}

// neverMatchesAWrittenKey reports whether a key segment carries upper case, which no source matches
// because sources enumerate keys lower-cased.
func neverMatchesAWrittenKey(name string) bool {
	return name != strings.ToLower(name)
}

// unaddressableChar returns the first dot or space in a key segment, and whether there was one. It
// applies to section names and field tags alike.
func unaddressableChar(segment string) (string, bool) {
	if i := strings.IndexAny(segment, ". "); i >= 0 {
		return segment[i : i+1], true
	}
	return "", false
}

// isLeaf reports whether a struct type is decoded whole, like time.Time, rather than walked for keys.
func isLeaf(t reflect.Type) bool {
	switch t.String() {
	case "time.Time", "big.Int":
		return true
	}
	return false
}

// Reset clears all registration state. For tests only.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	sections = map[string]Section{}
	defects = nil
	decodedNotLookedUp = map[string]string{}
}

// envPrefix is the environment namespace for every derived key. Unlike the legacy path, it does not
// follow the binary's name.
const envPrefix = "SEID"

// EnvName returns the environment variable that delivers a key: SEID_ plus the key upper-cased, with
// dots and hyphens as underscores, matching the boot's replacer.
func EnvName(key string) string { return EnvNameUnder(envPrefix, key) }

// EnvNameUnder is EnvName for a source whose environment prefix is prefix, upper-cased as viper does.
// An empty prefix names the bare key.
func EnvNameUnder(prefix, key string) string {
	name := strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(key))
	if prefix == "" {
		return name
	}
	return strings.ToUpper(prefix) + "_" + name
}
