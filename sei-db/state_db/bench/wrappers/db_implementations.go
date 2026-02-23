package wrappers

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
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
	cfg.Fsync = false
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

// Instantiates a new DBWrapper based on the given DBType.
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
