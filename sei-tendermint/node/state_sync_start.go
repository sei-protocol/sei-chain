package node

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	ablockstore "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evmonlyapp"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var (
	// errRestoreNeedsStateSync is returned for a home with a pending restore and giga state sync
	// disabled.
	errRestoreNeedsStateSync = errors.New("this home has a pending giga state sync restore, which needs giga state sync enabled")
	// errRestoreNeedsFullnode is returned for a home with a pending restore that starts as a
	// validator.
	errRestoreNeedsFullnode = errors.New("this home has a pending giga state sync restore, which a validator cannot finish")
	// errRestoreNeedsNextEpoch is returned for a pending restore whose checkpoint holds no epoch
	// after the epoch of the restored road.
	errRestoreNeedsNextEpoch = errors.New("the epoch registry of the FlatKV checkpoint holds no epoch after the epoch of the restored road, so a BlockDB cannot start at it")
)

// stateSyncStart is the giga state sync state of a home at node start.
type stateSyncStart struct {
	// enabled is the [giga.state_sync] enabled switch.
	enabled bool
	// pendingRestore is the road that the state sync restore marker of the home names, or None
	// when the home holds no marker.
	pendingRestore utils.Option[atypes.RoadHeight]
	// seed is the verified records of the state sync proof file of the pending restore, or None
	// when the home holds no state sync proof file.
	seed utils.Option[ablockstore.Seed]
}

// readStateSyncStart returns the giga state sync state of the home that conf, the Autobahn config
// file fc, giga and genDoc configure. It must run before the Giga storage opens. It returns an
// error for a home with a pending restore that this start cannot finish: the switch is off, the
// node runs as a validator, FlatKV and the app-hash vault are not both at the road of the
// restore, or the state sync proof file does not verify against the FlatKV checkpoint.
func readStateSyncStart(
	conf *config.Config,
	fc *config.AutobahnFileConfig,
	giga gigaconfig.Config,
	genDoc *types.GenesisDoc,
) (stateSyncStart, error) {
	start := stateSyncStart{enabled: giga.StateSync.Enabled}
	psd, err := resolvePersistentStateDir(conf.RootDir, fc.PersistentStateDir)
	if err != nil {
		return stateSyncStart{}, err
	}
	marked, err := readRestoreMarker(psd)
	if err != nil {
		return stateSyncStart{}, err
	}
	road, ok := marked.Get()
	if !ok {
		return start, nil
	}
	marker := p2p.RestoreMarkerFile(psd)
	if !giga.StateSync.Enabled {
		return stateSyncStart{}, fmt.Errorf("%w; set [giga.state_sync] enabled = true, or remove %s and the restored data", errRestoreNeedsStateSync, marker)
	}
	if conf.Mode == config.ModeValidator {
		return stateSyncStart{}, fmt.Errorf("%w; start it once as a fullnode, which finishes the restore and removes %s", errRestoreNeedsFullnode, marker)
	}
	storageConfig, err := buildGigaStorageConfig(psd, conf.Mode, giga.Storage, genDoc)
	if err != nil {
		return stateSyncStart{}, fmt.Errorf("build Autobahn storage config: %w", err)
	}
	if err := requireStoresAtRoad(storageConfig, road); err != nil {
		return stateSyncStart{}, fmt.Errorf("the pending giga state sync restore of %d on this home is incomplete; run seid statesync import again: %w", road, err)
	}
	checkpoint := filepath.Join(storageConfig.FlatKVConfig.DataDir, flatkv.SnapshotDirName(int64(road))) //nolint:gosec // a marked road fits in int64.
	seed, err := readRestoreSeed(p2p.RestoreProofFile(psd), checkpoint, road)
	if err != nil {
		return stateSyncStart{}, fmt.Errorf("read the BlockDB seed of the pending giga state sync restore of %d: %w", road, err)
	}
	start.pendingRestore = marked
	start.seed = seed
	return start, nil
}

