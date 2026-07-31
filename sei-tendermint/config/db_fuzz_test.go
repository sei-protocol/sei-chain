package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Where a node's databases live is not stated in config.toml. db-dir names a base
// directory, and the layout underneath it is chosen at open time by looking at what is
// already on disk: if a legacy flat file exists, the flat layout is kept forever;
// otherwise the subdirectory layout is used.
//
// So filesystem state, not configuration, decides the path — and the decision is
// sticky. A node migrated to the new layout would look in the wrong place, which is
// why the fallback exists; the cost is that "what does db-dir mean" has no answer
// without listing the directory.

// FuzzResolveDBDirLayoutFallback pins the layout choice per database identifier.
//
// The five identifiers that moved (blockstore, tx_index, state, evidence, peerstore)
// resolve into a tendermint/ subdirectory unless a legacy file for that identifier
// already sits in the base directory. Every other identifier has no subdirectory and
// resolves to the base directory unchanged.
//
// The check is per-identifier, not per-node, so a home carrying only a legacy
// blockstore.db keeps the flat path for blockstore and takes the new path for the
// others — a mixed layout that no config value describes.
func FuzzResolveDBDirLayoutFallback(f *testing.F) {
	f.Add("blockstore", false)
	f.Add("blockstore", true)
	f.Add("tx_index", true)
	f.Add("state", false)
	f.Add("evidence", true)
	f.Add("peerstore", false)
	f.Add("application", false) // not one of the moved identifiers
	f.Add("application", true)
	f.Add("", false)

	f.Fuzz(func(t *testing.T, dbID string, legacyPresent bool) {
		// Identifiers are code-supplied, not operator-supplied, so keep to values that
		// can name a file.
		if dbID == "" || len(dbID) > 32 ||
			filepath.Base(dbID) != dbID || dbID == "." || dbID == ".." {
			return
		}
		base := t.TempDir()

		if legacyPresent {
			legacy := filepath.Join(base, dbID+".db")
			if err := os.MkdirAll(legacy, 0o750); err != nil {
				t.Skipf("create legacy dir: %v", err)
			}
		}

		got := ResolveDBDir(dbID, base)

		moved := map[string]bool{
			"blockstore": true, "tx_index": true, "state": true,
			"evidence": true, "peerstore": true,
		}
		want := base
		if moved[dbID] && !legacyPresent {
			want = filepath.Join(base, "tendermint")
		}

		if got != want {
			t.Fatalf("ResolveDBDir(%q, base) = %q, want %q (legacyPresent=%v). The layout is "+
				"decided by what is on disk, per identifier", dbID, got, want, legacyPresent)
		}
	})
}

// TestResolveDBDirLegacyLayoutIsSticky pins the consequence: once a legacy file exists
// for an identifier, the flat path is returned on every subsequent open, so a node can
// never be moved to the new layout by changing configuration alone.
func TestResolveDBDirLegacyLayoutIsSticky(t *testing.T) {
	base := t.TempDir()

	fresh := ResolveDBDir("blockstore", base)
	if fresh != filepath.Join(base, "tendermint") {
		t.Fatalf("a fresh home must use the subdirectory layout, got %q", fresh)
	}

	if err := os.MkdirAll(filepath.Join(base, "blockstore.db"), 0o750); err != nil {
		t.Fatalf("create legacy dir: %v", err)
	}
	withLegacy := ResolveDBDir("blockstore", base)
	if withLegacy != base {
		t.Fatalf("a home with legacy data must keep the flat layout, got %q", withLegacy)
	}
	if withLegacy == fresh {
		t.Fatal("the legacy fallback no longer changes the resolved directory; if the layout is " +
			"now unconditional, existing nodes need a data migration")
	}
}

// TestResolveDBDirIsPerIdentifier records the mixed-layout case, which is the part an
// operator cannot infer: one legacy file pins one database to the old path and leaves
// the rest on the new one.
func TestResolveDBDirIsPerIdentifier(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "blockstore.db"), 0o750); err != nil {
		t.Fatalf("create legacy dir: %v", err)
	}

	if got := ResolveDBDir("blockstore", base); got != base {
		t.Fatalf("blockstore has legacy data and must use the flat path, got %q", got)
	}
	if got := ResolveDBDir("state", base); got != filepath.Join(base, "tendermint") {
		t.Fatalf("state has no legacy data and must use the subdirectory path, got %q", got)
	}
}

// TestDefaultDBProviderUsesTheConfiguredBackend pins the one part of the DB path that
// config does decide: db-backend selects the engine, and an unknown name is an error
// at open rather than a fallback.
func TestDefaultDBProviderUsesTheConfiguredBackend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SetRoot(t.TempDir())
	cfg.DBBackend = "definitely-not-a-backend"

	if _, err := DefaultDBProvider(&DBContext{ID: "state", Config: cfg}); err == nil {
		t.Fatal("an unknown db-backend must fail at open; a silent fallback would put a node's " +
			"state in an engine nobody chose")
	}
}
