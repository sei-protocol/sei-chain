package node

import (
	"errors"
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// readStateSyncStart returns the giga state sync state of the home that conf, giga and genDoc
// configure. It must run before the Giga storage opens. It returns an error for a home with a
// restore marker that this start cannot seed: the switch is off, the node runs as a validator, or
// FlatKV and the app-hash vault are not both at the marked road.
func readStateSyncStart(conf *config.Config, giga gigaconfig.Config, genDoc *types.GenesisDoc) (p2p.StateSyncStart, error) {
	start := p2p.StateSyncStart{Enabled: giga.StateSync.Enabled}
	fc, _, err := loadAutobahnCommittee(conf.AutobahnConfigFile)
	if err != nil {
		return p2p.StateSyncStart{}, fmt.Errorf("load Autobahn committee: %w", err)
	}
	psd, storageConfig, err := autobahnStorageConfig(conf.RootDir, conf.Mode, fc, giga.Storage, genDoc)
	if err != nil {
		return p2p.StateSyncStart{}, err
	}
	restored, err := p2p.ReadRestoreMarker(psd)
	if err != nil {
		return p2p.StateSyncStart{}, err
	}
	road, ok := restored.Get()
	if !ok {
		return start, nil
	}
	marker := p2p.RestoreMarkerFile(psd)
	if !giga.StateSync.Enabled {
		return p2p.StateSyncStart{}, fmt.Errorf("this home was restored by giga state sync; set [giga.state_sync] enabled = true, or remove %s and the restored data", marker)
	}
	if conf.Mode == config.ModeValidator {
		return p2p.StateSyncStart{}, fmt.Errorf("this home was restored by giga state sync and must start once as a fullnode, which removes %s", marker)
	}
	if err := requireRestoreInstalled(storageConfig, road); err != nil {
		return p2p.StateSyncStart{}, fmt.Errorf("the restore of %d on this home is incomplete, run restore again: %w", road, err)
	}
	start.Restored = restored
	return start, nil
}

// requireRestoreInstalled returns an error unless FlatKV and the app-hash vault of storageConfig
// are both at road.
func requireRestoreInstalled(storageConfig *seidbconfig.GigaStorageConfig, road atypes.RoadIndex) error {
	flatKVVersion, err := flatkv.ActiveSnapshotVersion(storageConfig.FlatKVConfig.DataDir)
	if errors.Is(err, os.ErrNotExist) {
		flatKVVersion = 0
	} else if err != nil {
		return fmt.Errorf("read the FlatKV version: %w", err)
	}
	if flatKVVersion != int64(road) { //nolint:gosec // a marked road fits in int64.
		return fmt.Errorf("FlatKV is at %d", flatKVVersion)
	}
	vaultHead, err := builder.GetLatestBlock(*storageConfig.AppHashBuilderConfig)
	if err != nil {
		return fmt.Errorf("read the app-hash vault head: %w", err)
	}
	if vaultHead != uint64(road) {
		return fmt.Errorf("the app-hash vault ends at %d", vaultHead)
	}
	return nil
}
