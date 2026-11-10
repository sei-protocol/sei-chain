package p2p

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	dbutils "github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// StateSyncProofExportConfig configures export of state sync proof files for retained checkpoints.
type StateSyncProofExportConfig struct {
	// Dir is the directory where canonical state sync proof files are written.
	Dir string
	// CheckpointHeights returns the FlatKV checkpoint heights retained locally.
	CheckpointHeights func() ([]int64, error)
	// BlockStore provides the road blocks, AppProposals and AppQCs to export.
	BlockStore stateSyncProofBlockStore
	// Anchor publishes the latest AppQC the data layer has flushed to BlockStore. Proofs are written
	// only at or below it, so it must never cover an AppQC that a crash could lose.
	Anchor utils.AtomicRecv[utils.Option[data.Anchor]]
}

type stateSyncProofBlockStore interface {
	ReadBlock(types.RoadIndex) (utils.Option[*types.RoadBlock], error)
	ReadAppProposal(types.RoadIndex) (utils.Option[*types.AppProposal], error)
	ReadAppQC(types.RoadIndex) (utils.Option[*types.AppQC], error)
}

type stateSyncProofExporter struct {
	cfg          StateSyncProofExportConfig
	closingEpoch func(types.RoadIndex) utils.Option[types.EpochIndex]
	// lastErr is the text of the last logged sweep error, or empty after a successful sweep.
	lastErr string
	// checkpoints is the retained checkpoint set that the last proof dir scan ran against, or None
	// before the first scan.
	checkpoints utils.Option[map[types.RoadIndex]struct{}]
	// exported holds the retained checkpoints whose proof file is on disk and decodes.
	exported map[types.RoadIndex]struct{}
	// skipped holds the retained checkpoints that get no proof file: LastRoad(X), or records that are
	// pruned, missing or inconsistent.
	skipped map[types.RoadIndex]struct{}
}

func newStateSyncProofExporter(
	cfg StateSyncProofExportConfig,
	closingEpoch func(types.RoadIndex) utils.Option[types.EpochIndex],
) *stateSyncProofExporter {
	return &stateSyncProofExporter{
		cfg:          cfg,
		closingEpoch: closingEpoch,
		exported:     map[types.RoadIndex]struct{}{},
		skipped:      map[types.RoadIndex]struct{}{},
	}
}

// runStateSyncProofExport sweeps once at startup and then on every Anchor update.
func runStateSyncProofExport(
	ctx context.Context,
	cfg StateSyncProofExportConfig,
	closingEpoch func(types.RoadIndex) utils.Option[types.EpochIndex],
) error {
	exporter := newStateSyncProofExporter(cfg, closingEpoch)
	// Anchor moves with every durable AppQC, so a live chain retries failed exports and picks up
	// checkpoints that appear after their AppQC within one block. FlatKV writes checkpoints
	// asynchronously, so on a halted chain a checkpoint published after the last Anchor update waits
	// for the startup sweep after the restart. Iter delivers the current Anchor first, which runs the
	// startup sweep.
	wake := make(chan struct{}, 1)
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnNamed("watchAnchor", func() error {
			return cfg.Anchor.Iter(ctx, func(context.Context, utils.Option[data.Anchor]) error {
				return utils.SendOrDrop(wake, struct{}{})
			})
		})
		s.SpawnNamed("sweep", func() error {
			for {
				if _, err := utils.Recv(ctx, wake); err != nil {
					return err
				}
				exporter.sweepAndHandleError()
			}
		})
		return nil
	})
}

// sweepAndHandleError runs one sweep. It counts a failed sweep and logs it when its text differs
// from the last logged failure.
func (e *stateSyncProofExporter) sweepAndHandleError() {
	err := e.sweepOnce()
	if err == nil {
		e.lastErr = ""
		return
	}
	Global.gigaStateSyncProofExportFailuresAt().Add(1)
	// Every durable AppQC wakes a sweep, so a permanent error would otherwise log once per block.
	if msg := err.Error(); msg != e.lastErr {
		logger.Error("state sync proof export failed; retrying on next sweep", "err", err)
		e.lastErr = msg
	}
}

