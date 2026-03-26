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
type LoadedGlobalBlock struct {
	Number types.GlobalBlockNumber
	Block  *types.Block
}

// numberedBlockEntry pairs a GlobalBlockNumber with a Block for WAL storage.
// Block doesn't carry its GlobalBlockNumber (that's assigned by the ordering
// layer), so we embed it in each WAL entry to make entries self-describing.
// A bit hacky to write GlobalBlockNumber into the entry, but we will have
// real storage solutions soon.
type numberedBlockEntry struct {
	Number types.GlobalBlockNumber
	Block  *types.Block
}

// numberedBlockCodec serializes numberedBlockEntry as [8-byte LE number][proto block].
type numberedBlockCodec struct{}

func (numberedBlockCodec) Marshal(e numberedBlockEntry) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(e.Number))
	return append(buf[:], types.BlockConv.Marshal(e.Block)...)
}

func (numberedBlockCodec) Unmarshal(raw []byte) (numberedBlockEntry, error) {
	if len(raw) < 8 {
		return numberedBlockEntry{}, fmt.Errorf("global block entry too short: %d bytes", len(raw))
	}
	n := types.GlobalBlockNumber(binary.LittleEndian.Uint64(raw[:8]))
	block, err := types.BlockConv.Unmarshal(raw[8:])
	if err != nil {
		return numberedBlockEntry{}, fmt.Errorf("unmarshal block %d: %w", n, err)
	}
	return numberedBlockEntry{Number: n, Block: block}, nil
}

// globalBlockState is the mutable state protected by GlobalBlockPersister's mutex.
type globalBlockState struct {
	iw   utils.Option[*indexedWAL[numberedBlockEntry]]
	next types.GlobalBlockNumber
}

func (s *globalBlockState) persistBlock(n types.GlobalBlockNumber, block *types.Block) error {
	if n < s.next {
		return nil
	}
	if n > s.next {
		return fmt.Errorf("global block %d out of sequence (next=%d)", n, s.next)
	}
	if iw, ok := s.iw.Get(); ok {
		if err := iw.Write(numberedBlockEntry{Number: n, Block: block}); err != nil {
			return fmt.Errorf("persist global block %d: %w", n, err)
		}
	}
	s.next = n + 1
	return nil
}

func (s *globalBlockState) truncateBefore(n types.GlobalBlockNumber) error {
	iw, ok := s.iw.Get()
	if !ok || n == 0 {
		return nil
	}
	if n >= s.next {
		s.next = n
		if iw.Count() > 0 {
			if err := iw.TruncateAll(); err != nil {
				return err
			}
		}
		return nil
	}
	if iw.Count() == 0 {
		return nil
	}
	firstGlobal := s.next - types.GlobalBlockNumber(iw.Count())
	if n <= firstGlobal {
		return nil
	}
	walIdx := iw.FirstIdx() + uint64(n-firstGlobal)
	if err := iw.TruncateBefore(walIdx, func(entry numberedBlockEntry) error {
		if entry.Number != n {
			return fmt.Errorf("global block at WAL index %d has number %d, expected %d (index mapping broken)", walIdx, entry.Number, n)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("truncate global block WAL before %d: %w", n, err)
	}
	return nil
}

// GlobalBlockPersister manages persistence of globally-ordered blocks using a WAL.
// Each entry embeds its GlobalBlockNumber since Block doesn't carry it.
// When stateDir is None, all disk I/O is skipped (no-op mode).
// All public methods are safe for concurrent use.
type GlobalBlockPersister struct {
	state        utils.Mutex[*globalBlockState]
	loadedBlocks []LoadedGlobalBlock // set once at construction, cleared after first read
}

// NewGlobalBlockPersister opens (or creates) a WAL in the globalblocks/ subdir
// and replays all persisted entries. Loaded blocks are available via
// LoadedBlocks() (one-shot). When stateDir is None, returns a no-op persister.
func NewGlobalBlockPersister(stateDir utils.Option[string]) (*GlobalBlockPersister, error) {
	sd, ok := stateDir.Get()
	if !ok {
		return &GlobalBlockPersister{state: utils.NewMutex(&globalBlockState{})}, nil
	}
	dir := filepath.Join(sd, globalBlocksDir)
	iw, err := openIndexedWAL(dir, numberedBlockCodec{})
	if err != nil {
		return nil, fmt.Errorf("open global block WAL in %s: %w", dir, err)
	}

	s := &globalBlockState{iw: utils.Some(iw)}
	loaded, err := loadAllGlobalBlocks(s)
	if err != nil {
		_ = iw.Close()
		return nil, err
	}
	if len(loaded) > 0 {
		s.next = loaded[len(loaded)-1].Number + 1
	}
	return &GlobalBlockPersister{
		state:        utils.NewMutex(s),
		loadedBlocks: loaded,
	}, nil
}

// LoadedBlocks returns blocks replayed from the WAL at startup and clears
// them from memory. Only meaningful on the first call after construction.
func (gp *GlobalBlockPersister) LoadedBlocks() []LoadedGlobalBlock {
	loaded := gp.loadedBlocks
	gp.loadedBlocks = nil
	return loaded
}

// LoadNext returns the next GlobalBlockNumber expected by the persister.
func (gp *GlobalBlockPersister) LoadNext() types.GlobalBlockNumber {
	for s := range gp.state.Lock() {
		return s.next
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
		s.iw = utils.None[*indexedWAL[numberedBlockEntry]]()
		return iw.Close()
	}
	panic("unreachable")
}

func loadAllGlobalBlocks(s *globalBlockState) ([]LoadedGlobalBlock, error) {
	iw, ok := s.iw.Get()
	if !ok {
		return nil, nil
	}
	entries, err := iw.ReadAll()
	if err != nil {
		return nil, err
	}
	loaded := make([]LoadedGlobalBlock, 0, len(entries))
	for i, entry := range entries {
		if i > 0 && entry.Number != loaded[i-1].Number+1 {
			return nil, fmt.Errorf("gap in global blocks: number %d follows %d", entry.Number, loaded[i-1].Number)
		}
		loaded = append(loaded, LoadedGlobalBlock{Number: entry.Number, Block: entry.Block})
	}
	return loaded, nil
}
