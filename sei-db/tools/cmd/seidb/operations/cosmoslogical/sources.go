package cosmoslogical

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/tools/cmd/seidb/operations"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

// rowFunc receives one row of one module. key and value are owned by the caller only for the
// duration of the call.
type rowFunc func(module string, key, value []byte) error

// memiavlSource reads the trees of one memiavl height.
type memiavlSource struct {
	openMode    string
	description string
	version     int64
	// trees names every tree present at version, ordered by name.
	trees []string
	// scan streams the rows of module in ascending key order. module must be in trees.
	scan  func(module string, fn func(key, value []byte) error) error
	close func()
}

// openMemiavlSource opens the memiavl root dir at height with openMode.
func openMemiavlSource(dbDir string, height int64, openMode string) (*memiavlSource, error) {
	switch openMode {
	case openModeSnapshot:
		return openMemiavlSnapshotSource(dbDir, height)
	case openModeChangelog:
		return openMemiavlChangelogSource(dbDir, height)
	case openModeReplay:
		return openMemiavlReplaySource(dbDir, height)
	default:
		return nil, fmt.Errorf("unknown --memiavl-open-mode %q (want %s, %s or %s)",
			openMode, openModeSnapshot, openModeChangelog, openModeReplay)
	}
}

// openMemiavlSnapshotSource reads the snapshot at height, where 0 selects the snapshot the
// current link names.
func openMemiavlSnapshotSource(dbDir string, height int64) (*memiavlSource, error) {
	snapshotDir, err := resolveSnapshotDir(dbDir, height)
	if err != nil {
		return nil, err
	}
	trees, err := listSnapshotTrees(snapshotDir)
	if err != nil {
		return nil, err
	}
	version, err := snapshotTreesVersion(snapshotDir, trees)
	if err != nil {
		return nil, err
	}
	return &memiavlSource{
		openMode:    openModeSnapshot,
		description: snapshotDir + " (snapshot kvs files, no changelog)",
		version:     version,
		trees:       trees,
		scan: func(module string, fn func(key, value []byte) error) error {
			return scanSnapshotTree(filepath.Join(snapshotDir, module), fn)
		},
		close: func() {},
	}, nil
}

// openMemiavlChangelogSource reads the newest snapshot at or below height merged with the
// changelog writes above it up to height.
func openMemiavlChangelogSource(dbDir string, height int64) (*memiavlSource, error) {
	if height <= 0 {
		return nil, errors.New("--memiavl-open-mode changelog needs --height above 0")
	}
	// The snapshot the replay starts from is only known once a replay ran, so the first
	// tree read names it, and every later tree must start from the same one.
	probe, err := memiavl.ReplayTreeChangelog(dbDir, height, keys.BankStoreKey, func(int64, proto.ChangeSet) error { return nil })
	if err != nil {
		return nil, changelogError(dbDir, err)
	}
	trees, err := listSnapshotTrees(probe.SnapshotDir)
	if err != nil {
		return nil, err
	}
	return &memiavlSource{
		openMode: openModeChangelog,
		description: fmt.Sprintf("%s merged with changelog versions %d to %d",
			probe.SnapshotDir, probe.StartVersion, probe.Version),
		version: probe.Version,
		trees:   trees,
		scan: func(module string, fn func(key, value []byte) error) error {
			overlay, r, err := readChangelogOverlay(dbDir, height, module)
			if err != nil {
				return err
			}
			if r.SnapshotDir != probe.SnapshotDir {
				return fmt.Errorf("memiavl snapshot changed from %s to %s during the scan; rerun",
					probe.SnapshotDir, r.SnapshotDir)
			}
			return scanSnapshotTreeWithOverlay(filepath.Join(r.SnapshotDir, module), overlay, fn)
		},
		close: func() {},
	}, nil
}

// openMemiavlReplaySource opens a read-only memiavl DB replayed to height.
func openMemiavlReplaySource(dbDir string, height int64) (*memiavlSource, error) {
	db, err := memiavl.OpenDB(height, memiavl.Options{
		Dir:               dbDir,
		ReadOnly:          true,
		ZeroCopy:          true,
		NoChangelogRepair: true,
	})
	if err != nil {
		return nil, changelogError(dbDir, err)
	}
	var trees []string
	for _, t := range db.Trees() {
		trees = append(trees, t.Name)
	}
	return &memiavlSource{
		openMode:    openModeReplay,
		description: dbDir + " (read-only memiavl DB replayed from snapshot and changelog)",
		version:     db.Version(),
		trees:       trees,
		scan: func(module string, fn func(key, value []byte) error) error {
			tree := db.TreeByName(module)
			if tree == nil {
				return fmt.Errorf("memiavl tree %q not found", module)
			}
			iter := tree.Iterator(nil, nil, true)
			defer func() { _ = iter.Close() }()
			for ; iter.Valid(); iter.Next() {
				if err := fn(iter.Key(), iter.Value()); err != nil {
					return err
				}
			}
			return iter.Error()
		},
		close: func() { _ = db.Close() },
	}, nil
}

