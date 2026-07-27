package keeper_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/upgrade/keeper"
)

// data/upgrade-info.json is written by a node as it halts for an upgrade and read back
// by the next binary during app construction. It is configuration in the sense that
// matters here: its location is derived from --home, and what the reader does with a
// damaged file decides whether the node starts.
//
// The two outcomes are deliberately asymmetric. An absent file means "no upgrade
// pending" and boots; a present-but-unparseable file is an error on every start until
// someone intervenes. That is the right way round — guessing at a half-written upgrade
// plan would be worse — but it means a truncated write during a halt turns into a
// permanent boot failure rather than a skipped upgrade.
//
// Reading it also has a side effect: resolving the path creates the data directory.

// newDiskKeeper builds a keeper with just enough state to read the file. The disk read
// touches neither the store nor the codec.
func newDiskKeeper(home string) keeper.Keeper {
	return keeper.NewKeeper(nil, nil, nil, home, nil)
}

// FuzzReadUpgradeInfoFromDisk pins the parse outcome across arbitrary file contents.
//
// Valid JSON for the plan shape is returned; anything else is an error. The property
// that matters is totality in one direction: the reader must never return a
// partially-populated plan alongside a nil error, because a plan with a name and no
// height would schedule an upgrade at block zero.
func FuzzReadUpgradeInfoFromDisk(f *testing.F) {
	f.Add(`{"name":"v6.6","height":100}`)
	f.Add(`{"name":"v6.6","height":100,"info":"x"}`)
	f.Add(`{}`)
	f.Add(`{"name":"v6.6"`) // truncated
	f.Add(``)
	f.Add(`null`)
	f.Add(`[]`)
	f.Add(`{"name":123,"height":"x"}`)
	f.Add("\x00\x01")

	f.Fuzz(func(t *testing.T, contents string) {
		home := t.TempDir()
		k := newDiskKeeper(home)

		path, err := k.GetUpgradeInfoPath()
		if err != nil {
			t.Fatalf("resolve upgrade info path: %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		info, err := k.ReadUpgradeInfoFromDisk()
		if err != nil {
			// A file the reader rejects keeps the node down, which is the pinned
			// direction; nothing else to assert.
			return
		}
		// Success must mean a coherent plan: a named upgrade needs a height.
		if info.Name != "" && info.Height == 0 {
			t.Fatalf("contents %q parsed to a named upgrade (%q) with height 0; a plan that "+
				"schedules an upgrade at block zero must not be reported as valid",
				contents, info.Name)
		}
	})
}

// TestReadUpgradeInfoFromDiskAbsentFileMeansNoUpgrade pins the boot-friendly half: no
// file is not an error, because most nodes have no upgrade pending most of the time.
func TestReadUpgradeInfoFromDiskAbsentFileMeansNoUpgrade(t *testing.T) {
	k := newDiskKeeper(t.TempDir())

	info, err := k.ReadUpgradeInfoFromDisk()
	if err != nil {
		t.Fatalf("an absent upgrade-info.json must not be an error: %v", err)
	}
	if info.Name != "" || info.Height != 0 {
		t.Fatalf("an absent file must resolve to an empty plan, got %+v", info)
	}
}

// TestReadUpgradeInfoFromDiskMalformedFileBlocksEveryStart pins the other half, and the
// asymmetry between them. A damaged file is not treated as "no upgrade" — it fails, and
// it keeps failing, because nothing rewrites or clears it.
func TestReadUpgradeInfoFromDiskMalformedFileBlocksEveryStart(t *testing.T) {
	home := t.TempDir()
	k := newDiskKeeper(home)

	path, err := k.GetUpgradeInfoPath()
	if err != nil {
		t.Fatalf("resolve upgrade info path: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"name":"v6.6",`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	for attempt := range 3 {
		if _, err := k.ReadUpgradeInfoFromDisk(); err == nil {
			t.Fatalf("attempt %d: a malformed upgrade-info.json must be an error rather than "+
				"being treated as no upgrade pending", attempt)
		}
	}
	// And the file is still there, so the failure is permanent without operator action.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the reader must not delete or rewrite the file it could not parse: %v", err)
	}
}

// TestGetUpgradeInfoPathCreatesTheDataDirectory records the side effect: resolving where
// the file would be creates data/ under the home, so a read that finds nothing still
// leaves a directory behind. A mistyped --home therefore provisions a tree rather than
// reporting that the home is wrong.
func TestGetUpgradeInfoPathCreatesTheDataDirectory(t *testing.T) {
	home := t.TempDir()
	dataDir := filepath.Join(home, "data")

	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("fixture must start without data/: %v", err)
	}

	k := newDiskKeeper(home)
	if _, err := k.GetUpgradeInfoPath(); err != nil {
		t.Fatalf("GetUpgradeInfoPath: %v", err)
	}

	info, err := os.Stat(dataDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("resolving the path must create data/ as a side effect, got %v", err)
	}
}
