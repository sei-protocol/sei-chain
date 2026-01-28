package composite

import (
	"fmt"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

// RecoverCompositeStateStore recovers the composite state store from WAL.
// This ensures both Cosmos_SS and EVM_SS are in sync after recovery.
//
// Recovery flow:
// 1. Get latest versions from both stores
// 2. If EVM_SS is behind Cosmos_SS, replay WAL entries to catch up EVM_SS
// 3. Standard WAL replay goes through CompositeStateStore.ApplyChangesetSync,
//    which dual-writes to both stores
func RecoverCompositeStateStore(
	logger logger.Logger,
	changelogPath string,
	compositeStore *CompositeStateStore,
) error {
	cosmosVersion := compositeStore.cosmosStore.GetLatestVersion()

	// If no EVM store, just do standard recovery
	if compositeStore.evmStore == nil {
		return recoverStandardWAL(logger, changelogPath, compositeStore, cosmosVersion)
	}

	evmVersion := compositeStore.evmStore.GetLatestVersion()
	logger.Info("Recovering CompositeStateStore",
		"cosmosVersion", cosmosVersion,
		"evmVersion", evmVersion,
		"changelogPath", changelogPath,
	)

	// If EVM_SS is behind, we need to catch it up
	if evmVersion < cosmosVersion {
		if err := syncEVMStore(logger, changelogPath, compositeStore, evmVersion, cosmosVersion); err != nil {
			// Log error but continue - EVM store is optimization layer
			// Reads will fall back to Cosmos store
			logger.Error("Failed to sync EVM store, continuing with degraded performance",
				"error", err,
				"evmVersion", evmVersion,
				"cosmosVersion", cosmosVersion,
			)
		}
	}

	// Standard recovery - any new WAL entries after cosmosVersion
	return recoverStandardWAL(logger, changelogPath, compositeStore, cosmosVersion)
}

// syncEVMStore syncs EVM_SS from WAL entries between evmVersion and cosmosVersion
func syncEVMStore(
	logger logger.Logger,
	changelogPath string,
	compositeStore *CompositeStateStore,
	evmVersion int64,
	cosmosVersion int64,
) error {
	logger.Info("Syncing EVM store from WAL",
		"fromVersion", evmVersion,
		"toVersion", cosmosVersion,
	)

	streamHandler, err := wal.NewChangelogWAL(logger, changelogPath, wal.Config{})
	if err != nil {
		return fmt.Errorf("failed to open changelog WAL: %w", err)
	}
	defer streamHandler.Close()

	firstOffset, err := streamHandler.FirstOffset()
	if err != nil || firstOffset <= 0 {
		return fmt.Errorf("failed to get first WAL offset: %w", err)
	}

	lastOffset, err := streamHandler.LastOffset()
	if err != nil || lastOffset <= 0 {
		return fmt.Errorf("failed to get last WAL offset: %w", err)
	}

	// Find the offset to start replay from (first entry with version > evmVersion)
	startOffset, err := findReplayStartOffset(streamHandler, firstOffset, lastOffset, evmVersion)
	if err != nil {
		return fmt.Errorf("failed to find replay start offset: %w", err)
	}

	if startOffset > lastOffset {
		logger.Info("No WAL entries to replay for EVM sync")
		return nil
	}

	// Replay WAL entries to EVM store only
	logger.Info("Replaying WAL to EVM store",
		"startOffset", startOffset,
		"endOffset", lastOffset,
	)

	return streamHandler.Replay(startOffset, lastOffset, func(index uint64, entry proto.ChangelogEntry) error {
		// Only apply if version <= cosmosVersion (we're catching up)
		if entry.Version > cosmosVersion {
			return nil
		}

		// Extract and apply EVM changes only
		evmChanges := extractEVMChangesFromChangesets(entry.Changesets)
		if len(evmChanges) > 0 {
			if err := compositeStore.evmStore.ApplyChangesetParallel(entry.Version, evmChanges); err != nil {
				return fmt.Errorf("failed to apply EVM changeset at version %d: %w", entry.Version, err)
			}
		}

		// Update EVM store version
		if err := compositeStore.evmStore.SetLatestVersion(entry.Version); err != nil {
			return fmt.Errorf("failed to set EVM version %d: %w", entry.Version, err)
		}

		return nil
	})
}

// findReplayStartOffset finds the WAL offset where version > targetVersion
func findReplayStartOffset(
	streamHandler wal.ChangelogWAL,
	firstOffset uint64,
	lastOffset uint64,
	targetVersion int64,
) (uint64, error) {
	// Binary search could be used here for large WALs, but linear scan is simpler
	// and WALs are typically not that large after pruning
	for offset := firstOffset; offset <= lastOffset; offset++ {
		entry, err := streamHandler.ReadAt(offset)
		if err != nil {
			return 0, err
		}
		if entry.Version > targetVersion {
			return offset, nil
		}
	}
	return lastOffset + 1, nil // No entries to replay
}

// recoverStandardWAL performs standard WAL recovery through the composite store
func recoverStandardWAL(
	logger logger.Logger,
	changelogPath string,
	stateStore types.StateStore,
	latestVersion int64,
) error {
	streamHandler, err := wal.NewChangelogWAL(logger, changelogPath, wal.Config{})
	if err != nil {
		return fmt.Errorf("failed to open changelog WAL: %w", err)
	}
	defer streamHandler.Close()

	firstOffset, errFirst := streamHandler.FirstOffset()
	if firstOffset <= 0 || errFirst != nil {
		// No WAL entries
		return nil
	}

	lastOffset, errLast := streamHandler.LastOffset()
	if lastOffset <= 0 || errLast != nil {
		return nil
	}

	lastEntry, err := streamHandler.ReadAt(lastOffset)
	if err != nil {
		return fmt.Errorf("failed to read last WAL entry: %w", err)
	}

	// Nothing to replay if WAL is at or behind current version
	if lastEntry.Version <= latestVersion {
		return nil
	}

	// Find start offset
	startOffset, err := findReplayStartOffset(streamHandler, firstOffset, lastOffset, latestVersion)
	if err != nil {
		return fmt.Errorf("failed to find replay start: %w", err)
	}

	if startOffset > lastOffset {
		return nil
	}

	logger.Info("Replaying WAL to CompositeStateStore",
		"startOffset", startOffset,
		"endOffset", lastOffset,
		"latestVersion", latestVersion,
	)

	// Replay through CompositeStateStore - this dual-writes to both stores
	return streamHandler.Replay(startOffset, lastOffset, func(index uint64, entry proto.ChangelogEntry) error {
		if err := stateStore.ApplyChangesetSync(entry.Version, entry.Changesets); err != nil {
			return fmt.Errorf("failed to apply changeset at version %d: %w", entry.Version, err)
		}
		if err := stateStore.SetLatestVersion(entry.Version); err != nil {
			return fmt.Errorf("failed to set version %d: %w", entry.Version, err)
		}
		return nil
	})
}

// extractEVMChangesFromChangesets extracts EVM-routable changes from changesets
func extractEVMChangesFromChangesets(changesets []*proto.NamedChangeSet) map[evm.EVMStoreType][]*iavl.KVPair {
	evmChanges := make(map[evm.EVMStoreType][]*iavl.KVPair, evm.NumEVMStoreTypes)

	for _, changeset := range changesets {
		if changeset.Name != evm.EVMStoreKey {
			continue
		}
		for _, kvPair := range changeset.Changeset.Pairs {
			evmStoreType, strippedKey := commonevm.ParseEVMKey(kvPair.Key)
			if evmStoreType != evm.StoreUnknown {
				evmChanges[evmStoreType] = append(evmChanges[evmStoreType], &iavl.KVPair{
					Key:    strippedKey,
					Value:  kvPair.Value,
					Delete: kvPair.Delete,
				})
			}
		}
	}

	return evmChanges
}

// NewCompositeStateStoreWithRecovery creates a CompositeStateStore and performs WAL recovery.
// This is the recommended way to create a CompositeStateStore in production.
func NewCompositeStateStoreWithRecovery(
	logger logger.Logger,
	homeDir string,
	cosmosStore types.StateStore,
	evmConfig *config.EVMStateStoreConfig,
	ssConfig config.StateStoreConfig,
) (*CompositeStateStore, error) {
	// Create the composite store
	compositeStore, err := NewCompositeStateStore(cosmosStore, evmConfig, homeDir, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create composite state store: %w", err)
	}

	// Determine changelog path
	changelogPath := utils.GetChangelogPath(utils.GetStateStorePath(homeDir, ssConfig.Backend))
	if ssConfig.DBDirectory != "" {
		changelogPath = utils.GetChangelogPath(ssConfig.DBDirectory)
	}

	// Perform recovery
	if err := RecoverCompositeStateStore(logger, changelogPath, compositeStore); err != nil {
		compositeStore.Close()
		return nil, fmt.Errorf("failed to recover composite state store: %w", err)
	}

	return compositeStore, nil
}
