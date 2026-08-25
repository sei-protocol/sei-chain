package littblock

import (
	"fmt"
	"sync/atomic"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	litttypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// tableName is the single table holding every record kind, despite the name.
//
// It is persisted layout rather than just an identifier: littdb stores a table's data at
// <root>/<tableName>/segments, so changing it makes NewBlockDB open a fresh empty table and leaves
// data under the old name unreachable.
const tableName = "blocks"

var _ blocktypes.BlockDB = (*blockDB)(nil)

// blockDB is a durable blocktypes.BlockDB backed by LittDB.
type blockDB struct {
	db    littdb.DB
	table littdb.Table

	// watermark is the reclamation floor. gcFilter and readers load it without
	// holding any lock, so it is accessed atomically.
	watermark atomic.Uint64
}

// NewBlockDB opens (or creates) a LittDB-backed blocktypes.BlockDB from config. The
// underlying LittDB is built from config.Litt, and the table applies
// config.RetentionTime as a TTL failsafe (pruning never reclaims data younger
// than that even once the watermark has advanced past it).
//
// Opening a store that holds records but no QC fails: the store lost data out of
// band and must not be served.
func NewBlockDB(config *BlockDBConfig) (blocktypes.BlockDB, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid block db config: %w", err)
	}
	db, err := littbuilder.NewDB(config.Litt)
	if err != nil {
		return nil, fmt.Errorf("failed to open litt db: %w", err)
	}

	s := &blockDB{db: db}

	// Every record kind lives in one table with a single write shard. The store
	// relies on LittDB's single-shard in-write-order crash atomicity (after a
	// crash the surviving writes form a contiguous prefix of the write order,
	// never a gapped subset), which is the durability guarantee the
	// blocktypes.BlockDB contract promises its caller. It also backs
	// write-order recovery scans. ShardingFactor > 1, or splitting the kinds
	// across several tables, would void this.
	tableConfig := littdb.DefaultTableConfig(tableName)
	tableConfig.TTL = config.RetentionTime
	tableConfig.GCFilter = s.gcFilter
	tableConfig.ShardingFactor = 1 // DO NOT CHANGE!!
	table, err := db.BuildTable(tableConfig)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to build ledger table: %w", err)
	}
	s.table = table
	if err := s.recoverWatermark(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to recover watermark: %w", err)
	}
	return s, nil
}

// recoverWatermark re-derives the reclamation floor from the oldest surviving QC
// record. The floor is in-memory only, so a restart forgets every
// SetPruneWatermark; that costs nothing for reclamation, since nothing new is
// released, but it must not leave blocks readable whose covering QC is already
// gone.
//
// QC records are written before anything else covering the same number, so the
// oldest one bounds what the store may serve, and the scan stops at it. A
// non-empty store with no QC record has lost that bound and must not reopen.
func (s *blockDB) recoverWatermark() error {
	it, err := s.Scan(false)
	if err != nil {
		return err
	}
	defer func() { _ = it.Close() }()
	empty := true
	for {
		ok, err := it.Next()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		empty = false
		if it.Kind() == blocktypes.KindQC {
			s.watermark.Store(it.Number())
			return nil
		}
	}
	if !empty {
		return fmt.Errorf("corrupt store: no QC in non-empty store")
	}
	return nil
}

// GetPruneWatermark returns the reclamation floor.
func (s *blockDB) GetPruneWatermark() uint64 {
	return s.watermark.Load()
}

// SetPruneWatermark raises the reclamation floor to n. Reclaiming the data
// happens later, on LittDB's GC schedule and no earlier than
// BlockDBConfig.RetentionTime.
func (s *blockDB) SetPruneWatermark(n uint64) {
	for {
		current := s.watermark.Load()
		if n <= current {
			return
		}
		if s.watermark.CompareAndSwap(current, n) {
			return
		}
	}
}

func (s *blockDB) PutRecord(kind blocktypes.RecordKind, first, next uint64, value []byte) error {
	prefix, err := kindPrefix(kind)
	if err != nil {
		return err
	}
	var aliases []*litttypes.SecondaryKey
	for m := first + 1; m < next; m++ {
		aliases = append(aliases, &litttypes.SecondaryKey{
			Key:    numberKey(prefix, m),
			Offset: 0,
			Length: uint32(len(value)), //nolint:gosec // value length fits u32 (litt value cap is 2^32)
		})
	}
	if err := s.table.Put(numberKey(prefix, first), value, aliases...); err != nil {
		return fmt.Errorf("failed to put %s [%d,%d): %w", kind, first, next, err)
	}
	return nil
}

