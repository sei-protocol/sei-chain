package registry_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// listBearing is a probe whose default is a package-level variable, which is the usual shape.
type listBearing struct {
	Allowed []string          `mapstructure:"allowed"`
	Labels  map[string]string `mapstructure:"labels"`
	Absent  []string          `mapstructure:"absent"`
}

var listBearingDefault = listBearing{
	Allowed: []string{"callTracer", "prestateTracer"},
	Labels:  map[string]string{"chain": "pacific-1"},
}

// TestAResolvedListIsTheCallersToWriteInto covers what a caller may do with a resolved value.
//
// A section's default is usually a package-level variable, so handing out its slice hands out the array
// that variable holds. A caller sorting or de-duplicating a resolved list in place, which is what a caller
// producing deterministic output does, would rewrite that variable for the whole process: every later
// resolution and every reader that copies the same struct. Two of the lists this reaches in practice are
// deny lists, so the rewrite is silent and it is a security control.
func TestAResolvedListIsTheCallersToWriteInto(t *testing.T) {
	registry.Reset()
	registry.RegisterSection("probe", &listBearing{}, func(registry.Mode) any { return listBearingDefault })
	for _, d := range registry.Defects() {
		t.Fatalf("the probe was refused: %v", d.Err)
	}

	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	resolved.Values["probe.allowed"].([]string)[0] = "written-by-the-caller"
	resolved.Values["probe.labels"].(map[string]string)["chain"] = "written-by-the-caller"

	if got := listBearingDefault.Allowed[0]; got != "callTracer" {
		t.Errorf("writing into the resolved list changed the section's own default to %q, so every later "+
			"resolution and every reader copying that struct carries the caller's value", got)
	}
	if got := listBearingDefault.Labels["chain"]; got != "pacific-1" {
		t.Errorf("writing into the resolved map changed the section's own default to %q", got)
	}

	again, err := registry.Resolve(registry.ModeFull, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := again.Values["probe.allowed"]; !reflect.DeepEqual(got, []string{"callTracer", "prestateTracer"}) {
		t.Errorf("a later resolution carries %v, so one caller's edit reached another's answer", got)
	}

	// A nil list stays nil rather than becoming an empty one, because absent and empty are different
	// answers to a reader that checks length.
	if got := again.Values["probe.absent"]; got == nil || !reflect.ValueOf(got).IsNil() {
		t.Errorf("an unset list resolved to %#v, want a nil slice of its own type", got)
	}
}

// TestSortingAResolvedNestedListLeavesTheSectionsOwnDefaultAlone is the same property one level down.
//
// A copy of the outer list or map still points at the inner storage, so a caller sorting an inner list
// reaches the section's own default exactly as directly as sorting the outer one would. The damage is the
// same and it is worse to find: the process-wide default is rewritten, and the resolution that carries it
// is the next one, in whatever code happens to run after.
//
// Driven through a second resolution rather than by inspecting the variable, because what matters is not
// that a copy was taken but that the next caller is unaffected.
func TestSortingAResolvedNestedListLeavesTheSectionsOwnDefaultAlone(t *testing.T) {
	type nested struct {
		Deny map[string][]string `mapstructure:"deny"`
	}
	// A package-level default is the shape every section uses, so the value handed out is the one the
	// process keeps.
	shared := nested{Deny: map[string][]string{"rpc": {"zebra", "aardvark"}}}
	registry.RegisterSection("detach_nested", &nested{}, func(registry.Mode) any { return shared })

	first, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	handed, ok := first.Values["detach_nested.deny"].(map[string][]string)
	if !ok {
		t.Fatalf("detach_nested.deny resolved to %T, want a map of lists", first.Values["detach_nested.deny"])
	}
	// What a caller does with a list it was given.
	sort.Strings(handed["rpc"])

	second, err := registry.Resolve(registry.ModeValidator, registry.Sources{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	again, ok := second.Values["detach_nested.deny"].(map[string][]string)
	if !ok {
		t.Fatalf("the second resolution carried %T", second.Values["detach_nested.deny"])
	}
	if want := []string{"zebra", "aardvark"}; !reflect.DeepEqual(again["rpc"], want) {
		t.Errorf("a later resolution carries %v, want %v. Sorting a list inside a resolved value rewrote "+
			"the section's own default, so every resolution after it carries the sorted one",
			again["rpc"], want)
	}
}
