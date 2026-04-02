package pebbleblockdb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/cockroachdb/pebble/v2/sstable"

	blockdb "github.com/sei-protocol/sei-chain/sei-db/block_db"
)

const cmdChannelSize = 256

// pebbleBlockDB implements blockdb.BlockDB backed by PebbleDB.
type pebbleBlockDB struct {
	db     *pebble.DB
	cmdCh  chan command
	writer *writer
	cache  *pendingCache

	// In-memory tracking for lo/hi metadata to avoid unnecessary writes.
	hasBlocks atomic.Bool
	loHeight  atomic.Uint64
	hiHeight  atomic.Uint64
}

var _ blockdb.BlockDB = (*pebbleBlockDB)(nil)

// Open creates or opens a PebbleDB-backed BlockDB at the given path.
func Open(_ context.Context, path string) (*pebbleBlockDB, error) {
	cache := pebble.NewCache(512 << 20) // 512 MB
	defer cache.Unref()

	opts := &pebble.Options{
		Cache:                       cache,
		FormatMajorVersion:          pebble.FormatVirtualSSTables,
		L0CompactionThreshold:       4,
		L0StopWritesThreshold:       1000,
		LBaseMaxBytes:               64 << 20,
		MemTableSize:                64 << 20,
		MemTableStopWritesThreshold: 4,
		DisableWAL:                  false,
	}

	opts.Levels[0].BlockSize = 32 << 10
	opts.Levels[0].IndexBlockSize = 256 << 10
	opts.Levels[0].FilterPolicy = bloom.FilterPolicy(10)
	opts.Levels[0].FilterType = pebble.TableFilter
	opts.Levels[0].Compression = func() *sstable.CompressionProfile { return sstable.ZstdCompression }
	opts.Levels[0].EnsureL0Defaults()

	for i := 1; i < len(opts.Levels); i++ {
		l := &opts.Levels[i]
		l.BlockSize = 32 << 10
		l.IndexBlockSize = 256 << 10
		l.FilterPolicy = bloom.FilterPolicy(10)
		l.FilterType = pebble.TableFilter
		l.Compression = func() *sstable.CompressionProfile { return sstable.ZstdCompression }
		l.EnsureL1PlusDefaults(&opts.Levels[i-1])
	}
	opts.Levels[6].FilterPolicy = nil

	db, err := pebble.Open(path, opts)
	if err != nil {
		return nil, fmt.Errorf("pebble open: %w", err)
	}

	pc := newPendingCache()
	cmdCh := make(chan command, cmdChannelSize)

	pdb := &pebbleBlockDB{
		db:    db,
		cmdCh: cmdCh,
		cache: pc,
	}

	// Load existing metadata if reopening.
	if lo, ok := pdb.readMeta(metaKeyLo()); ok {
		if hi, ok2 := pdb.readMeta(metaKeyHi()); ok2 {
			pdb.hasBlocks.Store(true)
			pdb.loHeight.Store(lo)
			pdb.hiHeight.Store(hi)
		}
	}

	pdb.writer = newWriter(db, cmdCh, pc)
	return pdb, nil
}

func (p *pebbleBlockDB) readMeta(key []byte) (uint64, bool) {
	val, closer, err := p.db.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, false
		}
		panic(fmt.Sprintf("pebble read meta: %v", err))
	}
	h := decodeHeightValue(val)
	closer.Close()
	return h, true
}

// WriteBlock serializes the block, caches it for read-your-writes, and
// queues the KV mutations to the background writer. Returns immediately.
func (p *pebbleBlockDB) WriteBlock(_ context.Context, block *blockdb.BinaryBlock) error {
	ops := p.buildBlockOps(block)
	p.cache.put(block)
	p.cmdCh <- command{kind: cmdWrite, ops: ops}
	return nil
}

func (p *pebbleBlockDB) Flush(_ context.Context) error {
	done := make(chan error, 1)
	p.cmdCh <- command{kind: cmdFlush, done: done}
	return <-done
}

func (p *pebbleBlockDB) GetBlockByHeight(_ context.Context, height uint64) (*blockdb.BinaryBlock, bool, error) {
	if blk, ok := p.cache.getByHeight(height); ok {
		return blk, true, nil
	}
	return p.getBlockByHeightFromDB(height)
}

func (p *pebbleBlockDB) GetBlockByHash(_ context.Context, hash []byte) (*blockdb.BinaryBlock, bool, error) {
	if blk, ok := p.cache.getByHash(hash); ok {
		return blk, true, nil
	}

	val, closer, err := p.db.Get(encodeHashIdxKey(hash))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	height := decodeHeightValue(val)
	closer.Close()

	blk, ok, err := p.getBlockByHeightFromDB(height)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	return blk, true, nil
}

func (p *pebbleBlockDB) GetTransactionByHash(
	_ context.Context,
	txHash []byte,
) (*blockdb.BinaryTransaction, bool, error) {
	if tx, ok := p.cache.getTxByHash(txHash); ok {
		return tx, true, nil
	}

	val, closer, err := p.db.Get(encodeTxIdxKey(txHash))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	height, txIndex := decodeTxIdxValue(val)
	closer.Close()

	txData, txCloser, err := p.db.Get(encodeTxDataKey(height, txIndex))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	data := bytes.Clone(txData)
	txCloser.Close()

	return &blockdb.BinaryTransaction{
		Hash:        bytes.Clone(txHash),
		Transaction: data,
	}, true, nil
}

func (p *pebbleBlockDB) Prune(_ context.Context, lowestHeightToKeep uint64) error {
	p.cmdCh <- command{kind: cmdPrune, pruneKeepHeight: lowestHeightToKeep}
	return nil
}

