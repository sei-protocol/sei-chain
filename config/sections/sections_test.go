package sections_test

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/sections"
)

// TestEverySectionThisBinaryDeclaresIsRegistered is the check the blank imports above exist for.
//
// Removing one of them compiles, links and passes every other test. The section it registered simply
// stops being in the key space, its keys resolve through the machinery that answered them before,
// and every report about them gets shorter rather than wrong.
func TestEverySectionThisBinaryDeclaresIsRegistered(t *testing.T) {
	if missing := sections.Missing(); len(missing) > 0 {
		t.Errorf("these declared sections are not in the registry: %s\n\nTheir keys resolve through "+
			"the machinery that answered them before the registry existed, and no diagnostic mentions "+
			"them. The usual cause is a removed import in this package",
			strings.Join(missing, ", "))
	}
}

// TestNoSectionEnteredTheKeySpaceUnwritten is the other direction.
//
// A section nothing names is one nothing would notice leaving, which is how the set became a
// consequence of the import graph in the first place.
func TestNoSectionEnteredTheKeySpaceUnwritten(t *testing.T) {
	if unexpected := sections.Unexpected(); len(unexpected) > 0 {
		t.Errorf("these sections are registered and not named in this package: %s\n\nAdd them, so that "+
			"one of them failing to register later is a failure rather than a shorter report",
			strings.Join(unexpected, ", "))
	}
}

// TestTheDeclaredSetIsNotEmpty keeps both checks above from passing by covering nothing.
func TestTheDeclaredSetIsNotEmpty(t *testing.T) {
	if len(sections.Names) == 0 {
		t.Fatal("no sections are declared, so both checks above hold for a registry with nothing in it")
	}
	if len(registry.Keys()) == 0 {
		t.Fatal("the registry declares no keys at all")
	}
}
