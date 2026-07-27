package privval_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/privval"
)

// priv-validator.key-file and .state-file are config values that name a validator's
// consensus identity, and the loader treats a missing file as an instruction to create
// one. That is the most consequential silent behavior in the whole configuration
// surface: a mistyped --home does not fail, it mints a brand-new consensus key with a
// zeroed last-sign state, which is exactly the state in which a validator will happily
// double-sign.
//
// The asymmetry between the two files is the second half. Both missing is treated as a
// fresh node and regenerates; key present with state missing is an error. So the
// protection against re-signing depends on which of the two files went away.

// FuzzLoadOrGenFilePVRegeneratesOnAMissingHome pins the load-or-generate rule and, more
// importantly, that a fresh generation produces a *different* key than an existing one.
//
// The fuzzer varies the directory so the property is about absence rather than about one
// fixture path: any home without the key files yields a new identity, silently.
func FuzzLoadOrGenFilePVRegeneratesOnAMissingHome(f *testing.F) {
	f.Add("config")
	f.Add("cfg")
	f.Add("a/b")
	f.Add("x")

	f.Fuzz(func(t *testing.T, subdir string) {
		// The path is code-derived in production; keep to values that can name a
		// directory tree.
		if subdir == "" || len(subdir) > 32 || filepath.IsAbs(subdir) ||
			!filepath.IsLocal(subdir) {
			return
		}

		root := t.TempDir()
		dir := filepath.Join(root, subdir)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Skipf("mkdir: %v", err)
		}
		keyFile := filepath.Join(dir, "priv_validator_key.json")
		stateFile := filepath.Join(dir, "priv_validator_state.json")

		first, err := privval.LoadOrGenFilePV(keyFile, stateFile)
		if err != nil {
			t.Fatalf("an absent key pair must be generated, not refused: %v", err)
		}
		if _, statErr := os.Stat(keyFile); statErr != nil {
			t.Fatalf("generation must write the key file: %v", statErr)
		}

		// Loading again returns the same identity, so the generation happened once.
		again, err := privval.LoadOrGenFilePV(keyFile, stateFile)
		if err != nil {
			t.Fatalf("an existing key pair must load: %v", err)
		}
		firstAddr := first.GetAddress().String()
		if firstAddr != again.GetAddress().String() {
			t.Fatal("loading an existing key file must not regenerate the identity")
		}

		// A different directory — the shape a mistyped --home produces — yields a
		// different consensus identity with no diagnostic.
		otherDir := t.TempDir()
		other, err := privval.LoadOrGenFilePV(
			filepath.Join(otherDir, "priv_validator_key.json"),
			filepath.Join(otherDir, "priv_validator_state.json"),
		)
		if err != nil {
			t.Fatalf("LoadOrGenFilePV in a second home: %v", err)
		}
		if firstAddr == other.GetAddress().String() {
			t.Fatal("two empty homes produced the same consensus key; generation must be fresh " +
				"per home, which is precisely why a mistyped --home is dangerous")
		}
	})
}

// TestLoadOrGenFilePVFreshStateHasNoSignHistory pins the part that makes a regenerated
// key unsafe rather than merely wrong.
//
// A newly generated state file carries height 0 and no last signature, so the
// double-sign protection that state exists to provide starts from nothing. A validator
// pointed at the wrong home therefore has both a new identity and no memory of what the
// old one signed.
func TestLoadOrGenFilePVFreshStateHasNoSignHistory(t *testing.T) {
	dir := t.TempDir()
	pv, err := privval.LoadOrGenFilePV(
		filepath.Join(dir, "priv_validator_key.json"),
		filepath.Join(dir, "priv_validator_state.json"),
	)
	if err != nil {
		t.Fatalf("LoadOrGenFilePV: %v", err)
	}
	if got := pv.LastSignState.Height; got != 0 {
		t.Fatalf("a freshly generated privval reports a signed height of %d; it must start at 0", got)
	}
	if len(pv.LastSignState.Signature) != 0 {
		t.Fatal("a freshly generated privval must carry no last signature")
	}
}

// TestLoadFilePVRequiresBothFiles pins the asymmetry.
//
// LoadOrGenFilePV regenerates when both files are absent, but LoadFilePV — and the
// load-or-gen path once the key exists — requires the state file too. So losing the key
// silently mints a new one, while losing only the state file is a hard failure. The
// safer outcome belongs to the more destructive loss, which is the inversion worth
// recording.
func TestLoadFilePVRequiresBothFiles(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "priv_validator_key.json")
	stateFile := filepath.Join(dir, "priv_validator_state.json")

	if _, err := privval.LoadOrGenFilePV(keyFile, stateFile); err != nil {
		t.Fatalf("seed generation: %v", err)
	}

	// Remove only the state file, keeping the identity.
	if err := os.Remove(stateFile); err != nil {
		t.Fatalf("remove state file: %v", err)
	}
	if _, err := privval.LoadFilePV(keyFile, stateFile); err == nil {
		t.Fatal("a key file with no state file must fail to load; regenerating the state would " +
			"erase double-sign protection for an identity that has already signed")
	}

	// Remove both, and generation is silent again.
	if err := os.Remove(keyFile); err != nil {
		t.Fatalf("remove key file: %v", err)
	}
	if _, err := privval.LoadOrGenFilePV(keyFile, stateFile); err != nil {
		t.Fatalf("with both files absent, generation must succeed: %v", err)
	}
}
