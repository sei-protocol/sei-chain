package node

import (
	"fmt"
	"maps"
	"slices"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// EpochGenesis is what a node's epoch registry opens with: the genesis doc and the genesis
// validators.
type EpochGenesis struct {
	GenDoc     *types.GenesisDoc
	Validators []atypes.PublicKey
}

// ResolveEpochGenesis returns the epoch genesis of the node that conf and genDoc configure, with
// the genesis validators in key order.
func ResolveEpochGenesis(conf *config.Config, genDoc *types.GenesisDoc) (EpochGenesis, error) {
	_, validators, err := loadAutobahnCommittee(conf.AutobahnConfigFile)
	if err != nil {
		return EpochGenesis{}, err
	}
	return EpochGenesis{GenDoc: genDoc, Validators: slices.SortedFunc(maps.Keys(validators), atypes.PublicKey.Compare)}, nil
}

// CheckpointEpochs is the epoch registry of a FlatKV checkpoint, open in memory.
type CheckpointEpochs struct {
	registry *epoch.Registry
}

// OpenCheckpointEpochs opens in memory the epoch registry that the FlatKV checkpoint in
// checkpointDir, the state after road height, persists. With no persisted registry it holds only
// the genesis epochs. It returns an error if the persisted registry does not decode, or if the
// registry does not hold the epoch of height and the epoch after it.
func OpenCheckpointEpochs(genesis EpochGenesis, checkpointDir string, height int64) (CheckpointEpochs, error) {
	if height < 1 {
		return CheckpointEpochs{}, fmt.Errorf("checkpoint height %d is not positive", height)
	}
	stored, err := readCheckpointEpochRegistry(checkpointDir)
	if err != nil {
		return CheckpointEpochs{}, err
	}
	registry, err := p2p.NewEpochRegistry(genesis.GenDoc, slices.Values(genesis.Validators), stored)
	if err != nil {
		return CheckpointEpochs{}, fmt.Errorf("open the checkpoint epoch registry: %w", err)
	}
	if err := requireEpochsForRoad(registry, atypes.RoadIndex(height)); err != nil {
		return CheckpointEpochs{}, err
	}
	return CheckpointEpochs{registry: registry}, nil
}

// readCheckpointEpochRegistry returns the persisted epoch registry of the FlatKV checkpoint in
// dir, or none when the checkpoint holds none.
func readCheckpointEpochRegistry(dir string) (utils.Option[[]byte], error) {
	raw, ok, err := flatkv.ReadSnapshotMiscValue(dir, bootstrap.EpochStateModule, []byte(bootstrap.EpochRegistryKey))
	if err != nil {
		return utils.None[[]byte](), fmt.Errorf("read the checkpoint epoch registry: %w", err)
	}
	if !ok {
		return utils.None[[]byte](), nil
	}
	return utils.Some(raw), nil
}

// requireEpochsForRoad returns an error unless registry holds the epoch of road and the epoch
// after it.
func requireEpochsForRoad(registry *epoch.Registry, road atypes.RoadIndex) error {
	current, err := registry.EpochAt(road)
	if err != nil {
		return fmt.Errorf("the checkpoint epoch registry has no epoch for road %d: %w", road, err)
	}
	if _, err := registry.Epoch(current.EpochIndex() + 1); err != nil {
		return fmt.Errorf("the checkpoint epoch registry has no epoch after road %d: %w", road, err)
	}
	return nil
}
