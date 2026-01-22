package composite

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

var _ types.StateStore = (*CompositeStateStore)(nil)

// CompositeStateStore routes between Cosmos_SS (MVCC) and EVM_SS (Default Comparer)
// - Reads: EVM_SS first for EVM keys, fallback to Cosmos_SS
// - Writes: Both stores for EVM keys, only Cosmos_SS for rest
type CompositeStateStore struct {
	cosmosStore types.StateStore   // Main MVCC PebbleDB for all modules
	evmStore    *evm.EVMStateStore // Separate EVM DBs with default comparer
	router      *evm.KeyRouter
	logger      logger.Logger
	mu          sync.RWMutex
}

// CompositeConfig holds configuration for composite state store
type CompositeConfig struct {
	CosmosConfig config.StateStoreConfig
	EVMConfig    config.EVMStateStoreConfig
}

// NewCompositeStateStore creates a new composite state store
func NewCompositeStateStore(logger logger.Logger, homeDir string, cfg CompositeConfig) (*CompositeStateStore, error) {
	// Initialize Cosmos_SS (existing MVCC PebbleDB)
	cosmosStore, err := ss.NewStateStore(logger, homeDir, cfg.CosmosConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create cosmos state store: %w", err)
	}

	var evmStore *evm.EVMStateStore
	if cfg.EVMConfig.Enable {
		evmDir := cfg.EVMConfig.DBDirectory
		if evmDir == "" {
			evmDir = filepath.Join(homeDir, "data", "evm_ss")
		}

		evmStore, err = evm.NewEVMStateStore(evmDir)
		if err != nil {
			cosmosStore.Close()
			return nil, fmt.Errorf("failed to create EVM state store: %w", err)
		}
	}

	return &CompositeStateStore{
		cosmosStore: cosmosStore,
		evmStore:    evmStore,
		router:      evm.NewKeyRouter(),
		logger:      logger,
	}, nil
}

func (s *CompositeStateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var lastErr error
	if s.evmStore != nil {
		if err := s.evmStore.Close(); err != nil {
			lastErr = err
		}
	}
	if err := s.cosmosStore.Close(); err != nil {
		lastErr = err
	}
	return lastErr
}

// Get reads from EVM_SS first for EVM keys, then fallback to Cosmos_SS
func (s *CompositeStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Try EVM_SS first for EVM-related keys
	if s.evmStore != nil {
		if evmStoreType, strippedKey, isEVM := s.router.RouteKey(storeKey, key); isEVM {
			val, err := s.evmStore.Get(evmStoreType, strippedKey, version)
			if err != nil {
				return nil, err
			}
			if val != nil {
				return val, nil
			}
			// Fallback to Cosmos_SS
		}
	}

	// Read from Cosmos_SS
	return s.cosmosStore.Get(storeKey, version, key)
}

// Has checks existence in EVM_SS first, then Cosmos_SS
func (s *CompositeStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.evmStore != nil {
		if evmStoreType, strippedKey, isEVM := s.router.RouteKey(storeKey, key); isEVM {
			db := s.evmStore.GetDB(evmStoreType)
			if db != nil {
				has, err := db.Has(strippedKey, version)
				if err != nil {
					return false, err
				}
				if has {
					return true, nil
				}
			}
		}
	}

	return s.cosmosStore.Has(storeKey, version, key)
}

// Iterator creates an iterator (uses Cosmos_SS for now, can be extended)
func (s *CompositeStateStore) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	// For simplicity, use Cosmos_SS iterator
	// A full implementation would merge iterators from both stores
	return s.cosmosStore.Iterator(storeKey, version, start, end)
}

// ReverseIterator creates a reverse iterator
func (s *CompositeStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return s.cosmosStore.ReverseIterator(storeKey, version, start, end)
}

// RawIterate iterates over raw data
func (s *CompositeStateStore) RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error) {
	return s.cosmosStore.RawIterate(storeKey, fn)
}

// GetLatestVersion returns the latest version
func (s *CompositeStateStore) GetLatestVersion() int64 {
	return s.cosmosStore.GetLatestVersion()
}

// SetLatestVersion sets latest version on both stores
func (s *CompositeStateStore) SetLatestVersion(version int64) error {
	if err := s.cosmosStore.SetLatestVersion(version); err != nil {
		return err
	}
	if s.evmStore != nil {
		return s.evmStore.SetLatestVersion(version)
	}
	return nil
}

// GetEarliestVersion returns earliest version
func (s *CompositeStateStore) GetEarliestVersion() int64 {
	return s.cosmosStore.GetEarliestVersion()
}

// SetEarliestVersion sets earliest version
func (s *CompositeStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	return s.cosmosStore.SetEarliestVersion(version, ignoreVersion)
}

