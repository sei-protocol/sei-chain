package hashvault

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble/v2"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/sei-protocol/seilog"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
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

	// head is the newest recorded height. Meaningless when notEmpty is false.
	head uint64
	// notEmpty is true when the vault holds at least one hash.
	notEmpty bool

	// outerFloor is the floor PruneBelow has raised. Only ever rises.
	outerFloor atomic.Uint64
	// gcFloor is the floor PruneHistory has raised. Only ever rises.
	gcFloor atomic.Uint64
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

	if err := p.loadHead(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to open hashvault: %w", err)
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
	if p.notEmpty && blockHeight > p.head+1 {
		return fmt.Errorf("block %d would leave a gap after the newest recorded block %d", blockHeight, p.head)
	}

	if cached, ok := p.cache.Get(blockHeight); ok {
		if !bytes.Equal(cached, hash) {
			if !p.config.HaltOnMismatch {
				return p.acceptMismatchedHash(blockHeight, cached, hash)
			}
			p.logHashMismatch(blockHeight, cached, hash)
			return ErrHashMismatch
		}
		return nil
	}

	key := hashKey(blockHeight)
	raw, closer, err := p.db.Get(key)
	switch {
	case errors.Is(err, pebble.ErrNotFound):
		if p.notEmpty && blockHeight <= p.head {
			// Below the oldest recorded height, where there is nothing to check the hash against.
			if !p.config.HaltOnMismatch {
				return p.acceptMismatchedHash(blockHeight, nil, hash)
			}
			return ErrBelowPruneBoundary
		}
		// First commit for this height: write it.
		value := encodeHashValue(blockHeight, hash)
		if werr := p.db.Set(key, value, p.writeOpts); werr != nil {
			return fmt.Errorf("failed to persist hash for block %d: %w", blockHeight, werr)
		}
		p.cache.Add(blockHeight, bytes.Clone(hash))
		p.head = max(p.head, blockHeight)
		p.notEmpty = true
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
		if !p.config.HaltOnMismatch {
			return p.acceptMismatchedHash(blockHeight, existing, hash)
		}
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

// loadHead reads the newest recorded height from disk and populates p.head and p.recorded.
func (p *PebbleHashVault) loadHead() error {
	_, head, recorded, err := storedRange(p.db)
	if err != nil {
		return fmt.Errorf("failed to read the newest recorded height: %w", err)
	}
	p.head = head
	p.notEmpty = recorded
	return nil
}

// Records a hash that differs from the recorded one, discarding every hash from blockHeight up in the same
// atomic batch. Used when HaltOnMismatch is false. p.mu must be held.
func (p *PebbleHashVault) acceptMismatchedHash(blockHeight uint64, existing []byte, hash []byte) error {
	logger.Error("Hashvault detected a state hash mismatch; hash-vault-halt-on-mismatch is false, so the "+
		"recorded hashes from this block up are discarded and the new hash replaces them.",
		"blockHeight", blockHeight,
		"existingHex", hex.EncodeToString(existing),
		"incomingHex", hex.EncodeToString(hash),
		"hashVaultDir", p.config.DataDir,
	)
	batch := p.db.NewBatch()
	defer func() { _ = batch.Close() }()
	if err := batch.DeleteRange(hashKey(blockHeight), hashKeyUpperBound(), nil); err != nil {
		return fmt.Errorf("failed to stage discarding hashes from block %d: %w", blockHeight, err)
	}
	if err := batch.Set(hashKey(blockHeight), encodeHashValue(blockHeight, hash), nil); err != nil {
		return fmt.Errorf("failed to stage the replacing hash for block %d: %w", blockHeight, err)
	}
	if err := batch.Commit(p.writeOpts); err != nil {
		return fmt.Errorf("failed to replace hashes from block %d: %w", blockHeight, err)
	}
	p.cache.Purge()
	p.cache.Add(blockHeight, bytes.Clone(hash))
	p.head = blockHeight
	p.notEmpty = true
	return nil
}

// Head returns the newest recorded height, and false when the vault holds no hashes.
func (p *PebbleHashVault) Head() (uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.head, p.notEmpty
}

// Get returns the hash recorded for blockHeight, without blocking.
func (p *PebbleHashVault) Get(blockHeight uint64) ([32]byte, gigatypes.BlockHashStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return [32]byte{}, gigatypes.BlockHashStatusError, ErrClosed
	}
	if !p.notEmpty || blockHeight > p.head {
		return [32]byte{}, gigatypes.BlockHashStatusNotReady, nil
	}
	if blockHeight < p.pruneBoundary {
		return [32]byte{}, gigatypes.BlockHashStatusTooOld, nil
	}
	raw, closer, err := p.db.Get(hashKey(blockHeight))
	if errors.Is(err, pebble.ErrNotFound) {
		return [32]byte{}, gigatypes.BlockHashStatusTooOld, nil
	}
	if err != nil {
		return [32]byte{}, gigatypes.BlockHashStatusError,
			fmt.Errorf("failed to read hash for block %d: %w", blockHeight, err)
	}
	defer func() { _ = closer.Close() }()
	hash, err := decodeHashValue(blockHeight, raw)
	if err != nil {
		return [32]byte{}, gigatypes.BlockHashStatusError,
			fmt.Errorf("failed to decode hash for block %d: %w", blockHeight, err)
	}
	var out [32]byte
	copy(out[:], hash)
	return out, gigatypes.BlockHashStatusFound, nil
}

// Reset deletes every recorded hash and the prune boundary, and records hash as blockHeight's, leaving it
// the only one.
func (p *PebbleHashVault) Reset(ctx context.Context, blockHeight uint64, hash []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("failed to reset hashvault: %w", err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	if len(hash) != BlockHashSize {
		return ErrInvalidHashLength
	}
	batch := p.db.NewBatch()
	defer func() { _ = batch.Close() }()
	if err := batch.DeleteRange(hashKey(0), hashKeyUpperBound(), nil); err != nil {
		return fmt.Errorf("failed to stage hashvault reset: %w", err)
	}
	if err := batch.Delete(pruneBoundaryKey, nil); err != nil {
		return fmt.Errorf("failed to stage prune boundary clear during reset: %w", err)
	}
	if err := batch.Set(hashKey(blockHeight), encodeHashValue(blockHeight, hash), nil); err != nil {
		return fmt.Errorf("failed to stage hash for block %d during reset: %w", blockHeight, err)
	}
	if err := batch.Commit(p.writeOpts); err != nil {
		return fmt.Errorf("failed to reset hashvault to block %d: %w", blockHeight, err)
	}
	p.pruneBoundary = 0
	p.cache.Purge()
	p.cache.Add(blockHeight, bytes.Clone(hash))
	p.head = blockHeight
	p.notEmpty = true
	return nil
}

// PruneBelow permits the hashes of blocks below blockHeight to be deleted, as far as the vault's owner is
// concerned. A hash is deleted only once PruneHistory has permitted it too.
func (p *PebbleHashVault) PruneBelow(blockHeight uint64) {
	raiseFloor(&p.outerFloor, blockHeight)
}

// raiseFloor raises floor to blockHeight, leaving it where it is when it is already higher.
func raiseFloor(floor *atomic.Uint64, blockHeight uint64) {
	for {
		current := floor.Load()
		if blockHeight <= current || floor.CompareAndSwap(current, blockHeight) {
			return
		}
	}
}

// Name implements controller.PrunableStore.
func (p *PebbleHashVault) Name() string {
	return "HashVault"
}

// PruneHistory implements controller.PrunableStore. It permits the hashes of blocks below blockHeight to
// be deleted, as far as the storage garbage collector is concerned, and prunes every hash below both that
// and the floor PruneBelow has raised. The newest recorded hash is always kept.
func (p *PebbleHashVault) PruneHistory(blockHeight uint64) error {
	raiseFloor(&p.gcFloor, blockHeight)
	head, recorded := p.Head()
	if !recorded {
		return nil
	}
	floor := min(p.outerFloor.Load(), p.gcFloor.Load(), head)
	if err := p.Prune(context.Background(), floor); err != nil {
		return fmt.Errorf("failed to prune hashvault below %d: %w", floor, err)
	}
	return nil
}

// PruneSnapshots implements controller.PrunableStore. The vault keeps no snapshots.
func (p *PebbleHashVault) PruneSnapshots(uint64) error {
	return nil
}

// ExternalPruning implements controller.PrunableStore. The vault has no pruner of its own.
func (p *PebbleHashVault) ExternalPruning() bool {
	return true
}

// GetRollbackFloor implements controller.PrunableStore. Every recorded block is readable directly, so the
// floor is the newest recorded block less rollbackWindow, or 0 when the window is deeper than that.
func (p *PebbleHashVault) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, recorded := p.Head()
	if !recorded || head < rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

// GetLatestBlock implements controller.PrunableStore. It returns the newest recorded block, or 0 when the
// vault holds no hashes.
func (p *PebbleHashVault) GetLatestBlock() (uint64, error) {
	head, _ := p.Head()
	return head, nil
}
