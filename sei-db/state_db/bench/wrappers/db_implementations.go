package wrappers

import (
	"fmt"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	ssComposite "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
)

const EVMStoreName = "evm"

type DBType string

const (
	MemIAVL         DBType = "MemIAVL"
	FlatKV          DBType = "FlatKV"
	CompositeDual   DBType = "CompositeDual"
	CompositeSplit  DBType = "CompositeSplit"
	CompositeCosmos DBType = "CompositeCosmos"

	SSComposite               DBType = "SSComposite"
	CompositeDual_SSComposite DBType = "CompositeDual+SSComposite"
)

func newMemIAVLCommitStore(dbDir string) (DBWrapper, error) {
	cfg := memiavl.DefaultConfig()
	cfg.AsyncCommitBuffer = 10
	cfg.SnapshotInterval = 100
	fmt.Printf("Opening memIAVL from directory %s\n", dbDir)
	cs := memiavl.NewCommitStore(dbDir, logger.NewNopLogger(), cfg)
	cs.Initialize([]string{EVMStoreName})
	_, err := cs.LoadVersion(0, false)
	if err != nil {
		if closeErr := cs.Close(); closeErr != nil {
			fmt.Printf("failed to close commit store during error recovery: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to load version: %w", err)
	}
	return NewMemIAVLWrapper(cs), nil
}

func newFlatKVCommitStore(dbDir string) (DBWrapper, error) {
	cfg := flatkv.DefaultConfig()
	cfg.Fsync = false
	fmt.Printf("Opening flatKV from directory %s\n", dbDir)
	cs := flatkv.NewCommitStore(dbDir, logger.NewNopLogger(), cfg)
	_, err := cs.LoadVersion(0)
	if err != nil {
		if closeErr := cs.Close(); closeErr != nil {
			fmt.Printf("failed to close commit store during error recovery: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to load version: %w", err)
	}
	return NewFlatKVWrapper(cs), nil
}

func newCompositeCommitStore(dbDir string, writeMode config.WriteMode) (DBWrapper, error) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = writeMode
	cfg.MemIAVLConfig.AsyncCommitBuffer = 10
	cfg.MemIAVLConfig.SnapshotInterval = 100

	cs := composite.NewCompositeCommitStore(dbDir, logger.NewNopLogger(), cfg)
	cs.Initialize([]string{EVMStoreName})

	loaded, err := cs.LoadVersion(0, false)
	if err != nil {
		if closeErr := cs.Close(); closeErr != nil {
			fmt.Printf("failed to close commit store during error recovery: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to load version: %w", err)
	}

	loadedStore := loaded.(*composite.CompositeCommitStore)

	return NewCompositeWrapper(loadedStore), nil
}

func openSSComposite(dir string) (*ssComposite.CompositeStateStore, error) {
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = config.PebbleDBBackend
	cfg.AsyncWriteBuffer = 0
	cfg.WriteMode = config.DualWrite
	cfg.ReadMode = config.EVMFirstRead
	return ssComposite.NewCompositeStateStore(cfg, dir, logger.NewNopLogger())
}

func newSSCompositeStateStore(dbDir string) (DBWrapper, error) {
	fmt.Printf("Opening composite state store from directory %s\n", dbDir)
	store, err := openSSComposite(dbDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open composite state store: %w", err)
	}
	return NewStateStoreWrapper(store), nil
}

func newCombinedCompositeDualSSComposite(dbDir string) (DBWrapper, error) {
	fmt.Printf("Opening CompositeDual (SC) + Composite (SS) from directory %s\n", dbDir)
	sc, err := newCompositeCommitStore(filepath.Join(dbDir, "sc"), config.DualWrite)
	if err != nil {
		return nil, fmt.Errorf("failed to create SC store: %w", err)
	}
	ss, err := openSSComposite(filepath.Join(dbDir, "ss"))
	if err != nil {
		_ = sc.Close()
		return nil, fmt.Errorf("failed to create SS store: %w", err)
	}
	return NewCombinedWrapper(sc, ss), nil
}

// NewDBImpl instantiates a new empty DBWrapper based on the given DBType.
func NewDBImpl(dbType DBType, dataDir string) (DBWrapper, error) {
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
	case SSComposite:
		return newSSCompositeStateStore(dataDir)
	case CompositeDual_SSComposite:
		return newCombinedCompositeDualSSComposite(dataDir)
	default:
		return nil, fmt.Errorf("unsupported DB type: %s", dbType)
	}
}
