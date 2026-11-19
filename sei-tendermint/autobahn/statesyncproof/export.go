package statesyncproof

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/sei-protocol/seilog"

	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var logger = seilog.NewLogger("tendermint", "autobahn", "statesyncproof")

// ExportConfig configures export of state sync proof files for retained checkpoints.
type ExportConfig struct {
	// Dir is the directory where canonical state sync proof files are written.
	Dir string
	// CheckpointRoads returns the road heights of the FlatKV checkpoints retained locally.
	CheckpointRoads func() ([]types.RoadHeight, error)
	// BlockStore provides the road blocks, AppProposals and AppQCs to export.
	BlockStore BlockStore
	// LastAppQC returns the road of the latest AppQC in BlockStore, or None when it holds none.
	// Proofs are written only at or below it.
	LastAppQC func() utils.Option[types.RoadHeight]
	// IsClosingRoad reports whether road is the last road of an epoch.
	IsClosingRoad func(road types.RoadHeight) bool
}

// BlockStore is the part of the Autobahn BlockStore that Exporter reads.
type BlockStore interface {
	ReadBlock(types.RoadHeight) (utils.Option[*types.RoadBlock], error)
	ReadAppProposal(types.RoadHeight) (utils.Option[*types.AppProposal], error)
	ReadAppQC(types.RoadHeight) (utils.Option[*types.AppQC], error)
}

// Exporter writes the state sync proof files of retained checkpoints. It is not safe for
// concurrent use.
type Exporter struct {
	cfg ExportConfig
	// checkpoints is the retained checkpoint set that the last proof dir scan ran against, or None
	// before the first scan.
	checkpoints utils.Option[map[types.RoadHeight]struct{}]
	// exported holds the retained checkpoints whose proof file is on disk and decodes.
	exported map[types.RoadHeight]struct{}
	// skipped holds the retained checkpoints that get no proof file: LastRoad(X), or records that are
	// pruned, missing or inconsistent.
	skipped map[types.RoadHeight]struct{}
}

// NewExporter returns an Exporter that has not scanned the proof dir yet.
func NewExporter(cfg ExportConfig) *Exporter {
	return &Exporter{
		cfg:      cfg,
		exported: map[types.RoadHeight]struct{}{},
		skipped:  map[types.RoadHeight]struct{}{},
	}
}

// Sweep exports every pending checkpoint at or below LastAppQC. An I/O error on one
// checkpoint or proof file does not stop the others; Sweep returns all of them joined, and a
// later Sweep retries them.
func (e *Exporter) Sweep() error {
	err := e.sweep()
	if err != nil {
		Global.exportFailuresAt().Add(1)
	}
	return err
}

