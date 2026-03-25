package wrappers

import (
	"context"
	"fmt"
	"path/filepath"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	ssComposite "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
)

const EVMStoreName = commonevm.EVMStoreKey

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
	cfg.SnapshotInterval = 1000
	cfg.SnapshotMinTimeInterval = 60
	fmt.Printf("Opening memIAVL from directory %s\n", dbDir)
	cs := memiavl.NewCommitStore(dbDir, cfg)
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

func newFlatKVCommitStore(ctx context.Context, dbDir string, config *flatkv.Config) (DBWrapper, error) {
	if config == nil {
		config = flatkv.DefaultConfig()
	}
	config.DataDir = dbDir

	fmt.Printf("Opening flatKV from directory %s\n", dbDir)
	cs, err := flatkv.NewCommitStore(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create FlatKV commit store: %w", err)
	}
	_, err = cs.LoadVersion(0, false)
	if err != nil {
		if closeErr := cs.Close(); closeErr != nil {
			fmt.Printf("failed to close commit store during error recovery: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to load version: %w", err)
	}
	return NewFlatKVWrapper(cs), nil
}

func newCompositeCommitStore(ctx context.Context, dbDir string, writeMode config.WriteMode) (DBWrapper, error) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = writeMode
	cfg.MemIAVLConfig.AsyncCommitBuffer = 10
	cfg.MemIAVLConfig.SnapshotInterval = 100

	cs := composite.NewCompositeCommitStore(ctx, dbDir, cfg)
	if err := cs.CleanupCrashArtifacts(); err != nil {
		return nil, fmt.Errorf("failed to cleanup crash artifacts: %w", err)
	}
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
	return ssComposite.NewCompositeStateStore(cfg, dir)
}

func newSSCompositeStateStore(dbDir string) (DBWrapper, error) {
	fmt.Printf("Opening composite state store from directory %s\n", dbDir)
	store, err := openSSComposite(dbDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open composite state store: %w", err)
	}
	return NewStateStoreWrapper(store), nil
}

func newCombinedCompositeDualSSComposite(ctx context.Context, dbDir string) (DBWrapper, error) {
	fmt.Printf("Opening CompositeDual (SC) + Composite (SS) from directory %s\n", dbDir)
	sc, err := newCompositeCommitStore(ctx, filepath.Join(dbDir, "sc"), config.DualWrite)
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
func NewDBImpl(ctx context.Context, dbType DBType, dataDir string, dbConfig any) (DBWrapper, error) {
	switch dbType {
	case MemIAVL:
		return newMemIAVLCommitStore(dataDir)
	case FlatKV:
		return newFlatKVCommitStore(ctx, dataDir, dbConfig.(*flatkv.Config))
	case CompositeDual:
		return newCompositeCommitStore(ctx, dataDir, config.DualWrite)
	case CompositeSplit:
		return newCompositeCommitStore(ctx, dataDir, config.SplitWrite)
	case CompositeCosmos:
		return newCompositeCommitStore(ctx, dataDir, config.CosmosOnlyWrite)
	case SSComposite:
		return newSSCompositeStateStore(dataDir)
	case CompositeDual_SSComposite:
		return newCombinedCompositeDualSSComposite(ctx, dataDir)
	default:
		return nil, fmt.Errorf("unsupported DB type: %s", dbType)
	}
}
