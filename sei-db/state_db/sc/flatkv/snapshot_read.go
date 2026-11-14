package flatkv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/cockroachdb/pebble/v2"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// SnapshotStats is the metadata a FlatKV snapshot records about its rows.
type SnapshotStats struct {
	RowCount int64
	Bytes    int64
	// StateHash is the checksum of the store-wide LtHash, which the app hash holds as its state hash.
	StateHash [32]byte
}

// SnapshotDirStats returns the stats that the metadata of snapshot dir records.
func SnapshotDirStats(dir string, version int64) (SnapshotStats, error) {
	dbs, err := openSnapshotDataDBs(dir)
	if err != nil {
		return SnapshotStats{}, err
	}
	defer closeSnapshotDataDBs(dbs)
	return snapshotStats(dbs, version)
}

// ExportSnapshotDir emits the live non-metadata rows of snapshot dir in ascending physical-key order, and
// returns the stats that the snapshot metadata records. It returns an error if the count or key+value bytes
// of the emitted rows differ from those stats. emit must not keep key or value after it returns.
func ExportSnapshotDir(ctx context.Context, dir string, version int64, emit func(key, value []byte) error) (SnapshotStats, error) {
	dbs, err := openSnapshotDataDBs(dir)
	if err != nil {
		return SnapshotStats{}, err
	}
	defer closeSnapshotDataDBs(dbs)

	recorded, err := snapshotStats(dbs, version)
	if err != nil {
		return SnapshotStats{}, err
	}

	cursors, err := openSnapshotCursors(dbs)
	if err != nil {
		return SnapshotStats{}, err
	}
	defer closeSnapshotCursors(cursors)

	var emittedRows, emittedBytes int64
	for {
		if err := ctx.Err(); err != nil {
			return SnapshotStats{}, err
		}
		next := minSnapshotCursor(cursors)
		if next == nil {
			break
		}
		key := next.iter.Key()
		value, err := next.iter.ValueAndErr()
		if err != nil {
			return SnapshotStats{}, fmt.Errorf("read %s value %x: %w", next.name, key, err)
		}
		emittedRows++
		emittedBytes += int64(len(key)) + int64(len(value))
		if err := emit(key, value); err != nil {
			return SnapshotStats{}, err
		}
		next.iter.Next()
		if err := next.err(); err != nil {
			return SnapshotStats{}, err
		}
		next.skipMeta()
	}
	if emittedRows != recorded.RowCount || emittedBytes != recorded.Bytes {
		return SnapshotStats{}, fmt.Errorf("%s holds %d rows and %d bytes, its metadata records %d rows and %d bytes",
			dir, emittedRows, emittedBytes, recorded.RowCount, recorded.Bytes)
	}
	return recorded, nil
}

type snapshotDataDB struct {
	name string
	db   *pebble.DB
}

func openSnapshotDataDBs(dir string) ([]snapshotDataDB, error) {
	dbs := make([]snapshotDataDB, 0, len(dataDBDirs))
	for _, name := range dataDBDirs {
		db, err := openSnapshotPebble(filepath.Join(dir, name))
		if err != nil {
			closeSnapshotDataDBs(dbs)
			return nil, fmt.Errorf("open %s database: %w", name, err)
		}
		dbs = append(dbs, snapshotDataDB{name: name, db: db})
	}
	return dbs, nil
}

func closeSnapshotDataDBs(dbs []snapshotDataDB) {
	for _, db := range dbs {
		_ = db.db.Close()
	}
}

func snapshotStats(dbs []snapshotDataDB, version int64) (SnapshotStats, error) {
	var stats SnapshotStats
	perDB := make(map[string]*lthash.LtHash, len(dbs))
	for _, db := range dbs {
		meta, err := readSnapshotLocalMeta(db)
		if err != nil {
			return SnapshotStats{}, err
		}
		if meta.CommittedVersion != version {
			return SnapshotStats{}, fmt.Errorf("%s records version %d, want %d", db.name, meta.CommittedVersion, version)
		}
		if meta.LtHash == nil {
			return SnapshotStats{}, fmt.Errorf("%s records no LtHash", db.name)
		}
		if err := validatePerModuleMetadata(db.name, meta); err != nil {
			return SnapshotStats{}, err
		}
		perDB[db.name] = meta.LtHash
		for _, moduleStats := range meta.ModuleStats {
			stats.RowCount += moduleStats.KeyCount
			stats.Bytes += moduleStats.Bytes
		}
	}
	stats.StateHash = lthash.SumDBHashes(dataDBDirs, perDB).Checksum()
	return stats, nil
}

