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
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
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
	Status() utils.Option[blockstore.SuffixRange]
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
	// skipped holds the retained checkpoints that get no proof file: LastRoad(X), or pruned records.
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
	if err := exporter.sweepAndHandleError(); err != nil {
		return err
	}
	// Anchor moves with every durable AppQC, so a live chain retries failed exports and picks up
	// checkpoints that appear after their AppQC within one block. A halted chain has no new proof
	// to write until the startup sweep after the restart.
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
				if err := exporter.sweepAndHandleError(); err != nil {
					return err
				}
			}
		})
		return nil
	})
}

// sweepAndHandleError runs one sweep. It returns only invariant errors. It counts every
// other failure and logs it when its text differs from the last logged failure.
func (e *stateSyncProofExporter) sweepAndHandleError() error {
	err := e.sweepOnce()
	if err == nil {
		e.lastErr = ""
		return nil
	}
	var invariant stateSyncProofInvariantError
	if errors.As(err, &invariant) {
		return err
	}
	Global.gigaStateSyncProofExportFailuresAt().Add(1)
	// Every durable AppQC wakes a sweep, so a permanent error would otherwise log once per block.
	if msg := err.Error(); msg != e.lastErr {
		logger.Error("state sync proof export failed; retrying on next sweep", "err", err)
		e.lastErr = msg
	}
	return nil
}

// sweepOnce exports every pending checkpoint whose AppQC is durable. An I/O error on one
// checkpoint does not stop the others; the sweep returns all of them joined. An invariant error
// stops the sweep at once.
func (e *stateSyncProofExporter) sweepOnce() error {
	checkpoints, err := e.retainedCheckpoints()
	if err != nil {
		return err
	}
	if err := e.syncCheckpoints(checkpoints); err != nil {
		return err
	}
	var errs []error
	for _, index := range e.exportable(checkpoints) {
		if err := e.exportCheckpoint(index); err != nil {
			var invariant stateSyncProofInvariantError
			if errors.As(err, &invariant) {
				return err
			}
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// syncCheckpoints rescans the proof dir on the first sweep and whenever the retained checkpoint
// set changes. The first scan keeps only the proof files that decode. Every scan forgets the
// checkpoints that are no longer retained and the exported checkpoints whose file is gone, so the
// next export writes them again.
func (e *stateSyncProofExporter) syncCheckpoints(checkpoints map[types.RoadIndex]struct{}) error {
	last, scanned := e.checkpoints.Get()
	if scanned && maps.Equal(last, checkpoints) {
		return nil
	}
	present, err := e.removeOrphans(checkpoints)
	if err != nil {
		return err
	}
	if scanned {
		maps.DeleteFunc(e.exported, func(index types.RoadIndex, _ struct{}) bool {
			_, ok := present[index]
			return !ok
		})
	} else {
		valid, err := e.removeInvalidFiles(present)
		if err != nil {
			return err
		}
		e.exported = valid
	}
	maps.DeleteFunc(e.skipped, func(index types.RoadIndex, _ struct{}) bool {
		_, retained := checkpoints[index]
		return !retained
	})
	e.checkpoints = utils.Some(checkpoints)
	return nil
}

// removeInvalidFiles removes the proof files in present that do not decode for the road in their
// name, and returns the rest.
func (e *stateSyncProofExporter) removeInvalidFiles(present map[types.RoadIndex]struct{}) (map[types.RoadIndex]struct{}, error) {
	valid := make(map[types.RoadIndex]struct{}, len(present))
	for index := range present {
		path := filepath.Join(e.cfg.Dir, statesyncproof.FileName(index))
		data, err := os.ReadFile(path) //nolint:gosec // path is derived from the configured state sync proof directory and a canonical file name.
		if err != nil {
			return nil, fmt.Errorf("read state sync proof file %s: %w", path, err)
		}
		if _, err := statesyncproof.Decode(data, index); err != nil {
			logger.Warn("removing invalid state sync proof file", "path", path, "err", err)
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("remove invalid state sync proof file %s: %w", path, err)
			}
			continue
		}
		valid[index] = struct{}{}
	}
	return valid, nil
}

// exportable returns the checkpoints that are neither exported nor skipped and whose AppQC is
// durable and in the BlockStore, in ascending order.
func (e *stateSyncProofExporter) exportable(checkpoints map[types.RoadIndex]struct{}) []types.RoadIndex {
	status, ok := e.cfg.BlockStore.Status().Get()
	if !ok {
		return nil
	}
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
		if index > durableAppQC || index >= status.NextAppQC {
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

// exportStateSyncProof writes the proof file of road index and marks it exported. Pruned records
// mark it skipped. Missing records leave it pending, so the next sweep reads them again.
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
		return nil
	}
	if !bytes.Equal(r.appProposal.AppHash(), r.appQC.Proposal().AppHash()) {
		// The data layer checks this before it publishes Anchor. Keep the check here as defense in depth against
		// a future writer path or corrupted local records.
		return newStateSyncProofInvariantError("AppProposal(%d) app hash does not match AppQC", index)
	}
	if r.appQC.Proposal().Index() != index {
		return newStateSyncProofInvariantError("AppQC index %d does not match checkpoint index %d", r.appQC.Proposal().Index(), index)
	}
	encoded, err := statesyncproof.Encode(r.roadBlock, r.appQC)
	if err != nil {
		return newStateSyncProofInvariantError("encode state sync proof %d: %w", index, err)
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

// removeOrphans removes the stale temp files and the proof files of checkpoints that are not
// retained. It returns the retained checkpoints that have a proof file.
func (e *stateSyncProofExporter) removeOrphans(checkpoints map[types.RoadIndex]struct{}) (map[types.RoadIndex]struct{}, error) {
	present := map[types.RoadIndex]struct{}{}
	entries, err := os.ReadDir(e.cfg.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return present, nil
		}
		return nil, fmt.Errorf("read state sync proof dir %s: %w", e.cfg.Dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(e.cfg.Dir, name)
		if filepath.Ext(name) == ".tmp" {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("remove stale temp state sync proof %s: %w", path, err)
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
			return nil, fmt.Errorf("remove orphan state sync proof %s: %w", path, err)
		}
	}
	return present, nil
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

type stateSyncProofInvariantError struct {
	err error
}

func newStateSyncProofInvariantError(format string, args ...any) stateSyncProofInvariantError {
	return stateSyncProofInvariantError{err: fmt.Errorf(format, args...)}
}

func (e stateSyncProofInvariantError) Error() string {
	return e.err.Error()
}

func (e stateSyncProofInvariantError) Unwrap() error {
	return e.err
}
