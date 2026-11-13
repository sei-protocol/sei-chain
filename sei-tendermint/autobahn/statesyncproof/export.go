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
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var logger = seilog.NewLogger("tendermint", "autobahn", "statesyncproof")

// ExportConfig configures export of state sync proof files for retained checkpoints.
type ExportConfig struct {
	// Dir is the directory where canonical state sync proof files are written.
	Dir string
	// CheckpointHeights returns the FlatKV checkpoint heights retained locally.
	CheckpointHeights func() ([]int64, error)
	// BlockStore provides the road blocks, AppProposals and AppQCs to export.
	BlockStore BlockStore
	// Anchor publishes the latest AppQC the data layer has flushed to BlockStore. Proofs are written
	// only at or below it, so it must never cover an AppQC that a crash could lose.
	Anchor utils.AtomicRecv[data.Anchor]
	// IsClosingRoad reports whether road is the last road of an epoch.
	IsClosingRoad func(road types.RoadIndex) bool
}

// BlockStore is the part of the Autobahn BlockStore that Exporter reads.
type BlockStore interface {
	ReadBlock(types.RoadIndex) (utils.Option[*types.RoadBlock], error)
	ReadAppProposal(types.RoadIndex) (utils.Option[*types.AppProposal], error)
	ReadAppQC(types.RoadIndex) (utils.Option[*types.AppQC], error)
}

// Exporter writes the state sync proof files of retained checkpoints. It is not safe for
// concurrent use.
type Exporter struct {
	cfg ExportConfig
	// checkpoints is the retained checkpoint set that the last proof dir scan ran against, or None
	// before the first scan.
	checkpoints utils.Option[map[types.RoadIndex]struct{}]
	// exported holds the retained checkpoints whose proof file is on disk and decodes.
	exported map[types.RoadIndex]struct{}
	// skipped holds the retained checkpoints that get no proof file: LastRoad(X), or records that are
	// pruned, missing or inconsistent.
	skipped map[types.RoadIndex]struct{}
}

// NewExporter returns an Exporter that has not scanned the proof dir yet.
func NewExporter(cfg ExportConfig) *Exporter {
	return &Exporter{
		cfg:      cfg,
		exported: map[types.RoadIndex]struct{}{},
		skipped:  map[types.RoadIndex]struct{}{},
	}
}

