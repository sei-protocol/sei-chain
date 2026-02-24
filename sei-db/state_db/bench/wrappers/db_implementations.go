package wrappers

import (
	"fmt"
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

func newMemIAVLCommitStore(b *testing.B, dbDir string) DBWrapper {
	b.Helper()
	cfg := memiavl.DefaultConfig()
	cfg.AsyncCommitBuffer = 10
	cfg.SnapshotInterval = 100
	fmt.Printf("Opening memIAVL from directory %s\n", dbDir)
	cs := memiavl.NewCommitStore(dbDir, logger.NewNopLogger(), cfg)
	cs.Initialize([]string{EVMStoreName})
	_, err := cs.LoadVersion(0, false)
	require.NoError(b, err)
	return NewMemIAVLWrapper(cs)
}

func newFlatKVCommitStore(b *testing.B, dbDir string) DBWrapper {
	b.Helper()
	cfg := flatkv.DefaultConfig()
	cfg.Fsync = false
	fmt.Printf("Opening flatKV from directory %s\n", dbDir)
	cs := flatkv.NewCommitStore(dbDir, logger.NewNopLogger(), cfg)
	_, err := cs.LoadVersion(0)
	require.NoError(b, err)
	return NewFlatKVWrapper(cs)
}

func newCompositeCommitStore(b *testing.B, dbDir string, writeMode config.WriteMode) DBWrapper {
	b.Helper()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = writeMode
	cfg.MemIAVLConfig.AsyncCommitBuffer = 10
	cfg.MemIAVLConfig.SnapshotInterval = 100

	cs := composite.NewCompositeCommitStore(dbDir, logger.NewNopLogger(), cfg)
	cs.Initialize([]string{EVMStoreName})

	loaded, err := cs.LoadVersion(0, false)
	require.NoError(b, err)

	loadedStore := loaded.(*composite.CompositeCommitStore)

	return NewCompositeWrapper(loadedStore)
}

// NewDBImpl instantiates a new empty DBWrapper based on the given DBType.
func NewDBImpl(
	b *testing.B,
	dbDir string,
	dbType DBType,
) DBWrapper {
	switch dbType {
	case MemIAVL:
		return newMemIAVLCommitStore(b, dbDir)
	case FlatKV:
		return newFlatKVCommitStore(b, dbDir)
	case CompositeDual:
		return newCompositeCommitStore(b, dbDir, config.DualWrite)
	case CompositeSplit:
		return newCompositeCommitStore(b, dbDir, config.SplitWrite)
	case CompositeCosmos:
		return newCompositeCommitStore(b, dbDir, config.CosmosOnlyWrite)
	default:
		b.Fatalf("unsupported DB type: %s", dbType)
		return nil
	}
}