func (p *pebbleBlockDB) Close(_ context.Context) error {
	done := make(chan error, 1)
	p.cmdCh <- command{kind: cmdClose, done: done}
	<-done

	// Wait for the writer goroutine to exit.
	<-p.writer.stopped

	return p.db.Close()
}

// --- internal helpers ---

func (p *pebbleBlockDB) buildBlockOps(block *blockdb.BinaryBlock) *blockOps {
	txHashes := make([][]byte, len(block.Transactions))
	for i, tx := range block.Transactions {
		txHashes[i] = tx.Hash
	}

	headerVal := marshalBlockHeader(block.Hash, block.BlockData, txHashes)

	// 1 block header + N tx data + 1 hash index + N tx indices + up to 2 metadata = 2N+4 max
	entries := make([]kvEntry, 0, 2*len(block.Transactions)+4)

	entries = append(entries, kvEntry{
		key:   encodeBlockKey(block.Height),
		value: headerVal,
	})

	for i, tx := range block.Transactions {
		entries = append(entries, kvEntry{
			key:   encodeTxDataKey(block.Height, uint32(i)),
			value: tx.Transaction,
		})
	}

	entries = append(entries, kvEntry{
		key:   encodeHashIdxKey(block.Hash),
		value: encodeHeightValue(block.Height),
	})

	for i, tx := range block.Transactions {
		entries = append(entries, kvEntry{
			key:   encodeTxIdxKey(tx.Hash),
			value: encodeTxIdxValue(block.Height, uint32(i)),
		})
	}

	// Update hi if this block advances it.
	for {
		old := p.hiHeight.Load()
		if p.hasBlocks.Load() && block.Height <= old {
			break
		}
		if p.hiHeight.CompareAndSwap(old, block.Height) {
			entries = append(entries,
				kvEntry{key: metaKeyHi(), value: encodeHeightValue(block.Height)},
			)
			break
		}
	}

	// Update lo if this is the first block or a new minimum.
	if !p.hasBlocks.Load() {
		p.loHeight.Store(block.Height)
		p.hasBlocks.Store(true)
		entries = append(entries,
			kvEntry{key: metaKeyLo(), value: encodeHeightValue(block.Height)},
		)
	} else {
		for {
			old := p.loHeight.Load()
			if block.Height >= old {
				break
			}
			if p.loHeight.CompareAndSwap(old, block.Height) {
				entries = append(entries,
					kvEntry{key: metaKeyLo(), value: encodeHeightValue(block.Height)},
				)
				break
			}
		}
	}

	return &blockOps{height: block.Height, entries: entries}
}

func (p *pebbleBlockDB) getBlockByHeightFromDB(height uint64) (*blockdb.BinaryBlock, bool, error) {
	val, closer, err := p.db.Get(encodeBlockKey(height))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	hdr, err := unmarshalBlockHeader(val)
	closer.Close()
	if err != nil {
		return nil, false, fmt.Errorf("unmarshal block header at height %d: %w", height, err)
	}

	txs := make([]*blockdb.BinaryTransaction, len(hdr.txHashes))
	for i, txHash := range hdr.txHashes {
		txData, txCloser, getErr := p.db.Get(encodeTxDataKey(height, uint32(i)))
		if getErr != nil {
			if errors.Is(getErr, pebble.ErrNotFound) {
				return nil, false, fmt.Errorf("tx data missing at height %d index %d", height, i)
			}
			return nil, false, getErr
		}
		txs[i] = &blockdb.BinaryTransaction{
			Hash:        bytes.Clone(txHash),
			Transaction: bytes.Clone(txData),
		}
		txCloser.Close()
	}

	return &blockdb.BinaryBlock{
		Height:       height,
		Hash:         hdr.hash,
		BlockData:    hdr.data,
		Transactions: txs,
	}, true, nil
}

// --- pending cache (read-your-writes overlay) ---

type pendingCache struct {
	mu           sync.RWMutex
	byHeight     map[uint64]*blockdb.BinaryBlock
	hashToHeight map[string]uint64
	txByHash     map[string]*blockdb.BinaryTransaction
}

func newPendingCache() *pendingCache {
	return &pendingCache{
		byHeight:     make(map[uint64]*blockdb.BinaryBlock),
		hashToHeight: make(map[string]uint64),
		txByHash:     make(map[string]*blockdb.BinaryTransaction),
	}
}

func (c *pendingCache) put(block *blockdb.BinaryBlock) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byHeight[block.Height] = block
	c.hashToHeight[string(block.Hash)] = block.Height
	for _, tx := range block.Transactions {
		c.txByHash[string(tx.Hash)] = tx
	}
}

func (c *pendingCache) getByHeight(height uint64) (*blockdb.BinaryBlock, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	blk, ok := c.byHeight[height]
	return blk, ok
}

func (c *pendingCache) getByHash(hash []byte) (*blockdb.BinaryBlock, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	h, ok := c.hashToHeight[string(hash)]
	if !ok {
		return nil, false
	}
	blk, ok := c.byHeight[h]
	return blk, ok
}

func (c *pendingCache) getTxByHash(txHash []byte) (*blockdb.BinaryTransaction, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tx, ok := c.txByHash[string(txHash)]
	return tx, ok
}

func (c *pendingCache) evict(heights []uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, h := range heights {
		blk, ok := c.byHeight[h]
		if !ok {
			continue
		}
		delete(c.byHeight, h)
		delete(c.hashToHeight, string(blk.Hash))
		for _, tx := range blk.Transactions {
			delete(c.txByHash, string(tx.Hash))
		}
	}
}
