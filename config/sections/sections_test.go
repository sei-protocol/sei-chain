package sections_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/sections"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
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

// TestTheDeclaredSurfaceMatchesTheRecord is the gate that makes a schema change impossible to miss.
//
// The record holds every section, every key and every per-mode baseline. So a key added, removed, renamed
// or retyped fails here, and so does a changed default, which is a changed contract for every node that
// never wrote the key.
//
// Recorded as text rather than as the hash of it. A hash fails with no way to see what moved, and the
// reviewer then has to reconstruct the change from the rest of the diff. This way the thing that changed
// is the diff, which is also what makes the record a freeze: nothing leaves the key space quietly either.
//
// It lives here because this is the package that imports every owner, so the record covers the whole
// declared set rather than whichever sections a given test binary happened to register.
func TestTheDeclaredSurfaceMatchesTheRecord(t *testing.T) {
	if len(sections.Names) == 0 {
		t.Fatal("no sections are declared, so the record would freeze an empty key space")
	}
	configtest.CheckDeclaredSurface(t, "sections", registry.Surface())
}

// TestTheFingerprintIsTheHashOfTheRecordedSurface keeps the two from drifting apart.
//
// The fingerprint is what a deploy can compare cheaply, and the surface is what a human reads. They have to
// be the same statement, or a green fingerprint could sit alongside a surface nobody recorded.
func TestTheFingerprintIsTheHashOfTheRecordedSurface(t *testing.T) {
	want := sha256.Sum256([]byte(registry.Surface()))
	if got := registry.Fingerprint(); got != hex.EncodeToString(want[:]) {
		t.Errorf("the fingerprint is %s and the hash of the surface is %s. A deploy comparing the "+
			"fingerprint would then be checking something other than what the record holds",
			got, hex.EncodeToString(want[:]))
	}
}

// TestWiringMatchesTheRecord records which checks this package calls.
func TestWiringMatchesTheRecord(t *testing.T) {
	configtest.CheckWiring(t)
}
