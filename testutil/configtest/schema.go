package configtest

import (
	"fmt"
	"reflect"
	"sort"
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
	// Mode is the node mode whose baseline is checked.
	Mode registry.Mode
	// Read runs the section's live reader over a set of written values.
	Read func(AppOpts) (any, error)
	// Probe is one value per declared key, each different from that key's baseline. A value equal to
	// the baseline would leave the reader's output unchanged, and the check would hold for a key the
	// reader never looks up.
	Probe map[string]any
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
	resolved, err := registry.Resolve(c.Mode)
	if err != nil {
		t.Fatalf("%s: cannot resolve the baseline for %q: %v", section, c.Mode, err)
	}
	base, err := c.Read(AppOpts{})
	if err != nil {
		t.Fatalf("%s: the reader refused an empty configuration, which is what a node that has written "+
			"nothing has: %v", section, err)
	}

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
		if sameValue(probe, baseline.Value) {
			t.Errorf("%s: the probe for %q is %v, which is also its baseline. The reader's output would "+
				"be identical either way, so this would hold for a key nothing reads",
				section, key, probe)
			continue
		}

		written, err := c.Read(AppOpts{key: probe})
		if err != nil {
			t.Errorf("%s: the reader refused %v under %q: %v", section, probe, key, err)
			continue
		}
		changed := settingsThatDiffer(base, written)
		switch len(changed) {
		case 1:
			// The one setting this key reaches. Its value when nothing is written is what the section's
			// baseline has to agree with.
			if got := fieldValue(base, changed[0]); !sameValue(baseline.Value, got) {
				t.Errorf("%s: the baseline for %q resolves to %#v and the reader leaves %s at %#v when "+
					"nothing is written. Declaring this section changes what a node runs",
					section, key, baseline.Value, changed[0], got)
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
