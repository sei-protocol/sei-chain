package node

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var (
	// ErrEmptyEpochRegistryRow is returned when a checkpoint holds an empty epoch_registry row.
	ErrEmptyEpochRegistryRow = errors.New("the checkpoint has an empty epoch_registry row: the chain does not populate it")
	// ErrClosingRoad is returned for a checkpoint at the last road of an epoch.
	ErrClosingRoad = errors.New("the checkpoint height is the last road of an epoch")
)

// EpochGenesis is what a node's epoch registry opens with: the genesis doc and the
// genesis validators.
type EpochGenesis struct {
	GenDoc     *types.GenesisDoc
	Validators []atypes.PublicKey
}

// ResolveEpochGenesis returns the epoch genesis of the node that conf and genDoc configure.
func ResolveEpochGenesis(conf *config.Config, genDoc *types.GenesisDoc) (EpochGenesis, error) {
	_, validators, err := loadAutobahnCommittee(conf.AutobahnConfigFile)
	if err != nil {
		return EpochGenesis{}, err
	}
	return EpochGenesis{GenDoc: genDoc, Validators: slices.Collect(maps.Keys(validators))}, nil
}

// PrepareEpochRegistry writes under stagingDir the epoch registry snapshot of the epoch_registry
// row in the FlatKV checkpoint in checkpointDir, the state after road height. With no row the
// snapshot holds only the genesis epochs. It returns an error if the row is empty or does not
// decode, if the registry does not open from genesis with the snapshot, if the registry does not
// hold the epoch of height and the epoch after it, or if height is the last road of an epoch.
//
// The registry check binds the row to this chain only while it holds epoch 0 or 1. A row whose
// epochs all follow epoch 1 opens on any chain with the same initial height.
func PrepareEpochRegistry(genesis EpochGenesis, checkpointDir string, height int64, stagingDir string) error {
	if height < 1 {
		return fmt.Errorf("checkpoint height %d is not positive", height)
	}
	row, err := evmonlyapp.ReadCheckpointEpochRegistryRow(checkpointDir)
	if err != nil {
		return err
	}
	if raw, ok := row.Get(); ok && len(raw) == 0 {
		return ErrEmptyEpochRegistryRow
	}
	if err := epoch.RestoreSnapshot(stagingDir, row); err != nil {
		return fmt.Errorf("seed epoch snapshot: %w", err)
	}
	registry, err := p2p.OpenEpochRegistry(genesis.GenDoc, genesis.Validators, stagingDir)
	if err != nil {
		return fmt.Errorf("open the seeded epoch registry: %w", err)
	}
	return requireEpochsForRoad(registry, atypes.RoadIndex(height))
}

// requireEpochsForRoad returns an error unless registry holds the epoch of road and the epoch
// after it, and road is not the last road of its epoch.
func requireEpochsForRoad(registry *epoch.Registry, road atypes.RoadIndex) error {
	if closing, ok := registry.ClosingEpoch(road).Get(); ok {
		return fmt.Errorf("road %d ends epoch %d: %w", road, closing, ErrClosingRoad)
	}
	current, err := registry.EpochAt(road)
	if err != nil {
		return fmt.Errorf("the seeded epoch registry has no epoch for road %d: %w", road, err)
	}
	if _, err := registry.Epoch(current.EpochIndex() + 1); err != nil {
		return fmt.Errorf("the seeded epoch registry has no epoch after road %d: %w", road, err)
	}
	return nil
}

// InstallEpochRegistry replaces the epoch registry snapshot of layout with the one that
// PrepareEpochRegistry wrote under stagingDir. stagingDir must be on the filesystem of the
// persistent state dir.
func InstallEpochRegistry(layout AutobahnLayout, stagingDir string) error {
	if err := os.RemoveAll(layout.EpochRegistryDir); err != nil {
		return fmt.Errorf("remove the epoch registry snapshot: %w", err)
	}
	if err := os.Rename(epoch.SnapshotDir(stagingDir), layout.EpochRegistryDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("install the epoch registry snapshot: %w", err)
	}
	return dbutils.SyncDir(filepath.Dir(layout.EpochRegistryDir))
}
