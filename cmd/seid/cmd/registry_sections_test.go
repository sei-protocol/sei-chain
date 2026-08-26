package cmd

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"

	// Linked so the checks below see the whole key space rather than the part this binary already
	// reached. A section registers during its own package's initialisation, and these two register the
	// node's and the application's configuration files; nothing else imports either yet, so without
	// these the cross-section refusals are evaluated over a set neither file is in. Named rather than
	// blank, so the assertion on the two root sections is what keeps them here: a dropped import
	// stops compiling instead of quietly narrowing the union. Test imports rather than production
	// ones, because nothing consumes these sections yet.
	"github.com/sei-protocol/sei-chain/config/cosmosbase"
	"github.com/sei-protocol/sei-chain/config/tendermintbase"
)

// TestEverySectionThisBinaryDeclaresIsUsable is the check no single section can make.
//
// A section registers during its own package's initialisation, and a registration the registry cannot use
// is recorded rather than panicked, so a section that failed to register is absent rather than loud. Two
// of the refusals depend on what else has registered: two sections declaring one key, and two keys that
// collapse onto one environment variable. Neither is visible from inside either section, and the section
// that loses is dropped whole, with every key it declared.
//
// This package links every section a node's configuration reaches, so asking here is asking about the set
// a node actually gets. Nothing is enumerated, so a section added later is covered without this file
// changing, provided its package is linked: a registrar nothing imports registers in no binary, and the
// blank imports above are what keep that claim true.
func TestEverySectionThisBinaryDeclaresIsUsable(t *testing.T) {
	for _, defect := range registry.Defects() {
		t.Errorf("%s was refused, so none of its keys is declared: %v", defect.Section, defect.Err)
	}
	if len(registry.Sections()) == 0 {
		t.Fatal("no section registered, so the checks above hold for an empty set. This package links " +
			"the packages that register, and one of those imports has gone")
	}
}

// TestEveryDeclaredKeyResolvesForEveryMode covers the half of a registration a section's own test cannot.
//
// Registering validates the struct a section declares against. Whether its defaults can state one value
// for every key it declared is checked when something resolves them, and until now nothing did outside the
// registry's own tests. A default that arrives short is refused rather than filled, so the failure is an
// error here instead of a key resolving to a zero nobody chose.
func TestEveryDeclaredKeyResolvesForEveryMode(t *testing.T) {
	for _, mode := range registry.Modes() {
		resolved, err := registry.Resolve(mode, registry.Sources{})
		if err != nil {
			t.Errorf("mode %q does not resolve: %v", mode, err)
			continue
		}
		for _, section := range registry.Sections() {
			for _, key := range section.Keys {
				if _, ok := resolved.Values[key]; !ok {
					t.Errorf("mode %q: %s declares %s and it did not resolve", mode, section.Name, key)
				}
			}
		}
	}
}

// TestNoRootKeyCollidesWithAnotherSectionsName covers the collision the registry does not refuse.
//
// A key at the top of a file that is also a section's name cannot be written: no file holds both a value
// for that name and a table under it, so one of the two settings is unreachable and nothing says which.
//
// Asked here because it is a property of the whole key space and of no single section. Two packages
// declare keys at a root, one per configuration file, and a check inside either sees only its own.
//
// The two files are the reason a match is reported rather than failed outright: the key space spans them
// both, and a root key in one file against a table name in the other is two settings an operator can
// write, in two places, with no collision at all. So this names the pair and which file each came from,
// and the judgement is a human's.
func TestNoRootKeyCollidesWithAnotherSectionsName(t *testing.T) {
	tables := map[string]bool{}
	var roots []registry.Section
	for _, s := range registry.Sections() {
		if s.Prefix != "" {
			tables[s.Prefix] = true
			continue
		}
		roots = append(roots, s)
	}
	// Named rather than counted. Both configuration files put keys at their root, one section each, and
	// a guard on the count passes when an import goes and the union quietly narrows to one file.
	atRoot := map[string]bool{}
	for _, root := range roots {
		atRoot[root.Name] = true
	}
	for _, want := range []string{cosmosbase.BaseSectionName, tendermintbase.RootSectionName} {
		if !atRoot[want] {
			t.Fatalf("%s declares no keys at the root of its file, so this covers one file rather than "+
				"the key space and a collision across the two would not be seen", want)
		}
	}
	for _, root := range roots {
		for _, key := range root.Keys {
			if strings.Contains(key, ".") {
				continue
			}
			if tables[key] {
				t.Errorf("%s declares %q at the root of its file and a section takes that same name for "+
					"a table. Within one file only one of the two is writable; across the two files "+
					"both are, and which this is has to be decided rather than assumed", root.Name, key)
			}
		}
	}
}
