package wrappers

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

const EVMStoreName = "evm"

type DBType string

const (
	MemIAVL         DBType = "MemIAVL"
	FlatKV          DBType = "FlatKV"
	CompositeDual   DBType = "CompositeDual"
	CompositeSplit  DBType = "CompositeSplit"
	CompositeCosmos DBType = "CompositeCosmos"
)

func newMemIAVLCommitStore(b *testing.B) DBWrapper {
	b.Helper()
	dir := b.TempDir()
	cfg := memiavl.DefaultConfig()
	cfg.AsyncCommitBuffer = 10
	cfg.SnapshotInterval = 100
	cs := memiavl.NewCommitStore(dir, logger.NewNopLogger(), cfg)
	cs.Initialize([]string{EVMStoreName})

	_, err := cs.LoadVersion(0, false)
	require.NoError(b, err)

	b.Cleanup(func() {
		_ = cs.Close()
	})

	return NewMemIAVLWrapper(cs)
}

func newFlatKVCommitStore(b *testing.B) DBWrapper {
	b.Helper()
	dir := b.TempDir()
	cfg := flatkv.DefaultConfig()
	cs := flatkv.NewCommitStore(dir, logger.NewNopLogger(), cfg)

	_, err := cs.LoadVersion(0)
	require.NoError(b, err)

	b.Cleanup(func() {
		_ = cs.Close()
	})

	return NewFlatKVWrapper(cs)
}

func newCompositeCommitStore(b *testing.B, writeMode config.WriteMode) DBWrapper {
	b.Helper()
	dir := b.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = writeMode
	cfg.MemIAVLConfig.AsyncCommitBuffer = 10
	cfg.MemIAVLConfig.SnapshotInterval = 100

	cs := composite.NewCompositeCommitStore(dir, logger.NewNopLogger(), cfg)
	cs.Initialize([]string{EVMStoreName})

	loaded, err := cs.LoadVersion(0, false)
	require.NoError(b, err)

	loadedStore := loaded.(*composite.CompositeCommitStore)

	b.Cleanup(func() {
		_ = loadedStore.Close()
	})

	return NewCompositeWrapper(loadedStore)
}

// NewDBImpl instantiates a new empty DBWrapper based on the given DBType.
func NewDBImpl(
	b *testing.B,
	dbType DBType,
) DBWrapper {

	switch dbType {
	case MemIAVL:
		return newMemIAVLCommitStore(b)
	case FlatKV:
		return newFlatKVCommitStore(b)
	case CompositeDual:
		return newCompositeCommitStore(b, config.DualWrite)
	case CompositeSplit:
		return newCompositeCommitStore(b, config.SplitWrite)
	case CompositeCosmos:
		return newCompositeCommitStore(b, config.CosmosOnlyWrite)
	default:
		b.Fatalf("unsupported DB type: %s", dbType)
		return nil
	}
}

// ImportFunc is called with the Importer to stream snapshot data into the store.
type ImportFunc func(importer types.Importer) (int64, error)

// NewDBImplFromSnapshot creates a commit store, imports a snapshot through the
// native Committer.Importer path, reloads, and returns the wrapped DBWrapper.
// importFn receives the Importer and should call AddModule / AddNode / Close.
func NewDBImplFromSnapshot(
	b *testing.B,
	dbType DBType,
	snapshotHeight int64,
	importFn ImportFunc,
) DBWrapper {
	b.Helper()

	switch dbType {
	case MemIAVL:
		return newMemIAVLFromSnapshot(b, snapshotHeight, importFn)
	case CompositeDual:
		return newCompositeFromSnapshot(b, config.DualWrite, snapshotHeight, importFn)
	case CompositeSplit:
		return newCompositeFromSnapshot(b, config.SplitWrite, snapshotHeight, importFn)
	case CompositeCosmos:
		return newCompositeFromSnapshot(b, config.CosmosOnlyWrite, snapshotHeight, importFn)
	default:
		b.Fatalf("snapshot import not supported for DB type: %s", dbType)
		return nil
	}
}

func newMemIAVLFromSnapshot(b *testing.B, height int64, importFn ImportFunc) DBWrapper {
	b.Helper()
	dir := b.TempDir()
	cfg := memiavl.DefaultConfig()
	cfg.AsyncCommitBuffer = 10
	cfg.SnapshotInterval = 100
	cs := memiavl.NewCommitStore(dir, logger.NewNopLogger(), cfg)

	importer, err := cs.Importer(height)
	require.NoError(b, err)

	_, err = importFn(importer)
	require.NoError(b, err)
	require.NoError(b, importer.Close())

	_, err = cs.LoadVersion(0, false)
	require.NoError(b, err)

	b.Cleanup(func() { _ = cs.Close() })
	return NewMemIAVLWrapper(cs)
}

func newCompositeFromSnapshot(b *testing.B, writeMode config.WriteMode, height int64, importFn ImportFunc) DBWrapper {
	b.Helper()
	dir := b.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = writeMode
	cfg.MemIAVLConfig.AsyncCommitBuffer = 10
	cfg.MemIAVLConfig.SnapshotInterval = 100

	cs := composite.NewCompositeCommitStore(dir, logger.NewNopLogger(), cfg)

	importer, err := cs.Importer(height)
	require.NoError(b, err)

	_, err = importFn(importer)
	require.NoError(b, err)
	require.NoError(b, importer.Close())

	loaded, err := cs.LoadVersion(0, false)
	require.NoError(b, err)
	loadedStore := loaded.(*composite.CompositeCommitStore)

	b.Cleanup(func() { _ = loadedStore.Close() })
	return NewCompositeWrapper(loadedStore)
}