// changelogError explains a changelog read failure. A torn last record is what a node that
// is writing a block looks like, so it asks for a rerun.
func changelogError(dbDir string, err error) error {
	if errors.Is(err, wal.ErrCorrupt) {
		return fmt.Errorf("the changelog under %s ends mid-record, which is what a node writing a "+
			"block looks like; rerun, and if it keeps failing the changelog is corrupt: %w", dbDir, err)
	}
	return fmt.Errorf("read memiavl %s: %w", dbDir, err)
}

// resolveSnapshotDir returns the memiavl snapshot directory at height, where 0 selects the
// snapshot the current link names.
func resolveSnapshotDir(dbDir string, height int64) (string, error) {
	name := fmt.Sprintf("snapshot-%020d", height)
	if height == 0 {
		link, err := os.Readlink(filepath.Join(dbDir, "current"))
		if err != nil {
			return "", fmt.Errorf("read current snapshot link: %w", err)
		}
		name = link
	}
	dir := filepath.Join(dbDir, name)
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("memiavl snapshot %s: %w (use --memiavl-open-mode changelog or replay "+
			"for a height without a snapshot)", dir, err)
	}
	return dir, nil
}

// listSnapshotTrees returns the tree directories of a memiavl snapshot, ordered by name.
func listSnapshotTrees(snapshotDir string) ([]string, error) {
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		return nil, fmt.Errorf("list memiavl snapshot %s: %w", snapshotDir, err)
	}
	var trees []string
	for _, e := range entries {
		if e.IsDir() {
			trees = append(trees, e.Name())
		}
	}
	sort.Strings(trees)
	return trees, nil
}

// snapshotTreesVersion returns the version that every tree of a snapshot records, and fails
// when two trees disagree.
func snapshotTreesVersion(snapshotDir string, trees []string) (int64, error) {
	version := int64(-1)
	for _, tree := range trees {
		v, err := readTreeVersion(filepath.Join(snapshotDir, tree))
		if err != nil {
			return 0, err
		}
		if version >= 0 && v != version {
			return 0, fmt.Errorf("memiavl snapshot %s: tree %s is at version %d, others at %d",
				snapshotDir, tree, v, version)
		}
		version = v
	}
	if version < 0 {
		return 0, fmt.Errorf("memiavl snapshot %s has no trees", snapshotDir)
	}
	return version, nil
}

// readTreeVersion reads the version field of a tree's metadata file (magic uint32 | format
// uint32 | version uint32, little-endian).
func readTreeVersion(treeDir string) (int64, error) {
	bz, err := os.ReadFile(filepath.Join(filepath.Clean(treeDir), "metadata"))
	if err != nil {
		return 0, fmt.Errorf("read tree metadata: %w", err)
	}
	if len(bz) < 12 {
		return 0, fmt.Errorf("tree metadata %s too short: %d bytes", treeDir, len(bz))
	}
	return int64(binary.LittleEndian.Uint32(bz[8:])), nil
}