// sweepOnce exports every pending checkpoint whose AppQC is durable. An I/O error on one
// checkpoint or proof file does not stop the others; the sweep returns all of them joined.
func (e *stateSyncProofExporter) sweepOnce() error {
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

// syncCheckpoints rescans the proof dir on the first sweep and whenever the retained checkpoint
// set changes. The first scan keeps only the proof files that decode. Every scan forgets the
// checkpoints that are no longer retained and the exported checkpoints whose file is gone, so the
// next export writes them again. A file that cannot be read or removed does not stop the scan: it
// is not counted as exported, and its error is returned with the others.
func (e *stateSyncProofExporter) syncCheckpoints(checkpoints map[types.RoadIndex]struct{}) error {
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
	e.checkpoints = utils.Some(checkpoints)
	return errors.Join(orphanErr, invalidErr)
}

// removeInvalidFiles removes the proof files in present that do not decode for the road in their
// name, and returns the rest. A file it cannot read or remove is left out of the result, and its
// error is returned joined with the others.
func (e *stateSyncProofExporter) removeInvalidFiles(present map[types.RoadIndex]struct{}) (map[types.RoadIndex]struct{}, error) {
	valid := make(map[types.RoadIndex]struct{}, len(present))
	var errs []error
	for index := range present {
		path := filepath.Join(e.cfg.Dir, statesyncproof.FileName(index))
		data, err := os.ReadFile(path) //nolint:gosec // path is derived from the configured state sync proof directory and a canonical file name.
		if err != nil {
			errs = append(errs, fmt.Errorf("read state sync proof file %s: %w", path, err))
			continue
		}
		if _, err := statesyncproof.Decode(data, index); err != nil {
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
func (e *stateSyncProofExporter) exportable(checkpoints map[types.RoadIndex]struct{}) []types.RoadIndex {
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
func (e *stateSyncProofExporter) exportCheckpoint(index types.RoadIndex) error {
	if e.closingEpoch(index).IsPresent() {
		e.skipped[index] = struct{}{}
		return nil
	}
	return e.exportStateSyncProof(index)
}

func (e *stateSyncProofExporter) durableAppQCIndex() (types.RoadIndex, bool) {
	anchor, ok := e.cfg.Anchor.Load().Get()
	if !ok {
		return 0, false
	}
	return anchor.AppQC.Proposal().Index(), true
}

func (e *stateSyncProofExporter) retainedCheckpoints() (map[types.RoadIndex]struct{}, error) {
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

// exportStateSyncProof writes the proof file of road index and marks it exported. Records that are
// pruned, missing or inconsistent mark it skipped.
func (e *stateSyncProofExporter) exportStateSyncProof(index types.RoadIndex) error {
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
		Global.gigaStateSyncProofExportFailuresAt().Add(1)
		logger.Error("skipping state sync proof: inconsistent BlockStore records", "index", index, "err", err)
		e.skipped[index] = struct{}{}
		return nil
	}
	if err := writeStateSyncProofFile(filepath.Join(e.cfg.Dir, statesyncproof.FileName(index)), encoded); err != nil {
		return err
	}
	e.exported[index] = struct{}{}
	return nil
}

// stateSyncProofRecords are the BlockStore records of one road that a proof file is built from.
type stateSyncProofRecords struct {
	roadBlock   *types.RoadBlock
	appProposal *types.AppProposal
	appQC       *types.AppQC
}

// encode returns the proof file of road index. It fails when the records do not all belong to road
// index or the local AppHash does not match the AppQC.
func (r stateSyncProofRecords) encode(index types.RoadIndex) ([]byte, error) {
	// The data layer checks this before it publishes Anchor. Keep the check here as defense in depth against
	// a future writer path or corrupted local records.
	if !bytes.Equal(r.appProposal.AppHash(), r.appQC.Proposal().AppHash()) {
		return nil, fmt.Errorf("AppProposal(%d) app hash does not match AppQC", index)
	}
	if r.appQC.Proposal().Index() != index {
		return nil, fmt.Errorf("AppQC index %d does not match checkpoint index %d", r.appQC.Proposal().Index(), index)
	}
	return statesyncproof.Encode(r.roadBlock, r.appQC)
}

// readRecords reads the records of road index. It returns None when any record is missing, and an
// error wrapping types.ErrPruned when any record is pruned.
func (e *stateSyncProofExporter) readRecords(index types.RoadIndex) (utils.Option[stateSyncProofRecords], error) {
	none := utils.None[stateSyncProofRecords]()
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
	var r stateSyncProofRecords
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
func (e *stateSyncProofExporter) removeOrphans(
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
		index, err := statesyncproof.ParseFileName(name)
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

func writeStateSyncProofFile(path string, data []byte) error {
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
