// TODO: add Prometheus metrics for blocks written and truncated.
package persist

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "internal", "autobahn", "consensus", "persist")

const blocksDir = "blocks"

// blocksWALName identifies every lane WAL in metrics. All lanes share it rather than deriving a name from
// the lane, which would make a metric label out of a validator's public key.
//
// Because the lanes are live at the same time under one name, their metrics are disabled: seiwal's
// queue-depth gauge is keyed on the name alone, so the lanes would overwrite each other's samples. Giving
// each lane a unique name — folding in laneDir(lane) — is what it would take to turn them back on.
const blocksWALName = "autobahn_blocks"

// blocksWALMetrics is whether lane WALs record metrics. See blocksWALName for why they cannot.
const blocksWALMetrics = false

// LoadedBlock is a block loaded from disk during state restoration.
type LoadedBlock struct {
	Number   types.BlockNumber
	Proposal *types.Signed[*types.LaneProposal]
}

// laneWALState is the mutable state of a lane WAL, protected by laneWAL's
// mutex. Each block is stored under its own block number, so no mapping
// between block numbers and WAL indices is needed.
type laneWALState struct {
	wal          seiwal.WAL[*types.Signed[*types.LaneProposal]]
	nextBlockNum types.BlockNumber
}

// persistBlock schedules a proposal for the WAL and advances nextBlockNum. The block is not durable
// until flush returns. Caller must hold the per-lane lock.
func (s *laneWALState) persistBlock(proposal *types.Signed[*types.LaneProposal]) error {
	h := proposal.Msg().Block().Header()
	// A lane whose cursor is still zero has never held a block, so its first block sets the baseline
	// and may carry any number.
	if s.nextBlockNum > 0 && h.BlockNumber() != s.nextBlockNum {
		return fmt.Errorf("block %s/%d out of sequence (next=%d)", h.Lane(), h.BlockNumber(), s.nextBlockNum)
	}
	if err := s.wal.Append(uint64(h.BlockNumber()), proposal); err != nil {
		return fmt.Errorf("persist block %s/%d: %w", h.Lane(), h.BlockNumber(), err)
	}
	s.nextBlockNum = h.BlockNumber() + 1
	return nil
}

// flush makes every block scheduled by persistBlock durable. Caller must hold the per-lane lock.
func (s *laneWALState) flush(lane types.LaneID) error {
	if err := s.wal.Flush(); err != nil {
		return fmt.Errorf("flush lane %s WAL: %w", lane, err)
	}
	return nil
}

// truncateForAnchor prunes the WAL so that `first` becomes the oldest retained block number, and moves
// the cursor up when the anchor has advanced past every block the lane holds.
//
// Pruning is lazy: blocks below `first` may remain on disk until the file holding them falls entirely
// below the threshold. Caller must hold the per-lane lock.
func (s *laneWALState) truncateForAnchor(lane types.LaneID, first types.BlockNumber) error {
	if err := s.wal.PruneBefore(uint64(first)); err != nil {
		return fmt.Errorf("prune lane %s WAL before block %d: %w", lane, first, err)
	}
	if first > s.nextBlockNum {
		// The anchor moved past every block persisted for this lane, so the next block to persist is
		// the anchor's first. That leaves a gap, which the WAL is configured to permit.
		s.nextBlockNum = first
	}
	return nil
}

// loadAll reads the lane WAL and returns the blocks that are still live, restoring nextBlockNum from
// the last one.
//
// Blocks stranded below a lazy prune are discarded: a gap in the stored block numbers marks where
// pruning has already logically removed everything before it.
func (s *laneWALState) loadAll(lane types.LaneID) ([]LoadedBlock, error) {
	entries, err := readAll(s.wal)
	if err != nil {
		return nil, fmt.Errorf("read lane %s WAL: %w", lane, err)
	}
	live := contiguousSuffix(entries)

	loaded := make([]LoadedBlock, 0, len(live))
	for _, entry := range live {
		h := entry.value.Msg().Block().Header()
		s.nextBlockNum = h.BlockNumber() + 1
		loaded = append(loaded, LoadedBlock{Number: h.BlockNumber(), Proposal: entry.value})
	}
	if len(loaded) > 0 {
		first, last := loaded[0].Number, loaded[len(loaded)-1].Number
		logger.Debug("loaded persisted blocks", "lane", lane.String(),
			"first", first, "last", last, "count", len(loaded), "discarded", len(entries)-len(live))
	}
	return loaded, nil
}

// laneWAL wraps a laneWALState with a mutex that serializes all writes and
// truncations on a single lane.
type laneWAL struct {
	state utils.Mutex[*laneWALState]
}