// Sweep exports every pending checkpoint whose AppQC is durable. An I/O error on one
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
	for _, index := range e.exportable(checkpoints) {
		if err := e.exportCheckpoint(index); err != nil {
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
func (e *Exporter) syncCheckpoints(checkpoints map[types.RoadIndex]struct{}) error {
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
		maps.DeleteFunc(e.exported, func(index types.RoadIndex, _ struct{}) bool {
			_, ok := present[index]
			return !ok
		})
	} else {
		e.exported, invalidErr = e.removeInvalidFiles(present)
	}
	maps.DeleteFunc(e.skipped, func(index types.RoadIndex, _ struct{}) bool {
		_, retained := checkpoints[index]
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
func (e *Exporter) removeInvalidFiles(present map[types.RoadIndex]struct{}) (map[types.RoadIndex]struct{}, error) {
	valid := make(map[types.RoadIndex]struct{}, len(present))
	var errs []error
	// A stable error text lets the caller suppress repeated logs of the same failure.
	for _, index := range slices.Sorted(maps.Keys(present)) {
		path := filepath.Join(e.cfg.Dir, FileName(index))
		data, err := os.ReadFile(path) //nolint:gosec // path is derived from the configured state sync proof directory and a canonical file name.
		if err != nil {
			errs = append(errs, fmt.Errorf("read state sync proof file %s: %w", path, err))
			continue
		}
		if _, err := Decode(data, index); err != nil {
			logger.Warn("removing invalid state sync proof file", "path", path, "err", err)
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove invalid state sync proof file %s: %w", path, err))
			}
			continue
		}
		valid[index] = struct{}{}
	}
	return valid, errors.Join(errs...)
}

// exportable returns the checkpoints that are neither exported nor skipped and whose AppQC is
// durable, in ascending order.
func (e *Exporter) exportable(checkpoints map[types.RoadIndex]struct{}) []types.RoadIndex {
	durableAppQC, ok := e.durableAppQCIndex()
	if !ok {
		return nil
	}
	out := make([]types.RoadIndex, 0, len(checkpoints))
	for _, index := range slices.Sorted(maps.Keys(checkpoints)) {
		if _, ok := e.exported[index]; ok {
			continue
		}
		if _, ok := e.skipped[index]; ok {
			continue
		}
		if index > durableAppQC {
			continue
		}
		out = append(out, index)
	}
	return out
}

// exportCheckpoint writes the proof file of checkpoint index, or skips it when it is LastRoad(X).
func (e *Exporter) exportCheckpoint(index types.RoadIndex) error {
	if e.cfg.IsClosingRoad(index) {
		e.skipped[index] = struct{}{}
		return nil
	}
	return e.exportProof(index)
}

func (e *Exporter) durableAppQCIndex() (types.RoadIndex, bool) {
	first, ok := e.cfg.Anchor.Load().First.Get()
	if !ok {
		return 0, false
	}
	return first.AppQC.Proposal().Index(), true
}

func (e *Exporter) retainedCheckpoints() (map[types.RoadIndex]struct{}, error) {
	heights, err := e.cfg.CheckpointHeights()
	if err != nil {
		return nil, fmt.Errorf("list checkpoint heights: %w", err)
	}
	checkpoints := make(map[types.RoadIndex]struct{}, len(heights))
	for _, height := range heights {
		if height < 0 {
			return nil, fmt.Errorf("checkpoint height %d is negative", height)
		}
		checkpoints[types.RoadIndex(height)] = struct{}{} //nolint:gosec // checked above.
	}
	return checkpoints, nil
}

// exportProof writes the proof file of road index and marks it exported. Records that are
// pruned, missing or inconsistent mark it skipped.
func (e *Exporter) exportProof(index types.RoadIndex) error {
	records, err := e.readRecords(index)
	if errors.Is(err, types.ErrPruned) {
		e.skipped[index] = struct{}{}
		return nil
	}
	if err != nil {
		return err
	}
	r, ok := records.Get()
	if !ok {
		// The data layer flushes the block, AppProposal and AppQC of a road together before Anchor
		// covers it, so a record missing at or below Anchor never appears later. A checkpoint from
		// before the BlockStore held any road is one such case.
		logger.Info("skipping state sync proof: BlockStore has no records for checkpoint", "index", index)
		e.skipped[index] = struct{}{}
		return nil
	}
	encoded, err := r.encode(index)
	if err != nil {
		// Skipping, not halting, keeps a corrupt local BlockStore from stopping the node through an
		// optional export, and never publishes the bad proof.
		Global.inconsistentRecordsAt().Add(1)
		logger.Error("skipping state sync proof: inconsistent BlockStore records", "index", index, "err", err)
		e.skipped[index] = struct{}{}
		return nil
	}
	if err := writeFile(filepath.Join(e.cfg.Dir, FileName(index)), encoded); err != nil {
		return err
	}
	e.exported[index] = struct{}{}
	return nil
}

// records are the BlockStore records of one road that a proof file is built from.
type records struct {
	roadBlock   *types.RoadBlock
	appProposal *types.AppProposal
	appQC       *types.AppQC
}

// encode returns the proof file of road index. It fails when the records do not all belong to road
// index or the local AppHash does not match the AppQC.
func (r records) encode(index types.RoadIndex) ([]byte, error) {
	// The data layer checks this before it publishes Anchor. Keep the check here as defense in depth against
	// a future writer path or corrupted local records.
	if !bytes.Equal(r.appProposal.AppHash(), r.appQC.Proposal().AppHash()) {
		return nil, fmt.Errorf("AppProposal(%d) app hash does not match AppQC", index)
	}
	if r.appQC.Proposal().Index() != index {
		return nil, fmt.Errorf("AppQC index %d does not match checkpoint index %d", r.appQC.Proposal().Index(), index)
	}
	return Encode(r.roadBlock, r.appQC)
}

// readRecords reads the records of road index. It returns None when any record is missing, and an
// error wrapping types.ErrPruned when any record is pruned.
func (e *Exporter) readRecords(index types.RoadIndex) (utils.Option[records], error) {
	none := utils.None[records]()
	roadBlock, err := e.cfg.BlockStore.ReadBlock(index)
	if err != nil {
		return none, fmt.Errorf("read road block %d: %w", index, err)
	}
	appProposal, err := e.cfg.BlockStore.ReadAppProposal(index)
	if err != nil {
		return none, fmt.Errorf("read AppProposal %d: %w", index, err)
	}
	appQC, err := e.cfg.BlockStore.ReadAppQC(index)
	if err != nil {
		return none, fmt.Errorf("read AppQC %d: %w", index, err)
	}
	var r records
	var ok bool
	if r.roadBlock, ok = roadBlock.Get(); !ok {
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
	checkpoints map[types.RoadIndex]struct{},
) (map[types.RoadIndex]struct{}, error) {
	present := map[types.RoadIndex]struct{}{}
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
		index, err := ParseFileName(name)
		if err != nil {
			continue
		}
		if _, ok := checkpoints[index]; ok {
			present[index] = struct{}{}
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
