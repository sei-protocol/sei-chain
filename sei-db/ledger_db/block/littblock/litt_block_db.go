package littblock

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
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
	it, err := s.scan(false)
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
func (s *blockDB) PutQC(group blocktypes.QCGroupRecord) error {
	return s.putRecord(encodeQCGroup(group))
}

func (s *blockDB) PutAppQC(appQC blocktypes.AppQCRecord) error {
	return s.putRecord(encodeAppQC(appQC))
}

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

func (s *blockDB) getRecord(kind blocktypes.RecordKind, n blocktypes.BlockNumber) ([]byte, bool, error) {
	key := kind.Key(n)
	value, exists, err := s.table.Get(key)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read %s %d: %w", kind, n, err)
	}
	return value, exists, nil
}

func (s *blockDB) GetBlockByHash(hash blocktypes.BlockHash) (blocktypes.BlockNumber, []byte, bool, error) {
	value, exists, err := s.table.Get(blocktypes.BlockHashKey(hash))
	if err != nil || !exists {
		return 0, nil, exists, err
	}
	block, err := decodeBlock(value)
	return block.Number, block.Value, err == nil, err
}

func (s *blockDB) scan(newestFirst bool) (*recordIterator, error) {
	it, err := s.table.Iterator(newestFirst)
	if err != nil {
		return nil, fmt.Errorf("failed to open record iterator: %w", err)
	}
	return &recordIterator{it: it}, nil
}

