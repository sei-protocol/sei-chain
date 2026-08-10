package wrappers

import (
	"context"
	"fmt"
	"path/filepath"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvConfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	ssComposite "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
)

const EVMStoreName = commonevm.EVMStoreKey

type DBType string

const (
	NoOp            DBType = "NoOp"
	MemIAVL         DBType = "MemIAVL"
	FlatKV          DBType = "FlatKV"
	CompositeDual   DBType = "CompositeDual"
	CompositeSplit  DBType = "CompositeSplit"
	CompositeCosmos DBType = "CompositeCosmos"

	SSComposite               DBType = "SSComposite"
	SSHistoricalOffload       DBType = "SSHistoricalOffload"
	CompositeDual_SSComposite DBType = "CompositeDual+SSComposite"
)

func DefaultBenchStateStoreConfig() *config.StateStoreConfig {
	cfg := config.DefaultStateStoreConfig()
	cfg.AsyncWriteBuffer = config.DefaultSSAsyncBuffer
	cfg.EVMSplit = true
	return &cfg
}

// DefaultBenchMemIAVLConfig returns the memiavl config the benchmarks open
// with by default. Note AsyncCommitBuffer=10: Commit() returns once the WAL
// write is enqueued, not once it is durable.
func DefaultBenchMemIAVLConfig() memiavl.Config {
	cfg := memiavl.DefaultConfig()
	cfg.AsyncCommitBuffer = 10
	cfg.SnapshotInterval = 1000
	cfg.SnapshotMinTimeInterval = 60
	return cfg
}