// scanSnapshotTree streams every leaf of a memiavl snapshot tree from its kvs file in ascending
// key order. The file holds one record per leaf, little-endian:
//
//	keyLen uint32 | key [keyLen] | valLen uint32 | value [valLen]
func scanSnapshotTree(treeDir string, fn func(key, value []byte) error) error {
	path := filepath.Join(filepath.Clean(treeDir), "kvs")
	f, err := os.Open(path)
	if err != nil {
		return memiavl.SnapshotPrunedError(filepath.Dir(treeDir), fmt.Errorf("open %s: %w", path, err))
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReaderSize(io.NewSectionReader(f, 0, math.MaxInt64), 16*1024*1024)
	var lenbuf [4]byte
	for {
		if _, err := io.ReadFull(r, lenbuf[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read %s key length: %w", path, err)
		}
		k := make([]byte, binary.LittleEndian.Uint32(lenbuf[:]))
		if _, err := io.ReadFull(r, k); err != nil {
			return fmt.Errorf("read %s key: %w", path, err)
		}
		if _, err := io.ReadFull(r, lenbuf[:]); err != nil {
			return fmt.Errorf("read %s value length: %w", path, err)
		}
		v := make([]byte, binary.LittleEndian.Uint32(lenbuf[:]))
		if _, err := io.ReadFull(r, v); err != nil {
			return fmt.Errorf("read %s value: %w", path, err)
		}
		if err := fn(k, v); err != nil {
			return err
		}
	}
}

// overlayWrite is the last changelog write to one key of one tree.
type overlayWrite struct {
	key     []byte
	value   []byte
	deleted bool
}

// readChangelogOverlay returns the last write to every key of module in the changelog above the
// snapshot that OpenDB(height) loads, sorted by key.
func readChangelogOverlay(dbDir string, height int64, module string) ([]overlayWrite, memiavl.TreeChangelogRange, error) {
	writes := make(map[string]overlayWrite)
	r, err := memiavl.ReplayTreeChangelog(dbDir, height, module, func(_ int64, cs proto.ChangeSet) error {
		for _, pair := range cs.Pairs {
			w := overlayWrite{key: pair.Key, value: pair.Value, deleted: pair.Delete}
			if w.deleted {
				w.value = nil
			} else if w.value == nil {
				// memiavl Tree.Set stores a nil value as an empty one.
				w.value = []byte{}
			}
			writes[string(pair.Key)] = w
		}
		return nil
	})
	if err != nil {
		return nil, memiavl.TreeChangelogRange{}, changelogError(dbDir, err)
	}
	overlay := make([]overlayWrite, 0, len(writes))
	for _, w := range writes {
		overlay = append(overlay, w)
	}
	sort.Slice(overlay, func(i, j int) bool { return bytes.Compare(overlay[i].key, overlay[j].key) < 0 })
	return overlay, r, nil
}

// scanSnapshotTreeWithOverlay streams the leaves of a snapshot tree with overlay applied, in
// ascending key order. overlay must be sorted by key.
func scanSnapshotTreeWithOverlay(treeDir string, overlay []overlayWrite, fn func(key, value []byte) error) error {
	emit := func(w overlayWrite) error {
		if w.deleted {
			return nil
		}
		return fn(w.key, w.value)
	}
	next := 0
	if err := scanSnapshotTree(treeDir, func(k, v []byte) error {
		for ; next < len(overlay) && bytes.Compare(overlay[next].key, k) < 0; next++ {
			if err := emit(overlay[next]); err != nil {
				return err
			}
		}
		if next < len(overlay) && bytes.Equal(overlay[next].key, k) {
			next++
			return emit(overlay[next-1])
		}
		return fn(k, v)
	}); err != nil {
		return err
	}
	for ; next < len(overlay); next++ {
		if err := emit(overlay[next]); err != nil {
			return err
		}
	}
	return nil
}

// flatKVMigrationState is the migrate_cosmos progress that a FlatKV store records.
type flatKVMigrationState struct {
	boundary migration.MigrationBoundary
	// version is the stored migration version, or -1 when the store has none.
	version int64
}

// readFlatKVMigrationState reads the migration version and boundary rows of a FlatKV store, as
// seen by migrate_cosmos. The version 1 row that the EVM migration left stays in place while
// migrate_cosmos runs, so the boundary row decides first.
func readFlatKVMigrationState(store gigatypes.LiveStateStore) (flatKVMigrationState, error) {
	state := flatKVMigrationState{version: -1}
	if data, ok := store.Get(migration.MigrationStore, []byte(migration.MigrationVersionKey)); ok {
		if len(data) != 8 {
			return state, fmt.Errorf("flatkv migration version length=%d, want 8", len(data))
		}
		state.version = int64(binary.BigEndian.Uint64(data)) //nolint:gosec
	}
	if data, ok := store.Get(migration.MigrationStore, []byte(migration.MigrationBoundaryKey)); ok {
		b, err := migration.DeserializeMigrationBoundary(data)
		if err != nil {
			return state, fmt.Errorf("decode flatkv migration boundary: %w", err)
		}
		state.boundary = b
		return state, nil
	}
	if state.version >= migration.Version2_FlatKVOnly {
		state.boundary = migration.MigrationBoundaryComplete
	} else {
		state.boundary = migration.MigrationBoundaryNotStarted
	}
	return state, nil
}

// flatKVSource is a FlatKV store opened at one height.
type flatKVSource struct {
	store gigatypes.LiveStateStore
	state flatKVMigrationState
}

func openFlatKVSource(dbDir string, height int64) (*flatKVSource, error) {
	store, err := operations.OpenFlatKVReadOnly(dbDir, height)
	if err != nil {
		return nil, fmt.Errorf("open flatkv read-only: %w", err)
	}
	state, err := readFlatKVMigrationState(store)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return &flatKVSource{store: store, state: state}, nil
}

func (s *flatKVSource) close() { _ = s.store.Close() }

// scanCosmosRows passes every FlatKV row of a selected Cosmos module to fn with its logical value,
// and returns how many rows name a module that is neither EVM, the migration store, nor a Cosmos
// module.
func (s *flatKVSource) scanCosmosRows(selected func(string) bool, fn rowFunc) (unknown uint64, err error) {
	known := make(map[string]bool)
	for _, m := range cosmosModules() {
		known[m] = true
	}
	iter, err := s.store.RawGlobalIterator()
	if err != nil {
		return 0, fmt.Errorf("raw global iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()
	var seen uint64
	for ; iter.Valid(); iter.Next() {
		seen++
		if seen%20_000_000 == 0 {
			progressf("  flatkv rows read=%d\n", seen)
		}
		module, key, err := ktype.StripModulePrefix(iter.Key())
		if err != nil {
			unknown++
			continue
		}
		if module == keys.EVMStoreKey || module == migration.MigrationStore {
			continue
		}
		if !known[module] {
			unknown++
			continue
		}
		if !selected(module) {
			continue
		}
		misc, err := vtype.DeserializeMiscData(iter.Value())
		if err != nil {
			return unknown, fmt.Errorf("decode flatkv row %X: %w", iter.Key(), err)
		}
		if err := fn(module, key, misc.GetValue()); err != nil {
			return unknown, err
		}
	}
	if err := iter.Error(); err != nil {
		return unknown, fmt.Errorf("iterate flatkv: %w", err)
	}
	return unknown, nil
}