// GetLatestMigratedKey returns latest migrated key
func (s *CompositeStateStore) GetLatestMigratedKey() ([]byte, error) {
	return s.cosmosStore.GetLatestMigratedKey()
}

// GetLatestMigratedModule returns latest migrated module
func (s *CompositeStateStore) GetLatestMigratedModule() (string, error) {
	return s.cosmosStore.GetLatestMigratedModule()
}

// ApplyChangesetSync applies changeset synchronously
// Writes to both EVM_SS and Cosmos_SS for EVM data, only Cosmos_SS for rest
func (s *CompositeStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Separate EVM changes from regular changes
	evmChanges := make(map[evm.EVMStoreType][]*iavl.KVPair)

	for _, changeset := range changesets {
		if changeset.Name == evm.EVMStoreKey && s.evmStore != nil {
			for _, kvPair := range changeset.Changeset.Pairs {
				if evmStoreType, strippedKey, isEVM := s.router.RouteKey(changeset.Name, kvPair.Key); isEVM {
					evmChanges[evmStoreType] = append(evmChanges[evmStoreType], &iavl.KVPair{
						Key:   strippedKey,
						Value: kvPair.Value,
					})
				}
			}
		}
	}

	// Apply to EVM_SS
	if s.evmStore != nil && len(evmChanges) > 0 {
		if err := s.evmStore.ApplyChangeset(version, evmChanges); err != nil {
			return fmt.Errorf("failed to apply EVM changeset: %w", err)
		}
	}

	// Always apply full changeset to Cosmos_SS
	return s.cosmosStore.ApplyChangesetSync(version, changesets)
}

// ApplyChangesetAsync applies changeset asynchronously
func (s *CompositeStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Extract and apply EVM changes
	evmChanges := make(map[evm.EVMStoreType][]*iavl.KVPair)

	for _, changeset := range changesets {
		if changeset.Name == evm.EVMStoreKey && s.evmStore != nil {
			for _, kvPair := range changeset.Changeset.Pairs {
				if evmStoreType, strippedKey, isEVM := s.router.RouteKey(changeset.Name, kvPair.Key); isEVM {
					evmChanges[evmStoreType] = append(evmChanges[evmStoreType], &iavl.KVPair{
						Key:   strippedKey,
						Value: kvPair.Value,
					})
				}
			}
		}
	}

	// Apply EVM changes synchronously (EVM stores don't have async yet)
	if s.evmStore != nil && len(evmChanges) > 0 {
		if err := s.evmStore.ApplyChangeset(version, evmChanges); err != nil {
			return fmt.Errorf("failed to apply EVM changeset: %w", err)
		}
	}

	// Apply to Cosmos_SS async
	return s.cosmosStore.ApplyChangesetAsync(version, changesets)
}

// Import imports data into the store
func (s *CompositeStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	// For import, we need to route to appropriate stores
	// Create channels for each store
	cosmosCh := make(chan types.SnapshotNode, 1000)
	evmStore := s.evmStore
	router := s.router

	go func() {
		defer close(cosmosCh)
		for node := range ch {
			// Route to appropriate store
			if evmStore != nil {
				if evmStoreType, strippedKey, isEVM := router.RouteKey(node.StoreKey, node.Key); isEVM {
					// Write to EVM store (ignore errors in background)
					_ = evmStore.Set(evmStoreType, strippedKey, node.Value, version)
				}
			}
			// Always send to Cosmos channel
			cosmosCh <- node
		}
	}()

	return s.cosmosStore.Import(version, cosmosCh)
}

// RawImport imports raw data
func (s *CompositeStateStore) RawImport(ch <-chan types.RawSnapshotNode) error {
	cosmosCh := make(chan types.RawSnapshotNode, 1000)
	evmStore := s.evmStore
	router := s.router

	go func() {
		defer close(cosmosCh)
		for node := range ch {
			if evmStore != nil {
				if evmStoreType, strippedKey, isEVM := router.RouteKey(node.StoreKey, node.Key); isEVM {
					_ = evmStore.Set(evmStoreType, strippedKey, node.Value, node.Version)
				}
			}
			cosmosCh <- node
		}
	}()

	return s.cosmosStore.RawImport(cosmosCh)
}

// Prune prunes old versions from both stores
func (s *CompositeStateStore) Prune(version int64) error {
	if s.evmStore != nil {
		if err := s.evmStore.Prune(version); err != nil {
			return fmt.Errorf("failed to prune EVM store: %w", err)
		}
	}
	return s.cosmosStore.Prune(version)
}

// GetEVMStore returns the EVM state store for direct access
func (s *CompositeStateStore) GetEVMStore() *evm.EVMStateStore {
	return s.evmStore
}

// GetCosmosStore returns the cosmos state store for direct access
func (s *CompositeStateStore) GetCosmosStore() types.StateStore {
	return s.cosmosStore
}
