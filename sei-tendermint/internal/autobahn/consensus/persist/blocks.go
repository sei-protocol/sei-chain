// TODO: add Prometheus metrics for blocks written and truncated.
package persist

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("tendermint", "internal", "autobahn", "consensus", "persist")

const blocksDir = "blocks"

// LoadedBlock is a block loaded from disk during state restoration.
type LoadedBlock struct {
	Number   types.BlockNumber
	Proposal *types.Signed[*types.LaneProposal]
}

// laneWALState is the mutable state of a lane WAL, protected by laneWAL's
// mutex. Block numbers within a lane are contiguous, so the first block
// number is derived: nextBlockNum - Count().
type laneWALState struct {
	*indexedWAL[*types.Signed[*types.LaneProposal]]
	nextBlockNum types.BlockNumber
}

func (s *laneWALState) firstBlockNum() utils.Option[types.BlockNumber] {
	if s.Count() == 0 {
		return utils.None[types.BlockNumber]()
	}
	return utils.Some(s.nextBlockNum - types.BlockNumber(s.Count()))
}

// persistBlock writes a proposal to the WAL and advances nextBlockNum.
// Caller must hold the per-lane lock.
func (s *laneWALState) persistBlock(proposal *types.Signed[*types.LaneProposal]) error {
	h := proposal.Msg().Block().Header()
	if (s.Count() > 0 || s.nextBlockNum > 0) && h.BlockNumber() != s.nextBlockNum {
		return fmt.Errorf("block %s/%d out of sequence (next=%d)", h.Lane(), h.BlockNumber(), s.nextBlockNum)
	}
	if err := s.Write(proposal); err != nil {
		return fmt.Errorf("persist block %s/%d: %w", h.Lane(), h.BlockNumber(), err)
	}
	s.nextBlockNum = h.BlockNumber() + 1
	return nil
}

