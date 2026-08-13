package configtest

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// SchemaCheck describes a section whose keys are declared by a struct written for that purpose.
//
// Some sections cannot declare their keys from the type their reader fills. That type may carry no
// mapstructure tags at all, or tags that name something other than the keys the reader resolves, and
// it may live in a tree this repository does not change. The section then declares a struct written
// for the purpose: it holds the spelling and nothing decodes into it.
//
// That split needs holding together, and the part that needs it most is the correspondence between a
// key and the setting it reaches. A test that states that correspondence twice proves nothing, since
// both statements come from the same reading. This one asks the reader instead.
type SchemaCheck struct {
	// Read runs the section's live reader over a set of written values.
	Read func(AppOpts) (any, error)
	// Probe is one value per declared key, each different from what the reader leaves that key's
	// setting at when nothing is written. A value that is not different leaves the reader's output
	// unchanged, which the check reports: it cannot tell that from a key the reader never looks up.
	//
	// Different from the empty read rather than from the baseline, because the two are not always the
	// same. A reader that assigns straight from a lookup resolves an absent key to zero, so a key whose
	// baseline is true needs a probe of true to be observable at all.
	Probe map[string]any
	// Context is extra values written alongside a key's probe, and in the read its baseline is compared
	// against. For a key whose effect another key suppresses, this is what makes it observable: the
	// state-commit write mode is ignored while automatic mode is on, so probing it without turning that
	// off changes nothing and reads as a key the reader never looks up.
	Context map[string]AppOpts
	// AlsoDerives names the settings a key changes besides its own. A reader that computes one setting
	// from two keys moves more than one field when either is written, and naming which are derived is
	// what keeps the one-setting rule meaningful instead of relaxed.
	AlsoDerives map[string][]string
}

// CheckSchemaMatchesTheReader holds a purpose-written schema against the reader it describes.
//
// Two properties, per declared key. Writing a value under the key changes exactly one setting, which
// is what says the key reaches something and says which thing. And the baseline the registry resolves
// for that key equals what the reader leaves that same setting at when nothing is written, which is
// what says declaring the section does not change what a node runs.
//
// Neither property is stated by hand. The setting a key reaches is found by writing to the key and
// observing the reader, so a schema field paired with the wrong setting fails here rather than
// resolving one operator's value into another's setting.
func CheckSchemaMatchesTheReader(t testing.TB, section string, c SchemaCheck) {
	t.Helper()

	registered, ok := registry.Lookup(section)
	if !ok {
		t.Fatalf("%s is not registered, so there is no schema to check", section)
	}
	if len(registered.Keys) == 0 {
		t.Fatalf("%s declares no keys, so every check below holds by covering nothing", section)
	}
	if _, err := c.Read(AppOpts{}); err != nil {
		t.Fatalf("%s: the reader refused an empty configuration, which is what a node that has written "+
			"nothing has: %v", section, err)
	}

	divergences := make([]divergence, 0, len(registered.Keys)*len(registry.Modes()))
	for _, mode := range registry.Modes() {
		divergences = append(divergences, c.checkMode(t, section, registered, mode)...)
	}
	CheckAbsentReadDivergences(t, section, divergences)
}

// divergence is one key whose baseline is not what the reader produces for an empty configuration.
type divergence struct {
	Key      string
	Mode     registry.Mode
	Setting  string
	Absent   string
	Baseline string
}

