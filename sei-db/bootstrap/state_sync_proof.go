package bootstrap

import (
	"errors"
	"fmt"
	"sync"
	"time"

	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// stateSyncProofSweepInterval is the time between two state sync proof export sweeps.
const stateSyncProofSweepInterval = 10 * time.Second

// StartStateSyncProofExport starts writing state sync proof files for retained FlatKV
// checkpoints. isClosingRoad reports whether a road is the last road of an epoch. Close stops it.
// It returns an error when the export already started or the manager is closed.
func (m *GigaStorageManager) StartStateSyncProofExport(isClosingRoad func(types.RoadIndex) bool) error {
	return m.stateSyncProofExport.start(func() *stateSyncProofExportLoop {
		exporter := statesyncproof.NewExporter(stateSyncProofExportConfig(m.cfg, m.blockStore, isClosingRoad))
		return startStateSyncProofExportLoop(exporter, stateSyncProofSweepInterval)
	})
}

// stateSyncProofExportConfig returns the export config of the stores cfg describes: proof files go to
// the state sync proof dir of cfg.HomePath, and checkpoints come from the FlatKV dir.
func stateSyncProofExportConfig(
	cfg *config.GigaStorageConfig,
	blockStore *blockstore.Store,
	isClosingRoad func(types.RoadIndex) bool,
) statesyncproof.ExportConfig {
	return statesyncproof.ExportConfig{
		Dir: dbutils.GetStateSyncProofPath(cfg.HomePath),
		CheckpointRoads: func() ([]types.RoadIndex, error) {
			return checkpointRoads(cfg.FlatKVConfig.DataDir)
		},
		BlockStore: blockStore,
		// The AppQC may not be flushed yet. A crash can lose it, but the proof file stays valid,
		// because the file holds the AppQC and the importer verifies it.
		LastAppQC:     func() utils.Option[types.RoadIndex] { return lastAppQC(blockStore.Status()) },
		IsClosingRoad: isClosingRoad,
	}
}

// checkpointRoads returns the road indices of the FlatKV checkpoints retained in dir.
func checkpointRoads(dir string) ([]types.RoadIndex, error) {
	versions, err := flatkv.SnapshotVersions(dir)
	if err != nil {
		return nil, err
	}
	roads := make([]types.RoadIndex, 0, len(versions))
	for _, version := range versions {
		if version < 0 {
			return nil, fmt.Errorf("checkpoint version %d is negative", version)
		}
		roads = append(roads, types.RoadIndex(version)) //nolint:gosec // checked above.
	}
	return roads, nil
}

// lastAppQC returns the road of the latest AppQC in a BlockStore with the given status, or None
// when it holds none.
func lastAppQC(status utils.Option[blockstore.SuffixRange]) utils.Option[types.RoadIndex] {
	suffix, ok := status.Get()
	if !ok || suffix.NextAppQC <= suffix.First {
		return utils.None[types.RoadIndex]()
	}
	return utils.Some(suffix.NextAppQC - 1)
}

// stateSyncProofExport holds the export loop of a GigaStorageManager. It starts at most once, and
// not after stop.
type stateSyncProofExport struct {
	mu sync.Mutex
	// loop is nil before start and after stop.
	loop    *stateSyncProofExportLoop
	stopped bool
}

// start runs newLoop and keeps its loop. It returns an error when a loop already started or stop ran.
func (e *stateSyncProofExport) start(newLoop func() *stateSyncProofExportLoop) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return errors.New("state sync proof export: storage is closed")
	}
	if e.loop != nil {
		return errors.New("state sync proof export already started")
	}
	e.loop = newLoop()
	return nil
}

// stop stops the loop, if any, and waits for an active sweep to end. A later start fails. It is
// idempotent.
func (e *stateSyncProofExport) stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopped = true
	if e.loop != nil {
		e.loop.stop()
		e.loop = nil
	}
}

// stateSyncProofExportLoop sweeps an exporter at start and then every interval until stop.
type stateSyncProofExportLoop struct {
	exporter *statesyncproof.Exporter
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// startStateSyncProofExportLoop starts a loop that sweeps exporter at once and then every interval.
func startStateSyncProofExportLoop(exporter *statesyncproof.Exporter, interval time.Duration) *stateSyncProofExportLoop {
	l := &stateSyncProofExportLoop{
		exporter: exporter,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
	l.wg.Add(1)
	go l.run()
	return l
}

// stop stops the loop and waits for it to exit, after the active sweep, if any. Must be called
// exactly once.
func (l *stateSyncProofExportLoop) stop() {
	close(l.stopCh)
	l.wg.Wait()
}

func (l *stateSyncProofExportLoop) run() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()

	lastErr := ""
	for {
		lastErr = l.sweep(lastErr)
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
		}
	}
}

// sweep runs one sweep and returns its error text, or "" on success. It logs the error only when
// the text differs from lastErr, the text of the previous sweep.
func (l *stateSyncProofExportLoop) sweep(lastErr string) string {
	err := l.exporter.Sweep()
	if err == nil {
		return ""
	}
	// A permanent error, such as a full disk, would otherwise log once per sweep.
	if err.Error() != lastErr {
		logger.Error("state sync proof export failed; retrying on next sweep", "err", err)
	}
	return err.Error()
}
