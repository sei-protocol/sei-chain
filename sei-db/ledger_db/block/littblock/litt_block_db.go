package littblock

import (
	"fmt"
	"slices"
	"sync/atomic"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
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
// Opening a store that holds records but no SuperBlock fails: the store lost data out of
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

// recoverWatermark restores the floor from the oldest surviving SuperBlock.
// A non-empty store without a SuperBlock cannot supply a recovery anchor.
func (s *blockDB) recoverWatermark() error {
	it, err := s.table.Iterator(false)
	if err != nil {
		return fmt.Errorf("failed to open record iterator: %w", err)
	}
	defer func() { _ = it.Close() }()
	empty := true
	for {
		ok, err := it.Next()
		if err != nil {
			return fmt.Errorf("failed to advance record iterator: %w", err)
		}
		if !ok {
			break
		}
		key, isPrimary, err := it.GetKey()
		if err != nil {
			return fmt.Errorf("failed to read record key: %w", err)
		}
		if !isPrimary {
			continue
		}
		recordKey := ledgerKey(key)
		kind, ok := recordKey.primaryKind()
		if !ok {
			return fmt.Errorf("invalid primary ledger key %x", key)
		}
		empty = false
		if kind == kindSuperBlock {
			s.watermark.Store(uint64(recordKey.number()))
			return nil
		}
	}
	if !empty {
		return fmt.Errorf("corrupt store: no SuperBlock in non-empty store")
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

// PutBlock stores a SuperBlock's QC and lane blocks in one atomic LittDB value.
func (s *blockDB) PutBlock(block blocktypes.BlockRecord) error {
	return s.putRecord(encodeBlock(block))
}

// PutAppQC stores an application QC at its road index.
func (s *blockDB) PutAppQC(appQC blocktypes.AppQCRecord) error {
	return s.putRecord(encodeAppQC(appQC))
}

// PutAppProposal stores an application proposal at its road index.
func (s *blockDB) PutAppProposal(proposal blocktypes.AppProposalRecord) error {
	return s.putRecord(encodeAppProposal(proposal))
}

// putRecord inserts an encoded record and its aliases.
func (s *blockDB) putRecord(record record) error {
	if err := s.table.Put(record.Key, record.Value, record.Aliases...); err != nil {
		return fmt.Errorf("failed to put ledger record: %w", err)
	}
	return nil
}

// getRecord reads the serialized payload at a numbered lookup key.
func (s *blockDB) getRecord(kind recordKind, n blocktypes.BlockNumber) ([]byte, bool, error) {
	value, exists, err := s.table.Get(kind.key(n))
	if err != nil {
		return nil, false, fmt.Errorf("failed to read record kind %d at %d: %w", kind, n, err)
	}
	if !exists {
		return nil, false, nil
	}
	return value, true, nil
}

// GetBlockByHash returns the complete record indexed by a SuperBlock hash.
func (s *blockDB) GetBlockByHash(hash blocktypes.BlockHash) (blocktypes.BlockRecord, bool, error) {
	value, exists, err := s.table.Get(blockHashKey(hash))
	if err != nil || !exists {
		return blocktypes.BlockRecord{}, exists, err
	}
	block, err := decodeBlock(value)
	return block, err == nil, err
}

// GetBlockByNumber returns the complete record at road index n.
func (s *blockDB) GetBlockByNumber(n blocktypes.BlockNumber) (blocktypes.BlockRecord, bool, error) {
	value, exists, err := s.table.Get(kindSuperBlock.key(n))
	if err != nil || !exists {
		return blocktypes.BlockRecord{}, exists, err
	}
	block, err := decodeBlock(value)
	return block, err == nil, err
}

// GetLaneBlock returns the lane block at a road index and header offset.
func (s *blockDB) GetLaneBlock(n blocktypes.BlockNumber, offset uint64) ([]byte, bool, error) {
	return s.table.Get(laneBlockKey(n, offset))
}

// ReadSuffix reads complete primary records from the latest AppQC onward.
func (s *blockDB) ReadSuffix() (blocktypes.Suffix, error) {
	it, err := s.table.Iterator(true)
	if err != nil {
		return blocktypes.Suffix{}, fmt.Errorf("failed to open record iterator: %w", err)
	}
	defer func() { _ = it.Close() }()
	var suffix blocktypes.Suffix

	var first blocktypes.BlockNumber
	gotAppQC := false
	for {
		ok, err := it.Next()
		if err != nil {
			return blocktypes.Suffix{}, fmt.Errorf("failed to advance record iterator: %w", err)
		}
		if !ok {
			break
		}
		key, isPrimary, err := it.GetKey()
		if err != nil {
			return blocktypes.Suffix{}, fmt.Errorf("failed to read record key: %w", err)
		}
		if !isPrimary {
			continue
		}
		recordKey := ledgerKey(key)
		kind, ok := recordKey.primaryKind()
		if !ok {
			return blocktypes.Suffix{}, fmt.Errorf("invalid primary ledger key %x", key)
		}
		if kind == kindAppQC && !gotAppQC {
			first = recordKey.number()
			gotAppQC = true
		}
		if !gotAppQC || recordKey.number() >= first {
			value, err := it.GetValue()
			if err != nil {
				return blocktypes.Suffix{}, fmt.Errorf("failed to read record value: %w", err)
			}
			switch kind {
			case kindSuperBlock:
				block, err := decodeBlock(value)
				if err != nil {
					return blocktypes.Suffix{}, err
				}
				suffix.Blocks = append(suffix.Blocks, block)
			case kindAppQC:
				suffix.AppQCs = append(suffix.AppQCs, blocktypes.AppQCRecord{Index: recordKey.number(), Data: value})
			case kindAppProp:
				suffix.AppProposals = append(suffix.AppProposals, blocktypes.AppProposalRecord{Index: recordKey.number(), Data: value})
			}
		}
		if gotAppQC && kind == kindSuperBlock && recordKey.number() <= first {
			break
		}
	}
	suffix.Blocks = slices.DeleteFunc(suffix.Blocks, func(block blocktypes.BlockRecord) bool { return gotAppQC && block.Index < first })
	suffix.AppQCs = slices.DeleteFunc(suffix.AppQCs, func(qc blocktypes.AppQCRecord) bool { return gotAppQC && qc.Index < first })
	suffix.AppProposals = slices.DeleteFunc(suffix.AppProposals, func(proposal blocktypes.AppProposalRecord) bool { return gotAppQC && proposal.Index < first })
	slices.Reverse(suffix.Blocks)
	slices.Reverse(suffix.AppQCs)
	slices.Reverse(suffix.AppProposals)
	return suffix, nil
}

// reclaimable reports whether a key is eligible for reclamation below watermark.
// Hash aliases are always eligible; their primary record and numbered aliases govern retention.
func (k ledgerKey) reclaimable(watermark blocktypes.BlockNumber) (bool, error) {
	switch keyKind(k) {
	case kindSuperBlock, kindLaneBlock, kindQC, kindAppProp, kindAppQC:
		return k.number() < watermark, nil
	case kindBlockHash:
		return true, nil
	default:
		return false, fmt.Errorf("unknown ledger key kind in %x", k)
	}
}

// gcFilter reports whether a ledger key is below the reclamation floor.
func (s *blockDB) gcFilter(key []byte, _ bool) (bool, error) {
	return ledgerKey(key).reclaimable(s.GetPruneWatermark())
}

// Flush makes all previously returned writes durable.
func (s *blockDB) Flush() error {
	if err := s.table.Flush(); err != nil {
		return fmt.Errorf("failed to flush ledger table: %w", err)
	}
	return nil
}

// Close releases the underlying LittDB.
func (s *blockDB) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("failed to close litt db: %w", err)
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

// GetQC returns the serialized QC at road n.
func (s *blockDB) GetQC(n blocktypes.BlockNumber) ([]byte, bool, error) {
	return s.getRecord(kindQC, n)
}

// GetAppQC returns the serialized application QC at road n.
func (s *blockDB) GetAppQC(n blocktypes.BlockNumber) ([]byte, bool, error) {
	return s.getRecord(kindAppQC, n)
}

// GetAppProposal returns the serialized application proposal at road n.
func (s *blockDB) GetAppProposal(n blocktypes.BlockNumber) ([]byte, bool, error) {
	return s.getRecord(kindAppProp, n)
}
