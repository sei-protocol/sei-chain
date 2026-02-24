// Package composite provides a unified commit store that coordinates
// between Cosmos (memiavl) and EVM (flatkv) committers.
package composite

import (
	"fmt"
	"math"

	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// EVMStoreName is the module name for the EVM store
const EVMStoreName = "evm"

// For backward compatibility purpose reuse current interface
var _ types.Committer = (*CompositeCommitStore)(nil)

// CompositeCommitStore manages multiple commit store backends (Cosmos/memiavl and FlatKV)
// and routes operations based on the configured migration strategy.
type CompositeCommitStore struct {
	logger logger.Logger

	// cosmosCommitter is the Cosmos (memiavl) backend - always initialized
	cosmosCommitter *memiavl.CommitStore

	// evmCommitter is the FlatKV backend - may be nil if not enabled
	evmCommitter flatkv.Store

	// homeDir is the base directory for the store
	homeDir string

	// config holds the store configuration
	config config.StateCommitConfig
}

// NewCompositeCommitStore creates a new composite commit store.
// Note: The store is NOT opened yet. Call LoadVersion to open and initialize the DBs.
// This matches the memiavl.NewCommitStore pattern.
func NewCompositeCommitStore(
	homeDir string,
	logger logger.Logger,
	cfg config.StateCommitConfig,
) *CompositeCommitStore {
	// Always initialize the Cosmos backend (creates struct only, not opened)
	cosmosCommitter := memiavl.NewCommitStore(homeDir, logger, cfg.MemIAVLConfig)

	store := &CompositeCommitStore{
		logger:          logger,
		cosmosCommitter: cosmosCommitter,
		homeDir:         homeDir,
		config:          cfg,
	}

	// Initialize FlatKV store struct if write mode requires it
	// Note: DB is NOT opened here, will be opened in LoadVersion
	if cfg.WriteMode == config.DualWrite || cfg.WriteMode == config.SplitWrite {
		store.evmCommitter = flatkv.NewCommitStore(homeDir, logger, cfg.FlatKVConfig)
	}

	return store
}

// Initialize initializes the store with the given store names
func (cs *CompositeCommitStore) Initialize(initialStores []string) {
	cs.cosmosCommitter.Initialize(initialStores)
}

// SetInitialVersion sets the initial version for the store
func (cs *CompositeCommitStore) SetInitialVersion(initialVersion int64) error {
	return cs.cosmosCommitter.SetInitialVersion(initialVersion)
}

// LoadVersion loads the specified version of the database.
// Being used for two scenarios:
// ReadOnly: Either for state sync or for historical proof
// Writable: Opened during initialization for root multistore
func (cs *CompositeCommitStore) LoadVersion(targetVersion int64, readOnly bool) (types.Committer, error) {
	cosmosSC, err := cs.cosmosCommitter.LoadVersion(targetVersion, readOnly)
	if err != nil {
		return nil, fmt.Errorf("failed to load cosmos version: %w", err)
	}

	cosmosCommitter, ok := cosmosSC.(*memiavl.CommitStore)
	if !ok {
		return nil, fmt.Errorf("unexpected committer type from cosmos LoadVersion")
	}

	// Read only mode should return a new SC
	if readOnly {
		newStore := &CompositeCommitStore{
			logger:          cs.logger,
			cosmosCommitter: cosmosCommitter,
			homeDir:         cs.homeDir,
			config:          cs.config,
		}
		// TODO: Support loading FlatKV at target version for read only
		return newStore, nil
	}

	cs.cosmosCommitter = cosmosCommitter
	// Load evmCommitter if initialized (nil when WriteMode is CosmosOnlyWrite).
	// This is the single entry point for evmCommitter.LoadVersion — CMS calls
	// CompositeCommitStore.LoadVersion(), which internally loads both backends.
	if cs.evmCommitter != nil {
		_, err := cs.evmCommitter.LoadVersion(targetVersion)
		if err != nil {
			return nil, fmt.Errorf("failed to load FlatKV version: %w", err)
		}
	}

	return cs, nil
}

// ApplyChangeSets applies changesets to the appropriate backends based on config.
func (cs *CompositeCommitStore) ApplyChangeSets(changesets []*proto.NamedChangeSet) error {
	if len(changesets) == 0 {
		return nil
	}

	// Separate EVM and cosmos changesets
	var evmChangeset []*proto.NamedChangeSet
	var cosmosChangeset []*proto.NamedChangeSet

	for _, changeset := range changesets {
		if changeset.Name == EVMStoreName {
			evmChangeset = append(evmChangeset, changeset)
		} else {
			cosmosChangeset = append(cosmosChangeset, changeset)
		}
	}

	// Handle write mode routing
	switch cs.config.WriteMode {
	case config.CosmosOnlyWrite:
		// All data goes to cosmos
		cosmosChangeset = changesets
		evmChangeset = nil
	case config.DualWrite:
		// EVM data goes to both, non-EVM only to cosmos
		cosmosChangeset = changesets
		// evmChangeset already filtered above
	case config.SplitWrite:
		// EVM goes to EVM store, non-EVM to cosmos (already filtered above)
	}

	// Cosmos changesets always goes to cosmos commit store
	if len(cosmosChangeset) > 0 {
		if err := cs.cosmosCommitter.ApplyChangeSets(cosmosChangeset); err != nil {
			return fmt.Errorf("failed to apply cosmos changesets: %w", err)
		}
	}

	if cs.evmCommitter != nil && len(evmChangeset) > 0 {
		if err := cs.evmCommitter.ApplyChangeSets(evmChangeset); err != nil {
			return fmt.Errorf("failed to apply EVM changesets: %w", err)
		}
	}

	return nil
}

// ApplyUpgrades applies store upgrades (only applicable to Cosmos backend)
func (cs *CompositeCommitStore) ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error {
	return cs.cosmosCommitter.ApplyUpgrades(upgrades)
}

// Commit commits the current state to all active backends
func (cs *CompositeCommitStore) Commit() (int64, error) {
	// Always commit to Cosmos
	cosmosVersion, err := cs.cosmosCommitter.Commit()
	if err != nil {
		return 0, fmt.Errorf("failed to commit cosmos: %w", err)
	}

	// Commit to FlatKV as well if enabled
	if cs.evmCommitter != nil {
		evmVersion, err := cs.evmCommitter.Commit()
		if err != nil {
			return 0, fmt.Errorf("failed to commit to EVM store: %w", err)
		}
		if cosmosVersion != evmVersion {
			return 0, fmt.Errorf("cosmos and EVM version mismatch after commit: cosmos=%d, evm=%d", cosmosVersion, evmVersion)
		}
	}

	return cosmosVersion, nil
}

// Version returns the current version
func (cs *CompositeCommitStore) Version() int64 {
	if cs.cosmosCommitter != nil {
		return cs.cosmosCommitter.Version()
	} else if cs.evmCommitter != nil {
		return cs.evmCommitter.Version()
	}
	return 0
}

// GetLatestVersion returns the latest version
func (cs *CompositeCommitStore) GetLatestVersion() (int64, error) {
	// TODO: switch to metadata db
	return cs.cosmosCommitter.GetLatestVersion()
}

// GetEarliestVersion returns the earliest version
func (cs *CompositeCommitStore) GetEarliestVersion() (int64, error) {
	// TODO: switch to metadata db
	return cs.cosmosCommitter.GetEarliestVersion()
}

// WorkingCommitInfo returns the working commit info
func (cs *CompositeCommitStore) WorkingCommitInfo() *proto.CommitInfo {
	// TODO: Need to combine hash for cosmos and evm
	return cs.cosmosCommitter.WorkingCommitInfo()
}

// LastCommitInfo returns the last commit info
func (cs *CompositeCommitStore) LastCommitInfo() *proto.CommitInfo {
	// TODO: Need to combine hash for cosmos and evm
	return cs.cosmosCommitter.LastCommitInfo()
}

// GetChildStoreByName returns the underlying child store by module name.
// This only applies to cosmos committer.
func (cs *CompositeCommitStore) GetChildStoreByName(name string) types.CommitKVStore {
	return cs.cosmosCommitter.GetChildStoreByName(name)
}

// Rollback rolls back to the specified version
func (cs *CompositeCommitStore) Rollback(targetVersion int64) error {
	if err := cs.cosmosCommitter.Rollback(targetVersion); err != nil {
		return fmt.Errorf("failed to rollback cosmos commit store: %w", err)
	}

	if cs.evmCommitter != nil {
		if err := cs.evmCommitter.Rollback(targetVersion); err != nil {
			return fmt.Errorf("failed to rollback evm commit store: %w", err)
		}
	}

	return nil
}

// Exporter returns an exporter for state sync
func (cs *CompositeCommitStore) Exporter(version int64) (types.Exporter, error) {
	if version < 0 || version > math.MaxUint32 {
		return nil, fmt.Errorf("version %d out of range", version)
	}
	// TODO: Add evm committer for exporter
	return cs.cosmosCommitter.Exporter(version)
}

// Importer returns an importer for state sync
func (cs *CompositeCommitStore) Importer(version int64) (types.Importer, error) {
	cosmosImporter, err := cs.cosmosCommitter.Importer(version)
	if err != nil {
		return nil, err
	}
	var evmImporter types.Importer
	if cs.evmCommitter != nil {
		evmImporter, err = cs.evmCommitter.Importer(version)
		if err != nil {
			return nil, err
		}
	}
	compositeImporter := NewImporter(cosmosImporter, evmImporter)
	return compositeImporter, nil
}

// Close closes all backends
func (cs *CompositeCommitStore) Close() error {
	var errs []error

	if cs.cosmosCommitter != nil {
		if err := cs.cosmosCommitter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close cosmos: %w", err))
		}
	}

	if cs.evmCommitter != nil {
		if err := cs.evmCommitter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close FlatKV: %w", err))
		}
	}

	return commonerrors.Join(errs...)
}