func (lw *laneWAL) maybePruneAndPersist(
	lane types.LaneID,
	deleteBefore utils.Option[types.BlockNumber],
	proposals []*types.Signed[*types.LaneProposal],
) error {
	for s := range lw.state.Lock() {
		if first, ok := deleteBefore.Get(); ok {
			if err := s.truncateForAnchor(lane, first); err != nil {
				return err
			}
		}
		for _, p := range proposals {
			if p.Msg().Block().Header().Lane() != lane {
				return fmt.Errorf("persist lane %s: proposal has lane %s", lane, p.Msg().Block().Header().Lane())
			}
			if err := s.persistBlock(p); err != nil {
				return err
			}
		}
		if len(proposals) > 0 {
			// persistBlock only schedules the write, so the blocks are not durable — and must not be
			// reported as persisted — until this returns.
			if err := s.flush(lane); err != nil {
				return err
			}
		}
		return nil
	}
	panic("unreachable")
}

func (lw *laneWAL) close() error {
	for s := range lw.state.Lock() {
		return s.wal.Close()
	}
	panic("unreachable")
}

// BlockPersister manages block persistence using one WAL per lane.
// Each lane gets its own WAL in a subdirectory named by hex-encoded lane ID,
// so truncation is independent per lane. A single shared WAL would be simpler
// but a lane whose blocks are never included in a committed block (e.g. the
// validator is removed from the committee) would prevent truncation of all
// other lanes' entries that follow it.
// When dir is None, all disk I/O is skipped (no-op mode).
//
// All public methods are safe for concurrent use.
type BlockPersister struct {
	dir   utils.Option[string] // immutable after construction
	lanes utils.RWMutex[map[types.LaneID]*laneWAL]
}

func laneDir(lane types.LaneID) string {
	return lane.HexString()
}

func newLaneWALState(dir string) (*laneWALState, error) {
	wal, err := openWAL(dir, blocksWALName, types.SignedLaneProposalConv, targetFileSize, blocksWALMetrics)
	if err != nil {
		return nil, err
	}
	return &laneWALState{wal: wal}, nil
}

// NewBlockPersister opens (or creates) per-lane WALs in subdirectories of
// blocks/ and replays all persisted entries. Returns the persister and loaded
// blocks grouped by lane (sorted by block number). A torn entry at the end of a
// lane WAL, left by a crash mid-write, is discarded by the WAL on open.
//
// The blocks returned for a lane are a superset of what the prune anchor
// considers live: pruning is lazy, so blocks the anchor has moved past can still
// be on disk. Blocks stranded behind a gap are dropped, but those directly below
// the anchor are indistinguishable from live ones here and are filtered by the
// caller, which is what holds the anchor.
//
// When stateDir is None, returns a no-op persister.
func NewBlockPersister(stateDir utils.Option[string]) (*BlockPersister, map[types.LaneID][]LoadedBlock, error) {
	sd, ok := stateDir.Get()
	if !ok {
		return &BlockPersister{lanes: utils.NewRWMutex(map[types.LaneID]*laneWAL{})}, nil, nil
	}
	dir := filepath.Join(sd, blocksDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, fmt.Errorf("create blocks dir %s: %w", dir, err)
	}

	lanes := map[types.LaneID]*laneWAL{}
	bp := &BlockPersister{dir: utils.Some(dir), lanes: utils.NewRWMutex(lanes)}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read blocks dir %s: %w", dir, err)
	}

	allBlocks := map[types.LaneID][]LoadedBlock{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		laneBytes, err := hex.DecodeString(e.Name())
		if err != nil {
			logger.Warn("skipping unexpected entry in blocks dir", "name", e.Name())
			continue
		}
		lane, err := types.LaneIDFromBytes(laneBytes)
		if err != nil {
			// Pre-LaneID hex(pubkey) dirs fail to parse; they leak until the
			// operator wipes persistent_state_dir (no automatic migration).
			logger.Warn("skipping lane dir with invalid LaneID (leaks until state wipe)", "name", e.Name(), "err", err)
			continue
		}
		lanePath := filepath.Join(dir, e.Name())
		s, err := newLaneWALState(lanePath)
		if err != nil {
			_ = bp.Close()
			return nil, nil, fmt.Errorf("open lane WAL in %s: %w", lanePath, err)
		}
		loaded, err := s.loadAll(lane)
		if err != nil {
			_ = s.wal.Close()
			_ = bp.Close()
			return nil, nil, fmt.Errorf("load lane WAL in %s: %w", lanePath, err)
		}
		lanes[lane] = &laneWAL{state: utils.NewMutex(s)}
		if len(loaded) > 0 {
			allBlocks[lane] = loaded
		}
	}

	return bp, allBlocks, nil
}