// checkMode runs the per-key checks for one node mode and returns the divergences it found.
func (c SchemaCheck) checkMode(t testing.TB, section string, registered registry.Section,
	mode registry.Mode) []divergence {
	t.Helper()

	resolved, err := registry.Resolve(mode)
	if err != nil {
		t.Fatalf("%s: cannot resolve the baseline for %q: %v", section, mode, err)
	}

	var found []divergence
	for _, key := range registered.Keys {
		probe, ok := c.Probe[key]
		if !ok {
			t.Errorf("%s: no probe value for %q, so nothing checks which setting it reaches or what it "+
				"resolves to", section, key)
			continue
		}
		baseline, ok := resolved.Keys[key]
		if !ok {
			t.Errorf("%s: the resolver produced no baseline for %q", section, key)
			continue
		}
		// The probe has to be the shape the schema declares, because that is the shape the resolved
		// configuration delivers. A probe of some other shape tests a value no operator could cause to
		// arrive, and a reader that accepts only one shape would look as though it accepted the
		// declared one. The simulation gas limit is the case: its reader takes a non-empty string and
		// ignores a number, so declaring it as a number gives an operator a setting that never applies.
		if want, got := reflect.TypeOf(baseline.Value), reflect.TypeOf(probe); want != got {
			t.Errorf("%s: the schema declares %q as %v and the probe is %v. The resolved configuration "+
				"delivers the declared shape, so a probe of another shape checks a value that cannot "+
				"arrive; make the probe match, or the schema match what the reader takes",
				section, key, want, got)
			continue
		}

		// Read both sides with this key's companions, so the baseline is compared against the same
		// state the probe is measured from.
		base, err := c.Read(c.opts(key, nil))
		if err != nil {
			t.Errorf("%s: the reader refused the companions for %q: %v", section, key, err)
			continue
		}
		written, err := c.Read(c.opts(key, probe))
		if err != nil {
			t.Errorf("%s: the reader refused %v under %q: %v", section, probe, key, err)
			continue
		}
		changed := c.settingsOwnedBy(key, settingsThatDiffer(base, written))
		switch len(changed) {
		case 1:
			// The one setting this key reaches. Its value when nothing is written is what the section's
			// baseline agrees with, or a difference that gets recorded rather than asserted away.
			if got := fieldValue(base, changed[0]); !sameValue(baseline.Value, got) {
				found = append(found, divergence{
					Key:      key,
					Mode:     mode,
					Setting:  changed[0],
					Absent:   fmt.Sprintf("%#v", got),
					Baseline: fmt.Sprintf("%#v", baseline.Value),
				})
			}
		case 0:
			t.Errorf("%s: writing %v under %q changed nothing the reader returns, so either the reader "+
				"does not resolve that key or the probe is not distinctive",
				section, probe, key)
		default:
			t.Errorf("%s: writing %v under %q changed %v. A key reaching several settings cannot have "+
				"one baseline checked against one of them, so this section needs saying out loud",
				section, probe, key, changed)
		}
	}
	return found
}

// CheckAbsentReadDivergences records the keys whose baseline is not what the reader produces when
// nothing is written, and holds that set against testdata/<section>.absent.golden.
//
// A record rather than an assertion, because some of these differences are deliberate. A reader that
// assigns a value straight from a lookup, with no check that the key was present, resolves an absent
// key to the zero value and clobbers its own declared default. What such a node runs when a key is
// missing is therefore not what the code says the default is, and it is not what seid init writes into
// app.toml either. Declaring the section has to pick one, and picking the declared default is a change
// for those nodes.
//
// Recording both sides is what makes that reviewable, and it is what a written reason cannot do. A
// reason keeps looking valid after it stops being true: guard the read and it still passes, silently,
// for good. Here the absent column moves, the row changes, and the fix shows up in a diff.
func CheckAbsentReadDivergences(t testing.TB, section string, found []divergence) {
	t.Helper()

	var b strings.Builder
	b.WriteString("# Keys whose baseline is not what the reader produces when nothing is written.\n")
	b.WriteString("# Regenerate with -update.\n")
	b.WriteString("#\n")
	b.WriteString("# A row means a node with this key missing runs the absent value today and would run\n")
	b.WriteString("# the baseline once the section is declared. Guarding the read is what empties this\n")
	b.WriteString("# file: the absent column then becomes the declared default and the row disappears.\n")
	b.WriteString("#\n")
	b.WriteString("# key\tsetting\tabsent\tbaseline\tmodes\n\n")

	if len(found) == 0 {
		b.WriteString("(none: every key resolves to what the reader produces for an empty configuration)\n")
	}
	for _, row := range groupByKey(found) {
		b.WriteString(row + "\n")
	}

	got := strings.TrimRight(b.String(), "\n")
	path := goldenFilePath(t, section, ".absent.golden")

	if goldenUpdateRequested() {
		writeGolden(t, section, path, got)
		return
	}
	want, err := os.ReadFile(path) // #nosec G304 -- goldenFilePath confines this to testdata
	if err != nil {
		t.Fatalf("%s: cannot read %s: %v\n\nThis record says which keys change value for a node that has "+
			"one of them missing. Create it with `go test ./<pkg>/ -update` and read the diff",
			section, path, err)
	}
	if recorded := strings.TrimRight(string(want), "\n"); recorded != got {
		t.Errorf("%s: the keys that change value for a node missing them no longer match %s.\n\ngot:\n%s"+
			"\n\nrecorded:\n%s\n\nA row added here is a node's behaviour changing. A row removed is a "+
			"reader being guarded, which is the fix. Regenerate with `go test ./<pkg>/ -update` and keep "+
			"the diff in the change that caused it", section, path, got, recorded)
	}
}