func (s *blockDB) PutBlock(n uint64, hash []byte, value []byte) error {
	hashAlias := &litttypes.SecondaryKey{
		Key:    blockHashKey(hash),
		Offset: 0,
		Length: uint32(len(value)), //nolint:gosec // value length fits u32 (litt value cap is 2^32)
	}
	if err := s.table.Put(numberKey(kindBlock, n), value, hashAlias); err != nil {
		return fmt.Errorf("failed to put block %d: %w", n, err)
	}
	return nil
}

func (s *blockDB) GetRecord(kind blocktypes.RecordKind, n uint64) ([]byte, bool, error) {
	prefix, err := kindPrefix(kind)
	if err != nil {
		return nil, false, err
	}
	value, exists, err := s.table.Get(numberKey(prefix, n))
	if err != nil {
		return nil, false, fmt.Errorf("failed to read %s %d: %w", kind, n, err)
	}
	return value, exists, nil
}

func (s *blockDB) GetBlockByHash(hash []byte) ([]byte, bool, error) {
	value, exists, err := s.table.Get(blockHashKey(hash))
	if err != nil {
		return nil, false, fmt.Errorf("failed to read block by hash: %w", err)
	}
	return value, exists, nil
}

func (s *blockDB) Scan(newestFirst bool) (blocktypes.RecordIterator, error) {
	it, err := s.table.Iterator(newestFirst)
	if err != nil {
		return nil, fmt.Errorf("failed to open record iterator: %w", err)
	}
	return &recordIterator{it: it}, nil
}

// gcFilter marks a key in the shared ledger table as reclaimable, dispatching on
// its kind prefix:
//
//   - number-keyed records (the primary key and every per-covered-number secondary) are
//     reclaimable once their number is below the watermark, so a QC's segment is
//     reclaimable only once its highest covered number (Next-1) is below the
//     watermark — i.e. once Next <= watermark; a QC/AppProposal/AppQC straddling the
//     watermark is retained;
//   - header-hash aliases share their block's segment, so they always pass — the
//     block's primary number key is what actually gates segment reclamation.
func (s *blockDB) gcFilter(key []byte, _ bool) (bool, error) {
	switch keyKind(key) {
	case kindBlock, kindQC, kindAppProp, kindAppQC:
		return decodeNumberKey(key) < s.GetPruneWatermark(), nil
	case kindBlockHash:
		return true, nil
	default:
		return false, fmt.Errorf("unknown ledger key kind %q", key[0])
	}
}

func (s *blockDB) Flush() error {
	if err := s.table.Flush(); err != nil {
		return fmt.Errorf("failed to flush ledger table: %w", err)
	}
	return nil
}

func (s *blockDB) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("failed to close litt db: %w", err)
	}
	return nil
}

// recordIterator adapts a littdb.Iterator to blocktypes.RecordIterator, presenting
// only the primary number-keyed records and skipping the hash aliases.
type recordIterator struct {
	it     littdb.Iterator
	kind   blocktypes.RecordKind
	number uint64
}

func (r *recordIterator) Next() (bool, error) {
	for {
		ok, err := r.it.Next()
		if err != nil {
			return false, fmt.Errorf("failed to advance record iterator: %w", err)
		}
		if !ok {
			return false, nil
		}
		key, isPrimary, err := r.it.GetKey()
		if err != nil {
			return false, fmt.Errorf("failed to read record key: %w", err)
		}
		if !isPrimary {
			continue
		}
		kind, ok := recordKind(keyKind(key))
		if !ok {
			continue
		}
		r.kind = kind
		r.number = decodeNumberKey(key)
		return true, nil
	}
}

func (r *recordIterator) Kind() blocktypes.RecordKind {
	return r.kind
}

func (r *recordIterator) Number() uint64 {
	return r.number
}

func (r *recordIterator) Value() ([]byte, error) {
	value, err := r.it.GetValue()
	if err != nil {
		return nil, fmt.Errorf("failed to read record value: %w", err)
	}
	return value, nil
}

func (r *recordIterator) Close() error {
	if err := r.it.Close(); err != nil {
		return fmt.Errorf("failed to close record iterator: %w", err)
	}
	return nil
}

// ForceGC runs a synchronous garbage-collection pass over the table backing db,
// so any pending prune takes effect immediately rather than on the periodic GC
// schedule. db must be a blocktypes.BlockDB returned by NewBlockDB. Intended for
// tests and operational tooling.
func ForceGC(db blocktypes.BlockDB) error {
	impl, ok := db.(*blockDB)
	if !ok {
		return fmt.Errorf("ForceGC: db is not a littblock block store (%T)", db)
	}
	managed, ok := impl.table.(littdb.ManagedTable)
	if !ok {
		return fmt.Errorf("table %q is not a ManagedTable", impl.table.Name())
	}
	if err := managed.RunGC(); err != nil {
		return fmt.Errorf("failed to run GC on table %q: %w", impl.table.Name(), err)
	}
	return nil
}