func (e *Exporter) sweep() error {
	checkpoints, err := e.retainedCheckpoints()
	if err != nil {
		return err
	}
	var errs []error
	if err := e.syncCheckpoints(checkpoints); err != nil {
		errs = append(errs, err)
	}
	for _, height := range e.exportable(checkpoints) {
		if err := e.exportCheckpoint(height); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// syncCheckpoints rescans the proof dir on the first sweep, whenever the retained checkpoint set
// changes, and after a scan that failed. The first scan keeps only the proof files that decode.
// Every scan forgets the checkpoints that are no longer retained and the exported checkpoints whose
// file is gone, so the next export writes them again. A file that cannot be read or removed does
// not stop the scan: it is not counted as exported, and its error is returned with the others.
func (e *Exporter) syncCheckpoints(checkpoints map[types.RoadHeight]struct{}) error {
	last, scanned := e.checkpoints.Get()
	if scanned && maps.Equal(last, checkpoints) {
		return nil
	}
	entries, err := os.ReadDir(e.cfg.Dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		// Without a listing the scan learns nothing, so the next sweep scans again.
		return fmt.Errorf("read state sync proof dir %s: %w", e.cfg.Dir, err)
	}
	present, orphanErr := e.removeOrphans(entries, checkpoints)
	var invalidErr error
	if scanned {
		maps.DeleteFunc(e.exported, func(height types.RoadHeight, _ struct{}) bool {
			_, ok := present[height]
			return !ok
		})
	} else {
		e.exported, invalidErr = e.removeInvalidFiles(present)
	}
	maps.DeleteFunc(e.skipped, func(height types.RoadHeight, _ struct{}) bool {
		_, retained := checkpoints[height]
		return !retained
	})
	if err := errors.Join(orphanErr, invalidErr); err != nil {
		// Leaving the set unrecorded makes the next sweep scan again and retry the failed files.
		return err
	}
	e.checkpoints = utils.Some(checkpoints)
	return nil
}

// removeInvalidFiles removes the proof files in present that do not decode for the road in their
// name, and returns the rest. A file it cannot read or remove is left out of the result, and its
// error is returned joined with the others in ascending road order.
func (e *Exporter) removeInvalidFiles(present map[types.RoadHeight]struct{}) (map[types.RoadHeight]struct{}, error) {
	valid := make(map[types.RoadHeight]struct{}, len(present))
	var errs []error
	// A stable error text lets the caller suppress repeated logs of the same failure.
	for _, height := range slices.Sorted(maps.Keys(present)) {
		path := filepath.Join(e.cfg.Dir, FileName(height))
		data, err := os.ReadFile(path) //nolint:gosec // path is derived from the configured state sync proof directory and a canonical file name.
		if err != nil {
			errs = append(errs, fmt.Errorf("read state sync proof file %s: %w", path, err))
			continue
		}
		if _, err := Decode(data, height); err != nil {
			logger.Warn("removing invalid state sync proof file", "path", path, "err", err)
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove invalid state sync proof file %s: %w", path, err))
			}
			continue
		}
		valid[height] = struct{}{}
	}
	return valid, errors.Join(errs...)
}

// exportable returns the checkpoints that are neither exported nor skipped and are at or below
// LastAppQC, in ascending order.
func (e *Exporter) exportable(checkpoints map[types.RoadHeight]struct{}) []types.RoadHeight {
	lastAppQC, ok := e.cfg.LastAppQC().Get()
	if !ok {
		return nil
	}
	out := make([]types.RoadHeight, 0, len(checkpoints))
	for _, height := range slices.Sorted(maps.Keys(checkpoints)) {
		if _, ok := e.exported[height]; ok {
			continue
		}
		if _, ok := e.skipped[height]; ok {
			continue
		}
		if height > lastAppQC {
			continue
		}
		out = append(out, height)
	}
	return out
}

// exportCheckpoint writes the proof file of checkpoint index, or skips it when it is LastRoad(X).
func (e *Exporter) exportCheckpoint(height types.RoadHeight) error {
	// The committee of epoch X+2 is computed when a node executes LastRoad(X). A node restored at
	// H never executes H, so a checkpoint at LastRoad(X) cannot be restored.
	// TODO: remove this skip when the restore from a state sync snapshot sets up the committee of
	// epoch X+2 itself, so that a node can restore from any road.
	if e.cfg.IsClosingRoad(height) {
		e.skipped[height] = struct{}{}
		return nil
	}
	return e.exportProof(height)
}

func (e *Exporter) retainedCheckpoints() (map[types.RoadHeight]struct{}, error) {
	roads, err := e.cfg.CheckpointRoads()
	if err != nil {
		return nil, fmt.Errorf("list checkpoint roads: %w", err)
	}
	checkpoints := make(map[types.RoadHeight]struct{}, len(roads))
	for _, road := range roads {
		checkpoints[road] = struct{}{}
	}
	return checkpoints, nil
}

// exportProof writes the proof file of road height and marks it exported. Records that are
// pruned, missing or inconsistent mark it skipped.
func (e *Exporter) exportProof(height types.RoadHeight) error {
	records, err := e.readRecords(height)
	if errors.Is(err, types.ErrPruned) {
		e.skipped[height] = struct{}{}
		return nil
	}
	if err != nil {
		return err
	}
	r, ok := records.Get()
	if !ok {
		// BlockStore writes the block, AppProposal and AppQC of a road before its AppQC cursor covers
		// the road, so a record missing at or below LastAppQC never appears later. A checkpoint from
		// before the BlockStore held any road is one such case.
		logger.Info("skipping state sync proof: BlockStore has no records for checkpoint", "height", height)
		e.skipped[height] = struct{}{}
		return nil
	}
	encoded, err := r.encode(height)
	if err != nil {
		// Skipping, not halting, keeps a corrupt local BlockStore from stopping the node through an
		// optional export, and never publishes the bad proof.
		Global.inconsistentRecordsAt().Add(1)
		logger.Error("skipping state sync proof: inconsistent BlockStore records", "height", height, "err", err)
		e.skipped[height] = struct{}{}
		return nil
	}
	if err := writeFile(filepath.Join(e.cfg.Dir, FileName(height)), encoded); err != nil {
		return err
	}
	e.exported[height] = struct{}{}
	return nil
}

// records are the BlockStore records of one road that a proof file is built from.
type records struct {
	block       *types.RoadBlock
	appProposal *types.AppProposal
	appQC       *types.AppQC
}

// encode returns the proof file of road height. It fails when the records do not all belong to road
// height or the local AppHash does not match the AppQC.
func (r records) encode(height types.RoadHeight) ([]byte, error) {
	// The data layer checks this before it writes the AppQC. Keep the check here as defense in depth against
	// a future writer path or corrupted local records.
	if !bytes.Equal(r.appProposal.AppHash(), r.appQC.Proposal().AppHash()) {
		return nil, fmt.Errorf("AppProposal(%d) app hash does not match AppQC", height)
	}
	if r.appQC.Proposal().Height() != height {
		return nil, fmt.Errorf("AppQC index %d does not match checkpoint index %d", r.appQC.Proposal().Height(), height)
	}
	return Encode(r.block, r.appQC)
}

// readRecords reads the records of road height. It returns None when any record is missing, and an
// error wrapping types.ErrPruned when any record is pruned.
func (e *Exporter) readRecords(height types.RoadHeight) (utils.Option[records], error) {
	none := utils.None[records]()
	roadBlock, err := e.cfg.BlockStore.ReadBlock(height)
	if err != nil {
		return none, fmt.Errorf("read road block %d: %w", height, err)
	}
	appProposal, err := e.cfg.BlockStore.ReadAppProposal(height)
	if err != nil {
		return none, fmt.Errorf("read AppProposal %d: %w", height, err)
	}
	appQC, err := e.cfg.BlockStore.ReadAppQC(height)
	if err != nil {
		return none, fmt.Errorf("read AppQC %d: %w", height, err)
	}
	var r records
	var ok bool
	if r.block, ok = roadBlock.Get(); !ok {
		return none, nil
	}
	if r.appProposal, ok = appProposal.Get(); !ok {
		return none, nil
	}
	if r.appQC, ok = appQC.Get(); !ok {
		return none, nil
	}
	return utils.Some(r), nil
}

// removeOrphans removes, among the proof dir entries, the stale temp files and the proof files of
// checkpoints that are not retained. It returns the retained checkpoints that have a proof file. A
// file it cannot remove stays in place, and its error is returned joined with the others.
func (e *Exporter) removeOrphans(
	entries []os.DirEntry,
	checkpoints map[types.RoadHeight]struct{},
) (map[types.RoadHeight]struct{}, error) {
	present := map[types.RoadHeight]struct{}{}
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(e.cfg.Dir, name)
		if filepath.Ext(name) == ".tmp" {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove stale temp state sync proof %s: %w", path, err))
			}
			continue
		}
		height, err := ParseFileName(name)
		if err != nil {
			continue
		}
		if _, ok := checkpoints[height]; ok {
			present[height] = struct{}{}
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove orphan state sync proof %s: %w", path, err))
		}
	}
	return present, errors.Join(errs...)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create state sync proof dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale temp file %s: %w", tmp, err)
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // tmp is derived from the configured state sync proof directory and a canonical file name.
	if err != nil {
		return fmt.Errorf("create temp state sync proof file %s: %w", tmp, err)
	}
	closeFile := true
	defer func() {
		if closeFile {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write temp state sync proof file %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync temp state sync proof file %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp state sync proof file %s: %w", tmp, err)
	}
	closeFile = false
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename temp state sync proof file %s to %s: %w", tmp, path, err)
	}
	if err := dbutils.SyncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync state sync proof dir %s: %w", filepath.Dir(path), err)
	}
	return nil
}
