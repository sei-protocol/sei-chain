package persist

import (
	"encoding/binary"
	"fmt"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const globalBlocksDir = "globalblocks"

// LoadedGlobalBlock is a block loaded from disk during state restoration.
// Also used as the internal WAL entry type. Block doesn't carry its
// GlobalBlockNumber (that's assigned by the ordering layer), so we embed
// it in each WAL entry to make entries self-describing.
type LoadedGlobalBlock struct {
	Number types.GlobalBlockNumber
	Block  *types.Block
}

// loadedGlobalBlockCodec serializes LoadedGlobalBlock as [8-byte LE number][proto block].
type loadedGlobalBlockCodec struct{}

func (loadedGlobalBlockCodec) Marshal(e LoadedGlobalBlock) []byte {
	proto := types.BlockConv.Marshal(e.Block)
	out := make([]byte, 8+len(proto))
	binary.LittleEndian.PutUint64(out, uint64(e.Number))
	copy(out[8:], proto)
	return out
}

func (loadedGlobalBlockCodec) Unmarshal(raw []byte) (LoadedGlobalBlock, error) {
	if len(raw) < 8 {
		return LoadedGlobalBlock{}, fmt.Errorf("global block entry too short: %d bytes", len(raw))
	}
	n := types.GlobalBlockNumber(binary.LittleEndian.Uint64(raw[:8]))
	block, err := types.BlockConv.Unmarshal(raw[8:])
	if err != nil {
		return LoadedGlobalBlock{}, fmt.Errorf("unmarshal block %d: %w", n, err)
	}
	return LoadedGlobalBlock{Number: n, Block: block}, nil
}

// globalBlockState is the mutable state protected by GlobalBlockPersister's mutex.
type globalBlockState struct {
	iw        utils.Option[*indexedWAL[LoadedGlobalBlock]]
	committee *types.Committee
	next      types.GlobalBlockNumber
	loaded    []LoadedGlobalBlock
}

func (s *globalBlockState) persistBlock(n types.GlobalBlockNumber, block *types.Block) error {
	if n < s.next {
		return nil
	}
	if n > s.next {
		return fmt.Errorf("global block %d out of sequence (next=%d)", n, s.next)
	}
	if iw, ok := s.iw.Get(); ok {
		if err := iw.Write(LoadedGlobalBlock{Number: n, Block: block}); err != nil {
			return fmt.Errorf("persist global block %d: %w", n, err)
		}
	}
	s.next = n + 1
	return nil
}

func (s *globalBlockState) truncateBefore(n types.GlobalBlockNumber) error {
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
	firstGlobal := s.next - types.GlobalBlockNumber(iw.Count())
	if n <= firstGlobal {
		return nil
	}
	walIdx := iw.FirstIdx() + uint64(n-firstGlobal)
	if err := iw.TruncateBefore(walIdx, func(entry LoadedGlobalBlock) error {
		if entry.Number != n {
			return fmt.Errorf("global block at WAL index %d has number %d, expected %d (index mapping broken)", walIdx, entry.Number, n)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("truncate global block WAL before %d: %w", n, err)
	}
	return nil
}

func (s *globalBlockState) truncateAfter(n types.GlobalBlockNumber) error {
	iw, ok := s.iw.Get()
	if !ok || iw.Count() == 0 {
		return nil
	}
	if n+1 >= s.next {
		return nil
	}
	firstGlobal := s.next - types.GlobalBlockNumber(iw.Count())
	if n < firstGlobal {
		if err := iw.TruncateAll(); err != nil {
			return err
		}
	} else {
		walIdx := iw.FirstIdx() + uint64(n-firstGlobal)
		if err := iw.TruncateAfter(walIdx); err != nil {
			return fmt.Errorf("truncate global block WAL after %d: %w", n, err)
		}
	}
	s.next = firstGlobal + types.GlobalBlockNumber(iw.Count())
	return nil
}

// GlobalBlockPersister manages persistence of globally-ordered blocks using a WAL.
// Each entry embeds its GlobalBlockNumber since Block doesn't carry it.
// When stateDir is None, all disk I/O is skipped (no-op mode).
// All public methods are safe for concurrent use.
type GlobalBlockPersister struct {
	state utils.Mutex[*globalBlockState]
}

// NewGlobalBlockPersister opens (or creates) a WAL in the globalblocks/ subdir
// and replays all persisted entries. Loaded blocks are available via
// ConsumeLoaded. When stateDir is None, returns a no-op persister.
func NewGlobalBlockPersister(stateDir utils.Option[string], committee *types.Committee) (*GlobalBlockPersister, error) {
	sd, ok := stateDir.Get()
	if !ok {
		return &GlobalBlockPersister{state: utils.NewMutex(&globalBlockState{committee: committee, next: committee.FirstBlock()})}, nil
	}
	dir := filepath.Join(sd, globalBlocksDir)
	iw, err := openIndexedWAL(dir, loadedGlobalBlockCodec{})
	if err != nil {
		return nil, fmt.Errorf("open global block WAL in %s: %w", dir, err)
	}

	s := &globalBlockState{iw: utils.Some(iw), committee: committee, next: committee.FirstBlock()}
	// TODO: avoid loading all blocks on startup; cache only the last N blocks
	// (e.g. 1000) in memory instead.
	loaded, err := s.loadAll()
	if err != nil {
		_ = iw.Close()
		return nil, err
	}
	if len(loaded) > 0 {
		s.next = loaded[len(loaded)-1].Number + 1
	}
	s.loaded = loaded
	return &GlobalBlockPersister{
		state: utils.NewMutex(s),
	}, nil
}

// Next returns the next GlobalBlockNumber expected by the persister.
func (gp *GlobalBlockPersister) Next() types.GlobalBlockNumber {
	for s := range gp.state.Lock() {
		return s.next
	}
	panic("unreachable")
}

// LoadedFirst returns the first loaded block number, or committee.FirstBlock() if empty.
func (gp *GlobalBlockPersister) LoadedFirst() types.GlobalBlockNumber {
	for s := range gp.state.Lock() {
		if len(s.loaded) > 0 {
			return s.loaded[0].Number
		}
		return s.committee.FirstBlock()
	}
	panic("unreachable")
}

// ConsumeLoaded returns blocks loaded from the WAL during construction
// and nils the internal slice so the data is not retained.
func (gp *GlobalBlockPersister) ConsumeLoaded() []LoadedGlobalBlock {
	for s := range gp.state.Lock() {
		loaded := s.loaded
		s.loaded = nil
		return loaded
	}
	panic("unreachable")
}

// TruncateAfter removes all block entries after global block number n.
// Used to discard blocks that were persisted without corresponding QCs.
func (gp *GlobalBlockPersister) TruncateAfter(n types.GlobalBlockNumber) error {
	for s := range gp.state.Lock() {
		return s.truncateAfter(n)
	}
	panic("unreachable")
}

// PersistBlock appends a block to the WAL. Duplicates are silently ignored.
// Gaps return an error.
func (gp *GlobalBlockPersister) PersistBlock(n types.GlobalBlockNumber, block *types.Block) error {
	for s := range gp.state.Lock() {
		return s.persistBlock(n, block)
	}
	panic("unreachable")
}

// TruncateBefore removes all entries before n from the WAL.
func (gp *GlobalBlockPersister) TruncateBefore(n types.GlobalBlockNumber) error {
	for s := range gp.state.Lock() {
		return s.truncateBefore(n)
	}
	panic("unreachable")
}

// Close shuts down the WAL.
func (gp *GlobalBlockPersister) Close() error {
	for s := range gp.state.Lock() {
		iw, ok := s.iw.Get()
		if !ok {
			return nil
		}
		s.iw = utils.None[*indexedWAL[LoadedGlobalBlock]]()
		return iw.Close()
	}
	panic("unreachable")
}

func (s *globalBlockState) loadAll() ([]LoadedGlobalBlock, error) {
	iw, ok := s.iw.Get()
	if !ok {
		return nil, nil
	}
	return iw.ReadAll()
}
