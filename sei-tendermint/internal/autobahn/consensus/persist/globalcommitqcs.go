package persist

import (
	"fmt"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const globalCommitQCsDir = "globalcommitqcs"

// globalCommitQCState is the mutable state protected by GlobalCommitQCPersister's mutex.
type globalCommitQCState struct {
	iw   utils.Option[*indexedWAL[*types.FullCommitQC]]
	next types.GlobalBlockNumber // next expected GlobalRange().First == last QC's GlobalRange().Next
}

func (s *globalCommitQCState) persistQC(qc *types.FullCommitQC) error {
	gr := qc.QC().GlobalRange()
	if gr.First < s.next {
		return nil
	}
	if gr.First > s.next {
		return fmt.Errorf("global commitqc first=%d out of sequence (next=%d)", gr.First, s.next)
	}
	if iw, ok := s.iw.Get(); ok {
		if err := iw.Write(qc); err != nil {
			return fmt.Errorf("persist global commitqc at %d: %w", gr.First, err)
		}
	}
	s.next = gr.Next
	return nil
}

func (s *globalCommitQCState) truncateBefore(n types.GlobalBlockNumber) error {
	if n == 0 {
		return nil
	}
	iw, ok := s.iw.Get()
	if n >= s.next {
		s.next = n
		if ok && iw.Count() > 0 {
			if err := iw.TruncateAll(); err != nil {
				return err
			}
		}
		return nil
	}
	if !ok || iw.Count() == 0 {
		return nil
	}
	// Scan from the front to find the first QC entry to keep.
	// A QC is kept if its range extends past n (GlobalRange().Next > n).
	// In practice the scan visits 0–1 entries per prune call because
	// pruning advances one block at a time while each QC covers many blocks.
	keepWalIdx := iw.FirstIdx()
	for keepWalIdx < iw.FirstIdx()+iw.Count() {
		entry, err := iw.ReadAt(keepWalIdx)
		if err != nil {
			return fmt.Errorf("read commitqc WAL at %d: %w", keepWalIdx, err)
		}
		if entry.QC().GlobalRange().Next > n {
			break
		}
		keepWalIdx++
	}
	if keepWalIdx > iw.FirstIdx() {
		if err := iw.TruncateBefore(keepWalIdx, func(*types.FullCommitQC) error {
			return nil // already validated during scan
		}); err != nil {
			return fmt.Errorf("truncate global commitqc WAL before %d: %w", keepWalIdx, err)
		}
	}
	return nil
}

// GlobalCommitQCPersister manages persistence of FullCommitQCs using a WAL.
// Each entry is one FullCommitQC covering a range of global block numbers.
// When stateDir is None, all disk I/O is skipped (no-op mode).
// All public methods are safe for concurrent use.
type GlobalCommitQCPersister struct {
	state     utils.Mutex[*globalCommitQCState]
	loadedQCs []*types.FullCommitQC // set once at construction, cleared after first read
}

// NewGlobalCommitQCPersister opens (or creates) a WAL in the globalcommitqcs/
// subdir and replays all persisted entries. Loaded QCs are available via
// LoadedQCs() (one-shot). When stateDir is None, returns a no-op persister.
func NewGlobalCommitQCPersister(stateDir utils.Option[string]) (*GlobalCommitQCPersister, error) {
	sd, ok := stateDir.Get()
	if !ok {
		return &GlobalCommitQCPersister{state: utils.NewMutex(&globalCommitQCState{})}, nil
	}
	dir := filepath.Join(sd, globalCommitQCsDir)
	iw, err := openIndexedWAL(dir, types.FullCommitQCConv)
	if err != nil {
		return nil, fmt.Errorf("open global commitqc WAL in %s: %w", dir, err)
	}

	s := &globalCommitQCState{iw: utils.Some(iw)}
	loaded, err := loadAllGlobalCommitQCs(s)
	if err != nil {
		_ = iw.Close()
		return nil, err
	}
	if len(loaded) > 0 {
		s.next = loaded[len(loaded)-1].QC().GlobalRange().Next
	}
	return &GlobalCommitQCPersister{
		state:     utils.NewMutex(s),
		loadedQCs: loaded,
	}, nil
}

// LoadedQCs returns QCs replayed from the WAL at startup and clears
// them from memory. Only meaningful on the first call after construction.
func (gp *GlobalCommitQCPersister) LoadedQCs() []*types.FullCommitQC {
	loaded := gp.loadedQCs
	gp.loadedQCs = nil
	return loaded
}

// LoadNext returns the next GlobalBlockNumber expected by the persister
// (i.e., the next QC's GlobalRange().First).
func (gp *GlobalCommitQCPersister) LoadNext() types.GlobalBlockNumber {
	for s := range gp.state.Lock() {
		return s.next
	}
	panic("unreachable")
}

// PersistQC appends a FullCommitQC to the WAL. Duplicates are silently ignored.
// Gaps return an error.
func (gp *GlobalCommitQCPersister) PersistQC(qc *types.FullCommitQC) error {
	for s := range gp.state.Lock() {
		return s.persistQC(qc)
	}
	panic("unreachable")
}

// TruncateBefore removes all QC entries whose range is fully before n.
func (gp *GlobalCommitQCPersister) TruncateBefore(n types.GlobalBlockNumber) error {
	for s := range gp.state.Lock() {
		return s.truncateBefore(n)
	}
	panic("unreachable")
}

// Close shuts down the WAL.
func (gp *GlobalCommitQCPersister) Close() error {
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

func loadAllGlobalCommitQCs(s *globalCommitQCState) ([]*types.FullCommitQC, error) {
	iw, ok := s.iw.Get()
	if !ok {
		return nil, nil
	}
	entries, err := iw.ReadAll()
	if err != nil {
		return nil, err
	}
	for i, entry := range entries {
		if i > 0 {
			gr := entry.QC().GlobalRange()
			prevNext := entries[i-1].QC().GlobalRange().Next
			if gr.First != prevNext {
				return nil, fmt.Errorf("gap in global commitqcs: first=%d follows previous next=%d",
					gr.First, prevNext)
			}
		}
	}
	return entries, nil
}