// truncateForAnchor truncates the WAL so that `first` becomes the oldest
// retained block number. Caller must hold the per-lane lock.
func (s *laneWALState) truncateForAnchor(lane types.LaneID, first types.BlockNumber) error {
	firstBN, ok := s.firstBlockNum().Get()
	if !ok {
		// WAL is empty; nothing to truncate but advance the cursor so
		// the next persistBlock expects the right block number.
		if first > s.nextBlockNum {
			s.nextBlockNum = first
		}
		return nil
	}
	if first <= firstBN {
		return nil
	}
	if first >= s.nextBlockNum {
		if err := s.TruncateAll(); err != nil {
			return fmt.Errorf("truncate all lane %s WAL: %w", lane, err)
		}
		s.nextBlockNum = first
		return nil
	}
	walIdx := s.FirstIdx() + uint64(first-firstBN)
	if err := s.TruncateBefore(walIdx, func(entry *types.Signed[*types.LaneProposal]) error {
		if got := entry.Msg().Block().Header().BlockNumber(); got != first {
			return fmt.Errorf("block at WAL index %d has number %d, expected %d (index mapping broken)", walIdx, got, first)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("truncate lane %s WAL before block %d: %w", lane, first, err)
	}
	return nil
}

// loadAll reads all entries from the lane WAL and returns the loaded blocks.
// Also restores nextBlockNum from the last entry.
func (s *laneWALState) loadAll() ([]LoadedBlock, error) {
	entries, err := s.ReadAll()
	if err != nil {
		return nil, err
	}
	loaded := make([]LoadedBlock, 0, len(entries))
	for i, proposal := range entries {
		h := proposal.Msg().Block().Header()
		if i > 0 && h.BlockNumber() != s.nextBlockNum {
			return nil, fmt.Errorf("gap in lane %s: block %d follows %d", h.Lane(), h.BlockNumber(), s.nextBlockNum-1)
		}
		s.nextBlockNum = h.BlockNumber() + 1
		loaded = append(loaded, LoadedBlock{Number: h.BlockNumber(), Proposal: proposal})
	}
	if len(loaded) > 0 {
		first, last := loaded[0].Number, loaded[len(loaded)-1].Number
		logger.Debug("loaded persisted blocks", "lane", entries[0].Msg().Block().Header().Lane().String(),
			"first", first, "last", last, "count", len(loaded))
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
	anchor utils.Option[*types.CommitQC],
	proposals []*types.Signed[*types.LaneProposal],
	afterEach utils.Option[func(*types.Signed[*types.LaneProposal])],
) error {
	for s := range lw.state.Lock() {
		if qc, ok := anchor.Get(); ok {
			if err := s.truncateForAnchor(lane, qc.LaneRange(lane).First()); err != nil {
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
			if fn, ok := afterEach.Get(); ok {
				fn(p)
			}
		}
		return nil
	}
	panic("unreachable")
}

func (lw *laneWAL) close() error {
	for s := range lw.state.Lock() {
		return s.Close()
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
// All public methods are safe for concurrent use. The lanes map is protected
// by an RWMutex; each laneWAL has its own Mutex for write serialization.
// MaybePruneAndPersistLane holds the per-lane lock for the entire
// truncate-then-append sequence, so concurrent calls on the same lane
// serialize correctly. Different lanes are fully parallel.
//
// NOTE: MaybePruneAndPersistLane releases the map RLock before acquiring
// the per-lane lock. This is safe because lanes are only added, never
// removed. If lane deletion is added in the future, the map RLock must be
// held through the WAL write.
type BlockPersister struct {
	dir   utils.Option[string] // immutable after construction
	lanes utils.RWMutex[map[types.LaneID]*laneWAL]
}

func laneDir(lane types.LaneID) string {
	return hex.EncodeToString(lane.Bytes())
}

func newLaneWALState(dir string) (*laneWALState, error) {
	iw, err := openIndexedWAL(dir, types.SignedMsgConv[*types.LaneProposal]())
	if err != nil {
		return nil, err
	}
	return &laneWALState{indexedWAL: iw}, nil
}

// NewBlockPersister opens (or creates) per-lane WALs in subdirectories of
// blocks/ and replays all persisted entries. Returns the persister and loaded
// blocks grouped by lane (sorted by block number). Corrupt tail entries are
// auto-truncated by the WAL library.
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
		lane, err := types.PublicKeyFromBytes(laneBytes)
		if err != nil {
			logger.Warn("skipping lane dir with invalid key", "name", e.Name(), "err", err)
			continue
		}
		lanePath := filepath.Join(dir, e.Name())
		s, err := newLaneWALState(lanePath)
		if err != nil {
			_ = bp.close()
			return nil, nil, fmt.Errorf("open lane WAL in %s: %w", lanePath, err)
		}
		loaded, err := s.loadAll()
		if err != nil {
			_ = s.Close()
			_ = bp.close()
			return nil, nil, fmt.Errorf("load lane WAL in %s: %w", lanePath, err)
		}
		lanes[lane] = &laneWAL{state: utils.NewMutex(s)}
		if len(loaded) > 0 {
			allBlocks[lane] = loaded
		}
	}

	return bp, allBlocks, nil
}

// getOrCreateLane returns the laneWAL for the given lane, creating it if
// necessary. Uses double-checked locking: fast path reads under RLock;
// slow path (lane creation) promotes to a write Lock.
// The returned pointer is safe to use after the lock is released because
// lanes are only ever added, never removed (see BlockPersister doc).
// Returns an error if called on a no-op persister (caller should check first).
func (bp *BlockPersister) getOrCreateLane(lane types.LaneID) (*laneWAL, error) {
	dir, ok := bp.dir.Get()
	if !ok {
		return nil, fmt.Errorf("getOrCreateLane called on no-op persister")
	}
	// Fast path: read-only check.
	for lanes := range bp.lanes.RLock() {
		if lw, ok := lanes[lane]; ok {
			return lw, nil
		}
	}
	// Slow path: create under write lock (double-checked).
	for lanes := range bp.lanes.Lock() {
		if lw, ok := lanes[lane]; ok {
			return lw, nil
		}
		s, err := newLaneWALState(filepath.Join(dir, laneDir(lane)))
		if err != nil {
			return nil, fmt.Errorf("create lane WAL for %s: %w", lane, err)
		}
		lw := &laneWAL{state: utils.NewMutex(s)}
		lanes[lane] = lw
		return lw, nil
	}
	panic("unreachable")
}

// MaybePruneAndPersistLane optionally truncates the lane's WAL and/or appends
// new proposals, depending on which arguments are present:
//
//   - anchor set, proposals non-empty: truncate WAL below anchor, then append (runtime path).
//   - anchor set, proposals empty:     truncate only, no appends (startup prune path).
//   - anchor empty, proposals non-empty: append only, no truncation.
//   - anchor empty, proposals empty:     no-op.
//
// afterEach, when present, is called after each successful append. It is
// invoked while the per-lane lock is held, so it must not re-enter the
// persister.
// No-op persister (dir=None): skips disk I/O but still invokes afterEach.
// Does not spawn goroutines — the caller schedules parallelism per lane.
//
// The per-lane lock is held for the entire truncate-then-append sequence,
// so concurrent calls on the same lane serialize correctly.
func (bp *BlockPersister) MaybePruneAndPersistLane(
	lane types.LaneID,
	anchor utils.Option[*types.CommitQC],
	proposals []*types.Signed[*types.LaneProposal],
	afterEach utils.Option[func(*types.Signed[*types.LaneProposal])],
) error {
	if _, ok := bp.dir.Get(); !ok {
		if fn, ok := afterEach.Get(); ok {
			for _, p := range proposals {
				fn(p)
			}
		}
		return nil
	}

	lw, err := bp.getOrCreateLane(lane)
	if err != nil {
		return err
	}
	return lw.maybePruneAndPersist(lane, anchor, proposals, afterEach)
}

// close shuts down all per-lane WALs. Internal: only used by tests and
// NewBlockPersister (error cleanup). Production code does not close WALs
// at shutdown — the OS reclaims resources on process exit.
// Safe for concurrent use.
func (bp *BlockPersister) close() error {
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
