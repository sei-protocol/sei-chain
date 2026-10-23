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
// Opening a store that holds records but no QC group fails: the store lost data out of
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
// group. The floor is in-memory only, so a restart forgets every
// SetPruneWatermark; that costs nothing for reclamation, since nothing new is
// released, but it must not leave blocks readable whose covering QC is already
// gone.
//
// The oldest QC group bounds what the store may serve. A non-empty store
// without a QC group has lost that bound and must not reopen.
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
		if it.Kind() == blocktypes.KindQCGroup {
			s.watermark.Store(uint64(it.Number()))
			return nil
		}
	}
	if !empty {
		return fmt.Errorf("corrupt store: no QC group in non-empty store")
	}
	return nil
}

// GetPruneWatermark returns the reclamation floor.
func (s *blockDB) GetPruneWatermark() blocktypes.BlockNumber {
	return blocktypes.BlockNumber(s.watermark.Load())
}

// SetPruneWatermark raises the reclamation floor to n. Reclaiming the data
// happens later, on LittDB's GC schedule and no earlier than
// BlockDBConfig.RetentionTime.
func (s *blockDB) SetPruneWatermark(n blocktypes.BlockNumber) {
	for {
		current := s.watermark.Load()
		if uint64(n) <= current {
			return
		}
		if s.watermark.CompareAndSwap(current, uint64(n)) {
			return
		}
	}
}

// PutQC stores a QC and its blocks in one atomic LittDB value.
func (s *blockDB) PutQC(qc blocktypes.QCRecord) error {
	record, err := qc.Encode()
	if err != nil {
		return err
	}
	return s.putRecord(record)
}

func (s *blockDB) PutAppQC(appQC blocktypes.AppQCRecord) error {
	record, err := appQC.Encode()
	if err != nil {
		return err
	}
	return s.putRecord(record)
}

func (s *blockDB) PutAppProposal(proposal blocktypes.AppProposalRecord) error {
	record, err := proposal.Encode()
	if err != nil {
		return err
	}
	return s.putRecord(record)
}

// putRecord inserts an encoded record and its aliases.
func (s *blockDB) putRecord(record blocktypes.Record) error {
	aliases := make([]*litttypes.SecondaryKey, len(record.Aliases))
	for i, alias := range record.Aliases {
		aliases[i] = &litttypes.SecondaryKey{Key: alias.Key, Offset: alias.Offset, Length: alias.Length}
	}
	if err := s.table.Put(record.Key, record.Value, aliases...); err != nil {
		return fmt.Errorf("failed to put ledger record: %w", err)
	}
	return nil
}

func (s *blockDB) GetRecord(kind blocktypes.RecordKind, n blocktypes.BlockNumber) ([]byte, bool, error) {
	key, err := kind.Key(n)
	if err != nil {
		return nil, false, err
	}
	value, exists, err := s.table.Get(key)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read %s %d: %w", kind, n, err)
	}
	return value, exists, nil
}

func (s *blockDB) GetBlockByHash(hash []byte) ([]byte, bool, error) {
	value, exists, err := s.table.Get(blocktypes.BlockHashKey(hash))
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

// gcFilter reports whether a ledger key is below the reclamation floor.
func (s *blockDB) gcFilter(key []byte, _ bool) (bool, error) {
	return blocktypes.Key(key).Reclaimable(s.GetPruneWatermark())
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
// primary records, including complete packed QC groups.
type recordIterator struct {
	it     littdb.Iterator
	kind   blocktypes.RecordKind
	number blocktypes.BlockNumber
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
		recordKey := blocktypes.Key(key)
		kind, ok := recordKey.RecordKind()
		if !ok {
			return false, fmt.Errorf("invalid primary ledger key kind %q", key[0])
		}
		r.kind = kind
		r.number = recordKey.Number()
		return true, nil
	}
}

func (r *recordIterator) Kind() blocktypes.RecordKind {
	return r.kind
}

func (r *recordIterator) Number() blocktypes.BlockNumber {
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