// groupByKey renders one line per distinct key and value pair, listing the modes it holds for.
//
// A baseline that does not vary by mode would otherwise repeat every row once per mode, and a genuine
// per-mode difference would be lost in that repetition.
func groupByKey(found []divergence) []string {
	type shape struct{ key, setting, absent, baseline string }
	modes := map[shape][]string{}
	var order []shape
	for _, d := range found {
		s := shape{d.Key, d.Setting, d.Absent, d.Baseline}
		if _, seen := modes[s]; !seen {
			order = append(order, s)
		}
		modes[s] = append(modes[s], string(d.Mode))
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].key != order[j].key {
			return order[i].key < order[j].key
		}
		return order[i].setting < order[j].setting
	})

	out := make([]string, 0, len(order))
	for _, s := range order {
		where := strings.Join(modes[s], ",")
		if len(modes[s]) == len(registry.Modes()) {
			where = "all"
		}
		out = append(out, fmt.Sprintf("%s\t%s\t%s\t%s\t%s", s.key, s.setting, s.absent, s.baseline, where))
	}
	return out
}

// opts returns the values to read with for one key: its companions, plus the probe when there is one.
func (c SchemaCheck) opts(key string, probe any) AppOpts {
	out := AppOpts{}
	for k, v := range c.Context[key] {
		out[k] = v
	}
	if probe != nil {
		out[key] = probe
	}
	return out
}

// settingsOwnedBy drops the settings a key only derives, leaving the one it stands for.
func (c SchemaCheck) settingsOwnedBy(key string, changed []string) []string {
	derived := map[string]bool{}
	for _, name := range c.AlsoDerives[key] {
		derived[name] = true
	}
	out := make([]string, 0, len(changed))
	for _, name := range changed {
		if !derived[name] {
			out = append(out, name)
		}
	}
	return out
}

// settingsThatDiffer returns the exported field paths whose values differ between two reader outputs.
func settingsThatDiffer(before, after any) []string {
	var paths []string
	walkFields(reflect.ValueOf(before), reflect.ValueOf(after), "", &paths)
	sort.Strings(paths)
	return paths
}

// walkFields appends the paths of differing exported fields, descending into nested structs.
func walkFields(before, after reflect.Value, prefix string, paths *[]string) {
	for before.Kind() == reflect.Ptr {
		if before.IsNil() != after.IsNil() {
			*paths = append(*paths, prefix)
			return
		}
		if before.IsNil() {
			return
		}
		before, after = before.Elem(), after.Elem()
	}
	if before.Kind() != reflect.Struct {
		if !reflect.DeepEqual(before.Interface(), after.Interface()) {
			*paths = append(*paths, prefix)
		}
		return
	}
	for i := 0; i < before.NumField(); i++ {
		if !before.Type().Field(i).IsExported() {
			continue
		}
		name := before.Type().Field(i).Name
		if prefix != "" {
			name = prefix + "." + name
		}
		walkFields(before.Field(i), after.Field(i), name, paths)
	}
}

// fieldValue returns the value at a dotted field path in a reader's output.
func fieldValue(from any, path string) any {
	v := reflect.ValueOf(from)
	for _, name := range splitPath(path) {
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return nil
			}
			v = v.Elem()
		}
		v = v.FieldByName(name)
		if !v.IsValid() {
			return nil
		}
	}
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	return v.Interface()
}

// splitPath breaks a dotted field path into its segments.
func splitPath(path string) []string {
	var out []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			out = append(out, path[start:i])
			start = i + 1
		}
	}
	return append(out, path[start:])
}

// sameValue reports whether two configuration values are the same setting.
//
// Written rather than handed to reflect.DeepEqual because the two sides come from different places. A
// schema declares the type a key carries and a reader may hold it as a pointer or a wider number, and
// a comparison that called those different would report drift that no operator could observe.
func sameValue(a, b any) bool {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	for av.Kind() == reflect.Ptr {
		if av.IsNil() {
			return !bv.IsValid() || (bv.Kind() == reflect.Ptr && bv.IsNil())
		}
		av = av.Elem()
	}
	for bv.Kind() == reflect.Ptr {
		if bv.IsNil() {
			return false
		}
		bv = bv.Elem()
	}
	// A reader may hold an optional setting as a pointer and leave it nil when nothing is written,
	// while a schema declares the type the key carries and says "nothing written" with that type's
	// zero. Those are the same state, so they compare equal here. A non-zero baseline against an
	// unset setting still does not, which is the drift worth catching.
	if !av.IsValid() {
		return !bv.IsValid() || bv.IsZero()
	}
	if !bv.IsValid() {
		return av.IsZero()
	}
	if number(av.Kind()) && number(bv.Kind()) {
		return fmt.Sprintf("%v", av.Interface()) == fmt.Sprintf("%v", bv.Interface())
	}
	// A schema declares the shape the configuration carries, which for an enumerated setting is a
	// plain string, while the reader may hold it as a named string type. A comparison that called
	// those different would report drift no operator could observe. The declared shape has to stay
	// plain: a named type does not survive the cast the reader performs on the written value.
	if av.Kind() == reflect.String && bv.Kind() == reflect.String {
		return av.String() == bv.String()
	}
	return reflect.DeepEqual(av.Interface(), bv.Interface())
}

// number reports whether a kind holds an integer or floating point value.
func number(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}
