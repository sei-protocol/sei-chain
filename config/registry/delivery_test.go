package registry_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// probeSection is the struct the tests here register.
type probeSection struct {
	A string `mapstructure:"a"`
	B string `mapstructure:"b"`
}

// registerProbe registers one section named for the caller and returns its defaults.
func registerProbe(t *testing.T, name string) {
	t.Helper()
	registry.RegisterSection(name, &probeSection{}, func(registry.Mode) any {
		return probeSection{A: "from the default", B: "from the default"}
	})
}

// TestADeclarationNamingNoSectionIsReported covers the misspelling these two calls invite.
//
// A section states its own keys and states how they are delivered from two calls side by side, and only
// the first is checked against a struct. A misspelling in the second is accepted by the compiler, names a
// section that does not exist, and leaves the section that does exist delivered the wrong way: its keys
// resolve, install into the source, and its reader never asks that source for them.
//
// Nothing else can catch it. The delivery is a claim about a reader in another package, so there is no
// second statement of it to compare against, which is why the registry answers for the name itself.
func TestADeclarationNamingNoSectionIsReported(t *testing.T) {
	registry.Reset()
	registerProbe(t, "mempool")
	registry.DeclareDecodedNotLookedUp("memool", "decoded into tmcfg.Config")

	var named []string
	for _, d := range registry.Defects() {
		named = append(named, d.Section)
		if !strings.Contains(d.Err.Error(), "no section of this name is registered") {
			t.Errorf("the defect for %q says %q, and it has to say the name matches no section",
				d.Section, d.Err)
		}
	}
	if !reflect.DeepEqual(named, []string{"memool"}) {
		t.Fatalf("the reported defects name %v, want the misspelled declaration alone. Unreported, the "+
			"section that is registered is delivered the wrong way and nothing says so", named)
	}
}

// TestADeclarationMatchingItsSectionIsNotReported is what keeps the check above from refusing every
// correct pair.
//
// The two calls are made in one package's initialisation and nothing fixes the order between them, so a
// declaration is routinely recorded before the registration it belongs to. A check at the moment of
// declaring would refuse the correct pair that happened to declare first, which is why the name is
// answered for at every read instead.
func TestADeclarationMatchingItsSectionIsNotReported(t *testing.T) {
	for _, order := range []string{"registration first", "declaration first"} {
		t.Run(order, func(t *testing.T) {
			registry.Reset()
			if order == "registration first" {
				registerProbe(t, "mempool")
				registry.DeclareDecodedNotLookedUp("mempool", "decoded into tmcfg.Config")
			} else {
				registry.DeclareDecodedNotLookedUp("mempool", "decoded into tmcfg.Config")
				registerProbe(t, "mempool")
			}
			requireNoDefects(t)
			if _, declared := registry.DecodedSections()["mempool"]; !declared {
				t.Error("the section is not reported as decoded, so its values would be delivered by a " +
					"lookup its reader never makes")
			}
		})
	}
}

// TestADeclarationWithNoReasonIsReported holds the reason to being required.
//
// The reason names the struct the values are decoded into, which is the only thing a reader can check the
// claim against. Accepted empty, the claim that a section is delivered differently rests on nothing.
func TestADeclarationWithNoReasonIsReported(t *testing.T) {
	registry.Reset()
	registerProbe(t, "mempool")
	registry.DeclareDecodedNotLookedUp("mempool", "")

	if len(registry.Defects()) != 1 {
		t.Fatalf("declaring with no reason produced %d defects, want one", len(registry.Defects()))
	}
	if _, declared := registry.DecodedSections()["mempool"]; declared {
		t.Error("the section was recorded as decoded despite being refused, so the refusal changed " +
			"nothing about how it is delivered")
	}
}

// TestResetClearsHowSectionsAreDelivered covers the isolation the registry offers a test.
//
// Reset exists so one test's registrations cannot reach another's declared set. A delivery declaration
// left behind survives into a registry that no longer holds the section it names, which is both a defect
// the next test did not cause and, if that test registers the same name, a delivery it never declared.
func TestResetClearsHowSectionsAreDelivered(t *testing.T) {
	registry.Reset()
	registerProbe(t, "mempool")
	registry.DeclareDecodedNotLookedUp("mempool", "decoded into tmcfg.Config")
	requireNoDefects(t)

	registry.Reset()
	if got := registry.DecodedSections(); len(got) != 0 {
		t.Errorf("a fresh registry reports %v as delivered by a decode, and it holds no sections at "+
			"all", got)
	}
	if got := registry.Defects(); len(got) != 0 {
		t.Errorf("a fresh registry reports %d defects, which the next test to read them did not cause",
			len(got))
	}
}

// TestOnlyASuppliedValueReachesADecodedSection is the difference between delivering a value and
// replacing an operator's file.
//
// A section read by a decode already holds what its own file said. Handing it a default would rewrite
// that on every boot for every key the operator's file does not mention, so a key that took its default
// is skipped and a key any other layer answered is delivered.
func TestOnlyASuppliedValueReachesADecodedSection(t *testing.T) {
	registry.Reset()
	registerProbe(t, "mempool")
	registerProbe(t, "api")
	registry.DeclareDecodedNotLookedUp("mempool", "decoded into tmcfg.Config")
	requireNoDefects(t)

	resolved, err := registry.Resolve(registry.ModeFull, registry.Sources{
		File: map[string]any{"mempool.a": "from the file", "api.a": "from the file"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	got := registry.SuppliedByDecodedSection(resolved)
	want := map[string]map[string]any{"mempool": {"mempool.a": "from the file"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the decoded sections are handed %v, want %v. A key nobody wrote arriving here replaces "+
			"an operator's own value, and a section delivered by a lookup arriving here is delivered "+
			"twice", got, want)
	}
	if _, held := got["mempool"]["mempool.b"]; held {
		t.Error("mempool.b took its default and was handed to the decode anyway, which writes a value " +
			"nobody chose over whatever the node's own file holds")
	}
}
