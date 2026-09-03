package flatkv

import (
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// This file holds how FlatKV locates and opens its injected state WAL. The WAL's use during normal
// operation lives with the operations that use it — writes in Commit (store_write.go), replay in
// replayIntoMutableStore / replayIntoReadOnlyCopy (store_replay.go), tail-truncate/prune in Rollback and
// tryTruncateWAL (snapshot.go), and the
// reopen of a closed instance in reopenWAL (store.go, next to resetForImport).

// OpenStateWAL opens (or creates, recovering any prior session) the state WAL for a FlatKV store configured
// by cfg, at the conventional changelog directory under the store's data dir. The returned instance is
// injected into NewCommitStore; the outer context constructs it here as the intermediate step toward
// managing the WAL entirely outside FlatKV. Pass nil to NewCommitStore instead to have FlatKV skip all WAL
// operations (e.g. read-only export clones).
func OpenStateWAL(cfg *config.Config) (statewal.StateWAL, error) {
	return statewal.New(StateWALConfig(cfg.DataDir))
}

// StateWALConfig returns the configuration of the state WAL for a store whose data directory is dir: a
// "changelog" subdirectory of it, with a fixed instance name used only to label metrics.
//
// It is the single definition of that layout, so a caller owning the WAL itself locates it through this
// rather than restating the convention — to open it, to read its range offline, or to cut its tail.
func StateWALConfig(dir string) *statewal.Config {
	return statewal.DefaultConfig(filepath.Join(dir, changelogDir), "flatkv")
}