// ReadSuffix reads complete primary records from the latest applied QC group onward.
func (s *blockDB) ReadSuffix() (blocktypes.Suffix, error) {
	it, err := s.scan(true)
	if err != nil {
		return blocktypes.Suffix{}, err
	}
	defer func() { _ = it.Close() }()
	var suffix blocktypes.Suffix
	var groups []blocktypes.QCGroupRecord
	var first blocktypes.BlockNumber
	gotAppQC := false
	for {
		ok, err := it.Next()
		if err != nil {
			return blocktypes.Suffix{}, err
		}
		if !ok {
			break
		}
		if it.Kind() == blocktypes.KindAppQC && !gotAppQC {
			first = it.Number()
			gotAppQC = true
		}
		if !gotAppQC || it.Number() >= first {
			value, err := it.Value()
			if err != nil {
				return blocktypes.Suffix{}, err
			}
			switch it.Kind() {
			case blocktypes.KindQCGroup:
				group, err := decodeQCGroup(value)
				if err != nil {
					return blocktypes.Suffix{}, err
				}
				groups = append(groups, group)
			case blocktypes.KindAppQC:
				qc, err := decodeAppQC(value)
				if err != nil {
					return blocktypes.Suffix{}, err
				}
				suffix.AppQCs = append(suffix.AppQCs, qc)
			case blocktypes.KindAppProposal:
				proposal, err := decodeAppProposal(value)
				if err != nil {
					return blocktypes.Suffix{}, err
				}
				suffix.AppProposals = append(suffix.AppProposals, proposal)
			}
		}
		if gotAppQC && it.Kind() == blocktypes.KindQCGroup && it.Number() <= first {
			break
		}
	}
	groups = slices.DeleteFunc(groups, func(group blocktypes.QCGroupRecord) bool { return gotAppQC && group.Blocks[0].Number < first })
	suffix.AppQCs = slices.DeleteFunc(suffix.AppQCs, func(qc blocktypes.AppQCRecord) bool { return gotAppQC && qc.First < first })
	suffix.AppProposals = slices.DeleteFunc(suffix.AppProposals, func(proposal blocktypes.AppProposalRecord) bool { return gotAppQC && proposal.First < first })
	slices.Reverse(groups)
	for _, group := range groups {
		suffix.QCs = append(suffix.QCs, group.QC)
		suffix.Blocks = append(suffix.Blocks, group.Blocks...)
	}
	slices.Reverse(suffix.AppQCs)
	slices.Reverse(suffix.AppProposals)
	return suffix, nil
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

// recordIterator adapts a littdb.Iterator, presenting
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
			return false, fmt.Errorf("invalid primary ledger key %x", key)
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

// record contains the encoded data passed to LittDB.
type record struct {
	Key     blocktypes.Key
	Value   []byte
	Aliases []*litttypes.SecondaryKey
}

func encodeQCGroup(group blocktypes.QCGroupRecord) record {
	if err := group.Validate(); err != nil {
		panic(err)
	}
	key := blocktypes.KindQCGroup.Key(group.Blocks[0].Number)
	r := record{Key: key}
	r.Value, _, _ = appendValue(r.Value, group.QC.Data)
	qcLength := uint32(len(r.Value)) //nolint:gosec // Validate bounds the value size
	for _, block := range group.Blocks {
		value := binary.BigEndian.AppendUint64(nil, uint64(block.Number))
		value = append(value, block.Value...)
		var offset, length uint32
		r.Value, offset, length = appendValue(r.Value, value)
		key := blocktypes.KindBlock.Key(block.Number)
		qcKey := blocktypes.KindQC.Key(block.Number)
		r.Aliases = append(r.Aliases,
			&litttypes.SecondaryKey{Key: qcKey, Length: qcLength},
			&litttypes.SecondaryKey{Key: key, Offset: offset, Length: length},
			&litttypes.SecondaryKey{Key: blocktypes.BlockHashKey(block.Hash), Offset: offset, Length: length})
	}
	return r
}

func appendValue(dst, value []byte) ([]byte, uint32, uint32) {
	length := uint32(len(value)) //nolint:gosec // validated before encoding
	dst = binary.BigEndian.AppendUint32(dst, length)
	offset := uint32(len(dst)) //nolint:gosec // validated before encoding
	return append(dst, value...), offset, length
}

func encodeAppQC(qc blocktypes.AppQCRecord) record {
	return encodeApp(blocktypes.KindAppQC, qc.First, qc.Next, qc.Data)
}
func encodeAppProposal(proposal blocktypes.AppProposalRecord) record {
	return encodeApp(blocktypes.KindAppProposal, proposal.First, proposal.Next, proposal.Data)
}
func encodeApp(kind blocktypes.RecordKind, first, next blocktypes.BlockNumber, data []byte) record {
	if uint64(len(data))+16 >= math.MaxUint32 {
		panic("app record exceeds value size limit")
	}
	key := kind.Key(first)
	r := record{Key: key}
	r.Value = binary.BigEndian.AppendUint64(r.Value, uint64(first))
	r.Value = binary.BigEndian.AppendUint64(r.Value, uint64(next))
	r.Value = append(r.Value, data...)
	for n := first + 1; n < next; n++ {
		key := kind.Key(n)
		r.Aliases = append(r.Aliases, &litttypes.SecondaryKey{Key: key, Length: uint32(len(r.Value))}) //nolint:gosec // checked above
	}
	return r
}

func consumeValue(value []byte) ([]byte, []byte, error) {
	if len(value) < 4 {
		return nil, nil, fmt.Errorf("truncated value length")
	}
	length := uint64(binary.BigEndian.Uint32(value))
	value = value[4:]
	if length > uint64(len(value)) {
		return nil, nil, fmt.Errorf("truncated value")
	}
	return value[:length], value[length:], nil
}
func decodeQCGroup(value []byte) (blocktypes.QCGroupRecord, error) {
	var group blocktypes.QCGroupRecord
	var err error
	group.QC.Data, value, err = consumeValue(value)
	if err != nil {
		return blocktypes.QCGroupRecord{}, err
	}
	for len(value) > 0 {
		var encoded []byte
		encoded, value, err = consumeValue(value)
		if err != nil {
			return blocktypes.QCGroupRecord{}, err
		}
		block, err := decodeBlock(encoded)
		if err != nil {
			return blocktypes.QCGroupRecord{}, err
		}
		group.Blocks = append(group.Blocks, block)
	}
	if len(group.Blocks) == 0 {
		return blocktypes.QCGroupRecord{}, fmt.Errorf("QC group requires blocks")
	}
	return group, nil
}
func decodeBlock(value []byte) (blocktypes.BlockRecord, error) {
	if len(value) < 8 {
		return blocktypes.BlockRecord{}, fmt.Errorf("truncated block number")
	}
	return blocktypes.BlockRecord{
		Number: blocktypes.BlockNumber(binary.BigEndian.Uint64(value)),
		Value:  value[8:],
	}, nil
}
func decodeApp(value []byte) (blocktypes.BlockNumber, blocktypes.BlockNumber, []byte, error) {
	if len(value) < 16 {
		return 0, 0, nil, fmt.Errorf("truncated app record")
	}
	return blocktypes.BlockNumber(binary.BigEndian.Uint64(value[:8])), blocktypes.BlockNumber(binary.BigEndian.Uint64(value[8:16])), value[16:], nil
}
func decodeAppQC(value []byte) (blocktypes.AppQCRecord, error) {
	first, next, data, err := decodeApp(value)
	return blocktypes.AppQCRecord{First: first, Next: next, Data: data}, err
}
func decodeAppProposal(value []byte) (blocktypes.AppProposalRecord, error) {
	first, next, data, err := decodeApp(value)
	return blocktypes.AppProposalRecord{First: first, Next: next, Data: data}, err
}
func (s *blockDB) GetQC(n blocktypes.BlockNumber) ([]byte, bool, error) {
	value, exists, err := s.getRecord(blocktypes.KindQC, n)
	if err != nil || !exists {
		return nil, exists, err
	}
	qc, err := decodeQC(value)
	return qc.Data, err == nil, err
}
func (s *blockDB) GetBlock(n blocktypes.BlockNumber) ([]byte, bool, error) {
	value, exists, err := s.getRecord(blocktypes.KindBlock, n)
	if err != nil || !exists {
		return nil, exists, err
	}
	block, err := decodeBlock(value)
	return block.Value, err == nil, err
}
func (s *blockDB) GetAppQC(n blocktypes.BlockNumber) ([]byte, bool, error) {
	value, exists, err := s.getRecord(blocktypes.KindAppQC, n)
	if err != nil || !exists {
		return nil, exists, err
	}
	qc, err := decodeAppQC(value)
	return qc.Data, err == nil, err
}
func (s *blockDB) GetAppProposal(n blocktypes.BlockNumber) ([]byte, bool, error) {
	value, exists, err := s.getRecord(blocktypes.KindAppProposal, n)
	if err != nil || !exists {
		return nil, exists, err
	}
	proposal, err := decodeAppProposal(value)
	return proposal.Data, err == nil, err
}

func decodeQC(value []byte) (blocktypes.QCRecord, error) {
	data, _, err := consumeValue(value)
	return blocktypes.QCRecord{Data: data}, err
}
