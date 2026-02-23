package wrappers

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
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

func newMemIAVLCommitStore(
	dataDir string,
) (DBWrapper, error) {

	cfg := memiavl.DefaultConfig()
	cfg.AsyncCommitBuffer = 10
	cfg.SnapshotInterval = 100
	cs := memiavl.NewCommitStore(dataDir, logger.NewNopLogger(), cfg)
	cs.Initialize([]string{EVMStoreName})

	_, err := cs.LoadVersion(0, false)
	if err != nil {
		cs.Close()
		return nil, fmt.Errorf("failed to load version: %w", err)
	}

	return NewMemIAVLWrapper(cs), nil
}

func newFlatKVCommitStore(
	dataDir string,
) (DBWrapper, error) {

	cfg := flatkv.DefaultConfig()
	cfg.Fsync = false
	cs := flatkv.NewCommitStore(dataDir, logger.NewNopLogger(), cfg)

	_, err := cs.LoadVersion(0)
	if err != nil {
		cs.Close()
		return nil, fmt.Errorf("failed to load version: %w", err)
	}

	return NewFlatKVWrapper(cs), nil
}

func newCompositeCommitStore(
	dataDir string,
	writeMode config.WriteMode,
) (DBWrapper, error) {

	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = writeMode
	cfg.MemIAVLConfig.AsyncCommitBuffer = 10
	cfg.MemIAVLConfig.SnapshotInterval = 100

	cs := composite.NewCompositeCommitStore(dataDir, logger.NewNopLogger(), cfg)
	cs.Initialize([]string{EVMStoreName})

	loaded, err := cs.LoadVersion(0, false)
	if err != nil {
		cs.Close()
		return nil, fmt.Errorf("failed to load version: %w", err)
	}

	loadedStore := loaded.(*composite.CompositeCommitStore)

	return NewCompositeWrapper(loadedStore), nil
}

// Instantiates a new DBWrapper based on the given DBType.
func NewDBImpl(
	dbType DBType,
	dataDir string,
) (DBWrapper, error) {

	switch dbType {
	case MemIAVL:
		return newMemIAVLCommitStore(dataDir)
	case FlatKV:
		return newFlatKVCommitStore(dataDir)
	case CompositeDual:
		return newCompositeCommitStore(dataDir, config.DualWrite)
	case CompositeSplit:
		return newCompositeCommitStore(dataDir, config.SplitWrite)
	case CompositeCosmos:
		return newCompositeCommitStore(dataDir, config.CosmosOnlyWrite)
	default:
		return nil, fmt.Errorf("unsupported DB type: %s", dbType)
	}

}
