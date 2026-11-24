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

var (
	// errRestoreNeedsStateSync is returned for a home with a pending restore and giga state sync
	// disabled.
	errRestoreNeedsStateSync = errors.New("this home has a pending giga state sync restore, which needs giga state sync enabled")
	// errRestoreNeedsFullnode is returned for a home with a pending restore that starts as a
	// validator.
	errRestoreNeedsFullnode = errors.New("this home has a pending giga state sync restore, which a validator cannot finish")
)

// readStateSyncStart returns the giga state sync state of the home that conf, the Autobahn config
// file fc, giga and genDoc configure. It must run before the Giga storage opens. It returns an
// error for a home with a pending restore that this start cannot finish: the switch is off, the
// node runs as a validator, or FlatKV and the app-hash vault are not both at the road of the
// restore.
func readStateSyncStart(
	conf *config.Config,
	fc *config.AutobahnFileConfig,
	giga gigaconfig.Config,
	genDoc *types.GenesisDoc,
) (p2p.StateSyncStart, error) {
	start := p2p.StateSyncStart{Enabled: giga.StateSync.Enabled}
	psd, err := resolvePersistentStateDir(conf.RootDir, fc.PersistentStateDir)
	if err != nil {
		return p2p.StateSyncStart{}, err
	}
	marked, err := p2p.ReadRestoreMarker(psd)
	if err != nil {
		return p2p.StateSyncStart{}, err
	}
	road, ok := marked.Get()
	if !ok {
		return start, nil
	}
	marker := p2p.RestoreMarkerFile(psd)
	if !giga.StateSync.Enabled {
		return p2p.StateSyncStart{}, fmt.Errorf("%w; set [giga.state_sync] enabled = true, or remove %s and the restored data", errRestoreNeedsStateSync, marker)
	}
	if conf.Mode == config.ModeValidator {
		return p2p.StateSyncStart{}, fmt.Errorf("%w; start it once as a fullnode, which finishes the restore and removes %s", errRestoreNeedsFullnode, marker)
	}
	storageConfig, err := buildGigaStorageConfig(psd, conf.Mode, giga.Storage, genDoc)
	if err != nil {
		return p2p.StateSyncStart{}, fmt.Errorf("build Autobahn storage config: %w", err)
	}
	if err := requireStoresAtRoad(storageConfig, road); err != nil {
		return p2p.StateSyncStart{}, fmt.Errorf("the pending giga state sync restore of %d on this home is incomplete; run seid statesync import again: %w", road, err)
	}
	start.PendingRestore = marked
	return start, nil
}

// requireStoresAtRoad returns an error unless FlatKV and the app-hash vault of storageConfig
// are both at road.
func requireStoresAtRoad(storageConfig *seidbconfig.GigaStorageConfig, road atypes.RoadHeight) error {
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