func newMemIAVLCommitStore(dbDir string, cfg *memiavl.Config) (DBWrapper, error) {
	if cfg == nil {
		defaultCfg := DefaultBenchMemIAVLConfig()
		cfg = &defaultCfg
	}
	fmt.Printf("Opening memIAVL from directory %s\n", dbDir)
	cs := memiavl.NewCommitStore(dbDir, *cfg)
	if err := cs.Initialize([]string{EVMStoreName}); err != nil {
		return nil, fmt.Errorf("memiavl Initialize: %w", err)
	}
	_, err := cs.LoadVersion(0, false)
	if err != nil {
		if closeErr := cs.Close(); closeErr != nil {
			fmt.Printf("failed to close commit store during error recovery: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to load version: %w", err)
	}
	return NewMemIAVLWrapper(cs), nil
}

func newFlatKVCommitStore(ctx context.Context, dbDir string, config *flatkvConfig.Config) (DBWrapper, error) {
	if config == nil {
		config = flatkvConfig.DefaultConfig()
	}
	config.DataDir = dbDir

	fmt.Printf("Opening flatKV from directory %s\n", dbDir)
	stateWAL, err := flatkv.OpenStateWAL(config)
	if err != nil {
		return nil, fmt.Errorf("failed to open FlatKV state WAL: %w", err)
	}
	cs, err := flatkv.NewCommitStore(ctx, config, stateWAL)
	if err != nil {
		_ = stateWAL.Close()
		return nil, fmt.Errorf("failed to create FlatKV commit store: %w", err)
	}
	if err := cs.LoadLatest(); err != nil {
		if closeErr := cs.Close(); closeErr != nil {
			fmt.Printf("failed to close commit store during error recovery: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to load version: %w", err)
	}
	return NewFlatKVWrapper(cs), nil
}

func newCompositeCommitStore(ctx context.Context, dbDir string, writeMode sctypes.WriteMode) (DBWrapper, error) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = writeMode
	cfg.MemIAVLConfig.AsyncCommitBuffer = 10
	cfg.MemIAVLConfig.SnapshotInterval = 100

	cs, err := composite.NewCompositeCommitStore(ctx, dbDir, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create composite commit store: %w", err)
	}
	if err := cs.CleanupCrashArtifacts(); err != nil {
		return nil, fmt.Errorf("failed to cleanup crash artifacts: %w", err)
	}
	if err := cs.Initialize([]string{EVMStoreName}); err != nil {
		return nil, fmt.Errorf("composite Initialize: %w", err)
	}

	if err := cs.LoadLatest(); err != nil {
		if closeErr := cs.Close(); closeErr != nil {
			fmt.Printf("failed to close commit store during error recovery: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to load version: %w", err)
	}

	return NewCompositeWrapper(cs), nil
}

func openSSComposite(dir string, cfg config.StateStoreConfig) (*ssComposite.CompositeStateStore, error) {
	return ssComposite.NewCompositeStateStore(cfg, dir)
}

func newSSCompositeStateStore(dbDir string, ssConfig *config.StateStoreConfig) (DBWrapper, error) {
	if ssConfig == nil {
		ssConfig = DefaultBenchStateStoreConfig()
	}
	fmt.Printf("Opening composite state store from directory %s\n", dbDir)
	store, err := openSSComposite(dbDir, *ssConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to open composite state store: %w", err)
	}
	return NewStateStoreWrapper(store), nil
}

func newCombinedCompositeDualSSComposite(
	ctx context.Context,
	dbDir string,
	ssConfig *config.StateStoreConfig,
) (DBWrapper, error) {
	if ssConfig == nil {
		ssConfig = DefaultBenchStateStoreConfig()
	}

	fmt.Printf("Opening CompositeDual (SC) + Composite (SS) from directory %s\n", dbDir)
	sc, err := newCompositeCommitStore(ctx, filepath.Join(dbDir, "sc"), sctypes.TestOnlyDualWrite)
	if err != nil {
		return nil, fmt.Errorf("failed to create SC store: %w", err)
	}
	ss, err := openSSComposite(filepath.Join(dbDir, "ss"), *ssConfig)
	if err != nil {
		_ = sc.Close()
		return nil, fmt.Errorf("failed to create SS store: %w", err)
	}
	return NewCombinedWrapper(sc, ss), nil
}

// backendConfig converts the untyped config NewDBImpl is handed into the one its backend expects.
//
// A nil config is ordinary rather than exceptional: runBenchmark passes nil for every backend, and
// each constructor supplies its own default. So nil is passed straight through, and only a config of
// the wrong type is an error. Asserting without this — dbConfig.(*T) on a nil interface — panics
// before the constructor can apply that default, which is how three bench backends came to crash at
// startup.
func backendConfig[T any](dbType DBType, dbConfig any) (*T, error) {
	if dbConfig == nil {
		return nil, nil
	}
	typed, ok := dbConfig.(*T)
	if !ok {
		var want T
		return nil, fmt.Errorf("invalid %s config type %T, want *%T", dbType, dbConfig, want)
	}
	return typed, nil
}

// NewDBImpl instantiates a new empty DBWrapper based on the given DBType.
func NewDBImpl(ctx context.Context, dbType DBType, dataDir string, dbConfig any) (DBWrapper, error) {
	switch dbType {
	case NoOp:
		return NewNoOpWrapper(), nil
	case MemIAVL:
		cfg, err := backendConfig[memiavl.Config](dbType, dbConfig)
		if err != nil {
			return nil, err
		}
		return newMemIAVLCommitStore(dataDir, cfg)
	case FlatKV:
		cfg, err := backendConfig[flatkvConfig.Config](dbType, dbConfig)
		if err != nil {
			return nil, err
		}
		return newFlatKVCommitStore(ctx, dataDir, cfg)
	case CompositeDual:
		return newCompositeCommitStore(ctx, dataDir, sctypes.TestOnlyDualWrite)
	case CompositeSplit:
		return newCompositeCommitStore(ctx, dataDir, sctypes.EVMMigrated)
	case CompositeCosmos:
		return newCompositeCommitStore(ctx, dataDir, sctypes.MemiavlOnly)
	case SSComposite:
		cfg, err := backendConfig[config.StateStoreConfig](dbType, dbConfig)
		if err != nil {
			return nil, err
		}
		return newSSCompositeStateStore(dataDir, cfg)
	case SSHistoricalOffload:
		// No default: the stream needs brokers only the caller knows, so a missing config is
		// reported by HistoricalOffloadConfig.Validate rather than invented here.
		cfg, err := backendConfig[HistoricalOffloadConfig](dbType, dbConfig)
		if err != nil {
			return nil, err
		}
		return newSSHistoricalOffloadStateStore(ctx, dataDir, cfg)
	case CompositeDual_SSComposite:
		cfg, err := backendConfig[config.StateStoreConfig](dbType, dbConfig)
		if err != nil {
			return nil, err
		}
		return newCombinedCompositeDualSSComposite(ctx, dataDir, cfg)
	default:
		return nil, fmt.Errorf("unsupported DB type: %s", dbType)
	}
}
