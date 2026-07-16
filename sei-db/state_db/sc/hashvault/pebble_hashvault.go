package hashvault

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/cockroachdb/pebble/v2"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/sei-protocol/seilog"
)

var _ HashVault = (*PebbleHashVault)(nil)

var logger = seilog.NewLogger("db", "state-db", "sc", "hashvault")

// PebbleHashVault is a PebbleDB-backed implementation of the HashVault interface.
type PebbleHashVault struct {
	config    HashVaultConfig
	db        *pebble.DB
	writeOpts *pebble.WriteOptions

	mu sync.Mutex
	// closed is true after Close. Every other public method returns ErrClosed once set.
	closed bool
	// pruneBoundary is the lowest height that may still be committed.
	pruneBoundary uint64
	cache         *lru.Cache[uint64, []byte]
}

// NewPebbleHashVault opens (or creates) a PebbleHashVault rooted at config.DataDir.
func NewPebbleHashVault(ctx context.Context, config HashVaultConfig) (*PebbleHashVault, error) {
	if !config.Fsync {
		logger.Info("forcing fsync on for production PebbleHashVault", "dataDir", config.DataDir)
	}
	config.Fsync = true
	return newPebbleHashVault(ctx, config)
}

// NewUnsafePebbleHashVault opens (or creates) a PebbleHashVault rooted at config.DataDir. Honors
// config.Fsync as set; intended for tests only. Never use in production: disabling fsync means a
// well-timed crash can lose the most recent committed hash and let the node vote a different hash
// for that block on the next boot.
func NewUnsafePebbleHashVault(ctx context.Context, config HashVaultConfig) (*PebbleHashVault, error) {
	return newPebbleHashVault(ctx, config)
}

func newPebbleHashVault(_ context.Context, config HashVaultConfig) (*PebbleHashVault, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid hashvault config: %w", err)
	}

	if err := os.MkdirAll(config.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create hashvault data dir %q: %w", config.DataDir, err)
	}

	db, err := pebble.Open(config.DataDir, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open hashvault pebble db at %q: %w", config.DataDir, err)
	}

	writeOpts := pebble.Sync
	if !config.Fsync {
		writeOpts = pebble.NoSync
	}

	p := &PebbleHashVault{
		config:    config,
		db:        db,
		writeOpts: writeOpts,
		cache:     lru.NewCache[uint64, []byte](config.CacheSize),
	}

	if err := p.loadPruneBoundary(); err != nil {
		_ = db.Close()
		return nil, err
	}

	empty, err := p.isEmpty()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if empty {
		// Surface the fresh-start case: an operator who expected this node to already have an
		// equivocation history on disk (e.g. after a restart) should notice an empty vault.
		logger.Info("opened hashvault with no data on disk; starting with an empty equivocation history",
			"dataDir", config.DataDir)
	}

	return p, nil
}

