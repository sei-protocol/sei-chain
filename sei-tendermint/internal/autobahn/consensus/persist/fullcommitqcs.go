package persist

import (
	"fmt"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const fullCommitQCsDir = "fullcommitqcs"

// fullCommitQCState is the mutable state protected by FullCommitQCPersister's mutex.
type fullCommitQCState struct {
	iw        utils.Option[*indexedWAL[*types.FullCommitQC]]
	committee *types.Committee
	next      types.GlobalBlockNumber // next expected GlobalRange().First == last QC's GlobalRange().Next
	loaded    []*types.FullCommitQC
}

func (s *fullCommitQCState) persistQC(qc *types.FullCommitQC) error {
	gr := qc.QC().GlobalRange(s.committee)
	if gr.First < s.next {
		return nil
	}
	if gr.First > s.next {
		return fmt.Errorf("full commitqc %d out of sequence (next=%d)", gr.First, s.next)
	}
	if iw, ok := s.iw.Get(); ok {
		if err := iw.Write(qc); err != nil {
			return fmt.Errorf("persist full commitqc at %d: %w", gr.First, err)
		}
	}
	s.next = gr.Next
	return nil
}

func (s *fullCommitQCState) truncateBefore(n types.GlobalBlockNumber) error {
	if n == 0 {
		return nil
	}
	if n > s.next {
		s.next = n
	}
	iw, ok := s.iw.Get()
	if !ok || iw.Count() == 0 {
		return nil
	}
	// Remove QCs whose range is fully before n. A QC is stale when
	// GlobalRange().Next <= n. In practice the scan visits 0–1 entries
	// per prune call because pruning advances one block at a time while
	// each QC covers many blocks.
	if err := iw.TruncateWhile(func(entry *types.FullCommitQC) bool {
		return entry.QC().GlobalRange(s.committee).Next <= n
	}); err != nil {
		return fmt.Errorf("truncate full commitqc WAL: %w", err)
	}
	return nil
}

// FullCommitQCPersister manages persistence of FullCommitQCs using a WAL.
// Each entry is one FullCommitQC covering a range of global block numbers.
// When stateDir is None, all disk I/O is skipped (no-op mode).
// All public methods are safe for concurrent use.
type FullCommitQCPersister struct {
	state utils.Mutex[*fullCommitQCState]
}

// NewFullCommitQCPersister opens (or creates) a WAL in the fullcommitqcs/
// subdir and replays all persisted entries. Loaded QCs are available via
// ConsumeLoaded. When stateDir is None, returns a no-op persister.
func NewFullCommitQCPersister(stateDir utils.Option[string], committee *types.Committee) (*FullCommitQCPersister, error) {
	sd, ok := stateDir.Get()
	if !ok {
		return &FullCommitQCPersister{state: utils.NewMutex(&fullCommitQCState{committee: committee, next: committee.FirstBlock()})}, nil
	}
	dir := filepath.Join(sd, fullCommitQCsDir)
	iw, err := openIndexedWAL(dir, types.FullCommitQCConv)
	if err != nil {
		return nil, fmt.Errorf("open full commitqc WAL in %s: %w", dir, err)
	}

	s := &fullCommitQCState{iw: utils.Some(iw), committee: committee, next: committee.FirstBlock()}
	loaded, err := s.loadAll()
	if err != nil {
		_ = iw.Close()
		return nil, err
	}
	if len(loaded) > 0 {
		s.next = loaded[len(loaded)-1].QC().GlobalRange(s.committee).Next
	}
	s.loaded = loaded
	return &FullCommitQCPersister{
		state: utils.NewMutex(s),
	}, nil
}

// Next returns the next GlobalBlockNumber expected by the persister
// (i.e., the next QC's GlobalRange().First).
func (gp *FullCommitQCPersister) Next() types.GlobalBlockNumber {
	for s := range gp.state.Lock() {
		return s.next
	}
	panic("unreachable")
}

// LoadedFirst returns the first global block number of the first loaded QC,
// or committee.FirstBlock() if empty.
func (gp *FullCommitQCPersister) LoadedFirst() types.GlobalBlockNumber {
	for s := range gp.state.Lock() {
		if len(s.loaded) > 0 {
			return s.loaded[0].QC().GlobalRange(s.committee).First
		}
		return s.committee.FirstBlock()
	}
	panic("unreachable")
}

// ConsumeLoaded returns QCs loaded from the WAL during construction
// and nils the internal slice so the data is not retained.
func (gp *FullCommitQCPersister) ConsumeLoaded() []*types.FullCommitQC {
	for s := range gp.state.Lock() {
		loaded := s.loaded
		s.loaded = nil
		return loaded
	}
	panic("unreachable")
}

// PersistQC appends a FullCommitQC to the WAL. Duplicates are silently ignored.
// Gaps return an error.
func (gp *FullCommitQCPersister) PersistQC(qc *types.FullCommitQC) error {
	for s := range gp.state.Lock() {
		return s.persistQC(qc)
	}
	panic("unreachable")
}

// TruncateBefore removes all QC entries whose range is fully before n.
func (gp *FullCommitQCPersister) TruncateBefore(n types.GlobalBlockNumber) error {
	for s := range gp.state.Lock() {
		return s.truncateBefore(n)
	}
	panic("unreachable")
}

// Close shuts down the WAL.
func (gp *FullCommitQCPersister) Close() error {
	for s := range gp.state.Lock() {
		iw, ok := s.iw.Get()
		if !ok {
			return nil
		}
		s.iw = utils.None[*indexedWAL[*types.FullCommitQC]]()
		return iw.Close()
	}
	panic("unreachable")
}

func (s *fullCommitQCState) loadAll() ([]*types.FullCommitQC, error) {
	iw, ok := s.iw.Get()
	if !ok {
		return nil, nil
	}
	return iw.ReadAll()
}