func readSnapshotLocalMeta(db snapshotDataDB) (*LocalMeta, error) {
	iter, err := db.db.NewIter(&pebble.IterOptions{
		LowerBound: ktype.MetaKeyPrefixBytes,
		UpperBound: ktype.PrefixEnd(ktype.MetaKeyPrefixBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("open metadata iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	var decoder LocalMetaDecoder
	for valid := iter.First(); valid; valid = iter.Next() {
		value, err := iter.ValueAndErr()
		if err != nil {
			return nil, fmt.Errorf("read metadata value %x: %w", iter.Key(), err)
		}
		decoder.Add(iter.Key(), value)
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterate metadata: %w", err)
	}
	return decoder.Finish()
}

type snapshotCursor struct {
	name string
	iter *pebble.Iterator
}

func openSnapshotCursors(dbs []snapshotDataDB) ([]*snapshotCursor, error) {
	cursors := make([]*snapshotCursor, 0, len(dbs))
	for _, db := range dbs {
		iter, err := db.db.NewIter(nil)
		if err != nil {
			closeSnapshotCursors(cursors)
			return nil, fmt.Errorf("open %s iterator: %w", db.name, err)
		}
		c := &snapshotCursor{name: db.name, iter: iter}
		iter.First()
		if err := c.err(); err != nil {
			_ = iter.Close()
			closeSnapshotCursors(cursors)
			return nil, err
		}
		c.skipMeta()
		if err := c.err(); err != nil {
			_ = iter.Close()
			closeSnapshotCursors(cursors)
			return nil, err
		}
		cursors = append(cursors, c)
	}
	return cursors, nil
}

func closeSnapshotCursors(cursors []*snapshotCursor) {
	for _, c := range cursors {
		_ = c.iter.Close()
	}
}

func (c *snapshotCursor) skipMeta() {
	for c.iter.Valid() && ktype.IsMetaKey(c.iter.Key()) {
		c.iter.Next()
	}
}

func (c *snapshotCursor) err() error {
	if err := c.iter.Error(); err != nil {
		return fmt.Errorf("iterate %s database: %w", c.name, err)
	}
	return nil
}

func minSnapshotCursor(cursors []*snapshotCursor) *snapshotCursor {
	var min *snapshotCursor
	for _, c := range cursors {
		if !c.iter.Valid() {
			continue
		}
		if min == nil || bytes.Compare(c.iter.Key(), min.iter.Key()) < 0 {
			min = c
		}
	}
	return min
}

// ReadSnapshotMiscValue returns the value of key in the non-EVM module moduleName in the snapshot
// directory dir, and whether the snapshot holds that row. It opens the snapshot's misc database
// read-only and does not change its data files. It returns an error if dir has no misc database.
func ReadSnapshotMiscValue(dir, moduleName string, key []byte) ([]byte, bool, error) {
	if moduleName == keys.EVMStoreKey {
		return nil, false, fmt.Errorf("module %q is not a misc module", moduleName)
	}
	dbDir := filepath.Join(dir, miscDBDir)
	db, err := openSnapshotPebble(dbDir)
	if err != nil {
		return nil, false, fmt.Errorf("open %q read-only: %w", dbDir, err)
	}
	defer func() { _ = db.Close() }()

	raw, closer, err := db.Get(ktype.BuildPhysicalKey(moduleName, key))
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s/%x in %q: %w", moduleName, key, dbDir, err)
	}
	defer func() { _ = closer.Close() }()
	row, err := vtype.DeserializeMiscData(raw)
	if err != nil {
		return nil, false, fmt.Errorf("decode %s/%x in %q: %w", moduleName, key, dbDir, err)
	}
	if row.IsDelete() {
		return nil, false, nil
	}
	return row.GetValue(), true, nil
}

func openSnapshotPebble(dbDir string) (*pebble.DB, error) {
	return pebble.Open(dbDir, &pebble.Options{
		ReadOnly:           true,
		FormatMajorVersion: pebble.FormatVirtualSSTables,
	})
}