// getOrCreateLane returns the lane WAL under the caller-held map lock.
func (bp *BlockPersister) getOrCreateLane(lanes map[types.LaneID]*laneWAL, lane types.LaneID, allowCreate bool) (*laneWAL, bool, error) {
	if lw, ok := lanes[lane]; ok {
		return lw, true, nil
	}
	if !allowCreate {
		return nil, false, nil
	}
	dir, ok := bp.dir.Get()
	if !ok {
		return nil, false, fmt.Errorf("getOrCreateLane called on no-op persister")
	}
	s, err := newLaneWALState(filepath.Join(dir, laneDir(lane)))
	if err != nil {
		return nil, false, fmt.Errorf("create lane WAL for %s: %w", lane, err)
	}
	lw := &laneWAL{state: utils.NewMutex(s)}
	lanes[lane] = lw
	return lw, true, nil
}

// MaybePruneAndPersistLane optionally truncates the lane's WAL and/or appends
// new proposals, depending on which arguments are present:
//
//   - deleteBefore set, proposals non-empty: truncate, then append (runtime path).
//   - deleteBefore set, proposals empty:     truncate only, no appends.
//   - deleteBefore empty, proposals non-empty: append only, no truncation.
//   - deleteBefore empty, proposals empty:     no-op.
//
// allowCreate: open a WAL if missing. Avail passes true when proposals are
// non-empty, so a validator that left before the first open still flushes
// pending proposals. After SyncLanes removes a lane, empty-proposal prune passes
// false so the WAL is not recreated.
//
// Appends are not durable until this returns (one flush for the whole batch).
// No-op persister (dir=None): skips disk I/O.
// Does not spawn goroutines — the caller schedules parallelism per lane.
func (bp *BlockPersister) MaybePruneAndPersistLane(
	lane types.LaneID,
	allowCreate bool,
	deleteBefore utils.Option[types.BlockNumber],
	proposals []*types.Signed[*types.LaneProposal],
) error {
	if _, ok := bp.dir.Get(); !ok {
		return nil
	}

	// Keep the lanes map read-locked for the whole persist so SyncLanes cannot
	// delete the WAL out from under this call. Lanes stay parallel: only the
	// create path below needs the write lock.
	for lanes := range bp.lanes.RLock() {
		if lw, ok := lanes[lane]; ok {
			return lw.maybePruneAndPersist(lane, deleteBefore, proposals)
		}
	}
	if !allowCreate {
		return nil
	}
	// Create path: hold the write lock through create+persist so SyncLanes cannot race.
	for lanes := range bp.lanes.Lock() {
		lw, _, err := bp.getOrCreateLane(lanes, lane, true)
		if err != nil {
			return err
		}
		return lw.maybePruneAndPersist(lane, deleteBefore, proposals)
	}
	panic("unreachable")
}

// deleteLaneLocked closes and removes one open lane WAL. Caller holds the map write lock.
// No-op if not open.
func (bp *BlockPersister) deleteLaneLocked(lanes map[types.LaneID]*laneWAL, dir string, lane types.LaneID) error {
	lw, ok := lanes[lane]
	if !ok {
		return nil
	}
	if err := lw.close(); err != nil {
		return fmt.Errorf("close lane %s WAL: %w", lane, err)
	}
	delete(lanes, lane)
	path := filepath.Join(dir, laneDir(lane))
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove lane dir %s: %w", path, err)
	}
	logger.Info("deleted inactive lane WAL", "lane", lane.String())
	return nil
}

// SyncLanes deletes open WALs whose LaneID is not a key of keep. Idempotent.
func SyncLanes[V any](bp *BlockPersister, keep map[types.LaneID]V) error {
	dir, ok := bp.dir.Get()
	if !ok {
		return nil
	}
	for lanes := range bp.lanes.Lock() {
		var stale []types.LaneID
		for lane := range lanes {
			if _, ok := keep[lane]; !ok {
				stale = append(stale, lane)
			}
		}
		for _, lane := range stale {
			if err := bp.deleteLaneLocked(lanes, dir, lane); err != nil {
				return err
			}
		}
		return nil
	}
	panic("unreachable")
}

// Close shuts down all per-lane WALs, releasing the exclusive lock each one holds on its directory.
//
// Production does not call this: a node exits by rugpull and the OS reclaims everything. It exists so
// that a process which opens the same state directory more than once in its lifetime — a test
// simulating a restart — can release the first owner before the second opens.
// Safe for concurrent use.
func (bp *BlockPersister) Close() error {
	if _, ok := bp.dir.Get(); !ok {
		return nil // no-op persister (persistence disabled)
	}
	for lanes := range bp.lanes.Lock() {
		var errs []error
		for _, lw := range lanes {
			if err := lw.close(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	panic("unreachable")
}