// readRestoreMarker returns the road that the state sync restore marker in the persistent state
// dir psd names, or None when psd holds no marker.
func readRestoreMarker(psd string) (utils.Option[atypes.RoadHeight], error) {
	path := p2p.RestoreMarkerFile(psd)
	raw, err := os.ReadFile(path) //nolint:gosec // path is the marker under the node's own state dir.
	if errors.Is(err, os.ErrNotExist) {
		return utils.None[atypes.RoadHeight](), nil
	}
	if err != nil {
		return utils.None[atypes.RoadHeight](), fmt.Errorf("read the state sync restore marker %s: %w", path, err)
	}
	// FlatKV versions and manifest heights are int64, so a higher road cannot be a restored height.
	road, err := strconv.ParseUint(strings.TrimSuffix(string(raw), "\n"), 10, 64)
	if err != nil || road == 0 || road > math.MaxInt64 {
		return utils.None[atypes.RoadHeight](), fmt.Errorf("the state sync restore marker %s holds %q, want one positive decimal height of at most %d", path, raw, math.MaxInt64)
	}
	return utils.Some(atypes.RoadHeight(road)), nil
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

// readRestoreSeed returns the road block and the AppQC of road from the state sync proof file at
// proofPath, or None when that file does not exist. It returns an error unless the epoch registry
// of the FlatKV checkpoint in checkpointDir holds the epoch of road and the epoch after it, and
// the proof verifies against the epoch of road.
func readRestoreSeed(proofPath, checkpointDir string, road atypes.RoadHeight) (utils.Option[ablockstore.Seed], error) {
	none := utils.None[ablockstore.Seed]()
	proofFile, err := os.ReadFile(proofPath) //nolint:gosec // proofPath is the proof file under the node's own state dir.
	// A start removes the proof file before the marker, so a start that stopped between the two
	// leaves a seeded BlockDB and no proof file.
	if errors.Is(err, os.ErrNotExist) {
		return none, nil
	}
	if err != nil {
		return none, fmt.Errorf("read the state sync proof file: %w", err)
	}
	registry, err := evmonlyapp.ReadCheckpointEpochRegistry(checkpointDir)
	if err != nil {
		return none, err
	}
	// data.NewState anchors a BlockDB that starts at road on these two epochs.
	if _, ok := registry.ForAnchor(road); !ok {
		return none, fmt.Errorf("%w; import a checkpoint of another height", errRestoreNeedsNextEpoch)
	}
	proof, _, err := statesyncproof.DecodeAndVerify(registry, proofFile, road)
	if err != nil {
		return none, err
	}
	return utils.Some(ablockstore.Seed{Block: proof.RoadBlock(), AppQC: proof.AppQC()}), nil
}

// buildFullnodeDataState returns the data state of the fullnode of cfg over blockStore. When start
// names a pending restore, it requires the restored road in blockStore, and removes the state sync
// proof file and the restore marker once the data state is built.
func buildFullnodeDataState(
	ctx context.Context,
	cfg *p2p.GigaRouterCommonConfig,
	blockStore *ablockstore.Store,
	genesisTimestamp time.Time,
	start stateSyncStart,
) (*data.State, error) {
	road, pending := start.pendingRestore.Get()
	if pending {
		if err := requireSeededBlockStore(ctx, cfg.AppHashBuilder, blockStore, road); err != nil {
			return nil, fmt.Errorf("the pending giga state sync restore of %d: %w; run seid statesync import again", road, err)
		}
	} else if start.enabled {
		warnUnseededStart(cfg, blockStore)
	}
	dataState, err := p2p.BuildDataState(cfg, blockStore, cfg.App, genesisTimestamp)
	if err != nil {
		return nil, err
	}
	// The marker stays until data.NewState accepts the seeded BlockDB, so that a failed start can
	// run again or be imported over.
	if pending {
		if err := removeRestoreFiles(cfg.PersistentStateDir); err != nil {
			return nil, err
		}
		logger.Info("Started the BlockDB at the road of the giga state sync restore", "road", road)
	}
	return dataState, nil
}

// requireSeededBlockStore returns an error unless blockStore holds the AppQC of road, and that
// AppQC signs the app hash that appHashBuilder holds for road.
func requireSeededBlockStore(
	ctx context.Context,
	appHashBuilder builder.AppHashBuilder,
	blockStore *ablockstore.Store,
	road atypes.RoadHeight,
) error {
	stored, err := blockStore.ReadAppQC(road)
	if err != nil {
		return fmt.Errorf("read AppQC %d from the BlockDB: %w", road, err)
	}
	appQC, ok := stored.Get()
	if !ok {
		return fmt.Errorf("the BlockDB holds no AppQC of road %d, and this home holds no state sync proof file to seed it from", road)
	}
	entry, err := appHashBuilder.GetAppHash(ctx, uint64(road))
	if err != nil {
		return fmt.Errorf("read the restore anchor of road %d: %w", road, err)
	}
	anchor := entry.AppHash()
	if signed := appQC.Proposal().AppHash(); !bytes.Equal(signed, anchor[:]) {
		return fmt.Errorf("the app-hash vault holds %x for road %d, but the AppQC of the BlockDB signs %x", anchor, road, signed)
	}
	return nil
}

// warnUnseededStart logs a warning when blockStore is empty while cfg.App is past genesis.
func warnUnseededStart(cfg *p2p.GigaRouterCommonConfig, blockStore *ablockstore.Store) {
	if blockStore.Status().IsPresent() {
		return
	}
	appHeight := cfg.App.LastBlockHeight()
	if appHeight < cfg.GenDoc.InitialHeight {
		return
	}
	// Without a seed the node fetches blocks from genesis, and once every peer has pruned them
	// it waits with no error. This line is the only sign of that.
	logger.Warn("Giga state sync: the app is past genesis, the BlockDB is empty, and this home has no state sync restore marker; the node fetches blocks from genesis",
		"app_height", appHeight, "marker", p2p.RestoreMarkerFile(cfg.PersistentStateDir))
}

// removeRestoreFiles deletes the state sync proof file and then the restore marker of the
// persistent state dir psd, and syncs psd.
func removeRestoreFiles(psd string) error {
	for _, path := range []string{p2p.RestoreProofFile(psd), p2p.RestoreMarkerFile(psd)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return dbutils.SyncDir(psd)
}