// isEmpty reports whether the underlying DB holds no keys at all (a freshly created vault with no
// committed hashes and no prune boundary).
func (p *PebbleHashVault) isEmpty() (bool, error) {
	iter, err := p.db.NewIter(nil)
	if err != nil {
		return false, fmt.Errorf("failed to open hashvault iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()
	return !iter.First(), nil
}

// loadPruneBoundary reads the on-disk prune boundary (if any) and populates p.pruneBoundary.
func (p *PebbleHashVault) loadPruneBoundary() error {
	raw, closer, err := p.db.Get(pruneBoundaryKey)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("failed to read prune boundary: %w", err)
	}
	defer func() { _ = closer.Close() }()

	boundary, err := decodeBoundaryValue(raw)
	if err != nil {
		logger.Error("hashvault prune boundary is malformed; refusing to start",
			"dataDir", p.config.DataDir, "rawHex", hex.EncodeToString(raw), "err", err)
		return err
	}
	p.pruneBoundary = boundary
	return nil
}

// CommitToHash implements HashVault.
func (p *PebbleHashVault) CommitToHash(ctx context.Context, blockHeight uint64, hash []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrClosed
	}
	if blockHeight < p.pruneBoundary {
		return ErrBelowPruneBoundary
	}
	if len(hash) != BlockHashSize {
		return ErrInvalidHashLength
	}

	if cached, ok := p.cache.Get(blockHeight); ok {
		if !bytes.Equal(cached, hash) {
			p.logHashMismatch(blockHeight, cached, hash)
			return ErrHashMismatch
		}
		return nil
	}

	key := hashKey(blockHeight)
	raw, closer, err := p.db.Get(key)
	switch {
	case errors.Is(err, pebble.ErrNotFound):
		// First commit for this height: write it.
		value := encodeHashValue(blockHeight, hash)
		if werr := p.db.Set(key, value, p.writeOpts); werr != nil {
			return fmt.Errorf("failed to persist hash for block %d: %w", blockHeight, werr)
		}
		p.cache.Add(blockHeight, bytes.Clone(hash))
		return nil
	case err != nil:
		return fmt.Errorf("failed to read hash for block %d: %w", blockHeight, err)
	}
	// Found an existing entry; clone the raw bytes so we can release the closer before doing
	// further work.
	cloned := bytes.Clone(raw)
	_ = closer.Close()

	existing, err := decodeHashValue(blockHeight, cloned)
	if err != nil {
		logger.Error("hashvault detected on-disk corruption; DO NOT RESTART WITHOUT HUMAN INVESTIGATION",
			"blockHeight", blockHeight, "rawHex", hex.EncodeToString(cloned), "err", err)
		return err
	}
	if !bytes.Equal(existing, hash) {
		p.logHashMismatch(blockHeight, existing, hash)
		return ErrHashMismatch
	}
	p.cache.Add(blockHeight, existing)
	return nil
}

// Prune implements HashVault. The boundary advance and range deletion are written in a single
// atomic Pebble batch: a crash mid-Prune either rolls forward to the new boundary (with the
// deletions applied) or leaves the old state intact. On return, every height strictly below
// blockHeight is guaranteed durable-deleted (subject to config.Fsync).
func (p *PebbleHashVault) Prune(ctx context.Context, blockHeight uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return ErrClosed
	}
	if blockHeight <= p.pruneBoundary {
		return nil
	}

	batch := p.db.NewBatch()
	defer func() { _ = batch.Close() }()
	if err := batch.Set(pruneBoundaryKey, encodeBoundaryValue(blockHeight), nil); err != nil {
		return fmt.Errorf("failed to stage prune boundary advance to %d: %w", blockHeight, err)
	}
	// DeleteRange's upper bound is exclusive, so hashKey(blockHeight) keeps the boundary block
	// itself per the HashVault.Prune contract.
	if err := batch.DeleteRange(hashKey(0), hashKey(blockHeight), nil); err != nil {
		return fmt.Errorf("failed to stage prune deletion below %d: %w", blockHeight, err)
	}
	if err := batch.Commit(p.writeOpts); err != nil {
		return fmt.Errorf("failed to commit prune to %d: %w", blockHeight, err)
	}

	p.pruneBoundary = blockHeight
	return nil
}

// Close implements HashVault. Subsequent calls return nil. After Close, every other public method
// returns ErrClosed.
func (p *PebbleHashVault) Close(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	p.cache.Purge()
	if err := p.db.Close(); err != nil {
		return fmt.Errorf("failed to close hashvault pebble db: %w", err)
	}
	return nil
}

func (p *PebbleHashVault) logHashMismatch(blockHeight uint64, existing, incoming []byte) {
	logger.Error("Hashvault detected app hash mismatch; node attempted to change its mind. "+
		"DO NOT RESTART WITHOUT HUMAN INVESTIGATION. If you are CERTAIN this is not a real "+
		"equivocation, you can bypass this guard by stopping the node and deleting the HashVault "+
		"data directory (hashVaultDir below), then restarting. WARNING: deleting it removes "+
		"equivocation protection — if the node then commits a conflicting hash for a height it has "+
		"already finalized, the validator may be SLASHED.",
		"blockHeight", blockHeight,
		"existingHex", hex.EncodeToString(existing),
		"incomingHex", hex.EncodeToString(incoming),
		"hashVaultDir", p.config.DataDir,
	)
}
