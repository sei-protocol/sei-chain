// Package blockstore provides LittDB-backed storage for Autobahn consensus.
package blockstore

import (
	"encoding/binary"
	"fmt"
	"slices"
	"sync/atomic"

	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// tableName is the single table holding every record kind, despite the name.
//
// It is persisted layout rather than just an identifier: littdb stores a table's data at
// <root>/<tableName>/segments, so changing it makes New open a fresh empty table and leaves
// data under the old name unreachable.
const tableName = "blocks"

// Store is the durable backing store for the consensus state machine. It persists complete
// CommitQC groups, indexed by RoadIndex and road block header hash, and execution records.
// All methods are safe for concurrent use.
//
// Writes are visible immediately. Flush makes all previously completed writes
// durable. A crash can lose an unflushed suffix, but surviving records have no
// gaps: a CommitQC and all its lane blocks survive together, and application
// records never survive without their preceding QC group and app records.
//
// Each write stream must advance by one RoadIndex. At each road, WriteBlock must
// precede WriteAppProposal, which must precede WriteAppQC. Rejected writes store
// nothing. Reads below First return ErrPruned; absent records at or above First
// return None. Hash lookups return None for unknown or pruned SuperBlocks.
type Store struct {
	status utils.Mutex[*utils.Option[types.SuffixRange]]
	first  atomic.Uint64
	db     littdb.DB
	table  littdb.Table
}

// New opens a consensus block store and restores its retention floor and write cursors.
func New(config *Config) (*Store, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config.Validate(): %w", err)
	}
	db, err := littbuilder.NewDB(config.Litt)
	if err != nil {
		return nil, fmt.Errorf("littbuilder.NewDB(): %w", err)
	}

	s := &Store{db: db, status: utils.NewMutex(new(utils.Option[types.SuffixRange]))}
	Global.pruneWatermarkAt().Set(0)

	// Every record kind lives in one table with a single write shard. The store
	// relies on LittDB's single-shard in-write-order crash atomicity (after a
	// crash the surviving writes form a contiguous prefix of the write order,
	// never a gapped subset), which is the durability guarantee the
	// Store contract promises its caller. It also backs
	// write-order recovery scans. ShardingFactor > 1, or splitting the kinds
	// across several tables, would void this.
	tableConfig := littdb.DefaultTableConfig(tableName)
	tableConfig.TTL = config.RetentionTime
	tableConfig.GCFilter = s.gcFilter
	tableConfig.ShardingFactor = 1 // DO NOT CHANGE!!
	table, err := db.BuildTable(tableConfig)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db.BuildTable(): %w", err)
	}
	s.table = table
	first, err := s.recoverWatermark()
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("s.recoverWatermark(): %w", err)
	}
	suffix, err := s.readSuffix()
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("s.readSuffix(): %w", err)
	}
	if err := s.restore(first, suffix); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("s.restore(%v): %w", first, err)
	}
	return s, nil
}

// recoverWatermark restores the floor from the oldest surviving SuperBlock.
// A non-empty store without a SuperBlock cannot supply a recovery anchor.
func (s *Store) recoverWatermark() (types.RoadIndex, error) {
	it, err := s.table.Iterator(false)
	if err != nil {
		return 0, fmt.Errorf("s.table.Iterator(false): %w", err)
	}
	defer func() { _ = it.Close() }()
	empty := true
	for {
		ok, err := it.Next()
		if err != nil {
			return 0, fmt.Errorf("it.Next(): %w", err)
		}
		if !ok {
			break
		}
		key, isPrimary, err := it.GetKey()
		if err != nil {
			return 0, fmt.Errorf("it.GetKey(): %w", err)
		}
		if !isPrimary {
			continue
		}
		recordKey, err := decodeLedgerKey(key)
		if err != nil {
			return 0, fmt.Errorf("decodeLedgerKey(): %w", err)
		}
		empty = false
		switch recordKey.kind {
		case kindBlock:
			key, err := recordKey.decodeBlock()
			if err != nil {
				return 0, fmt.Errorf("recordKey.decodeBlock(): %w", err)
			}
			return key.Index, nil
		case kindAppQC:
			if _, err := recordKey.decodeAppQC(); err != nil {
				return 0, fmt.Errorf("recordKey.decodeAppQC(): %w", err)
			}
		case kindAppProp:
			if _, err := recordKey.decodeAppProposal(); err != nil {
				return 0, fmt.Errorf("recordKey.decodeAppProposal(): %w", err)
			}
		default:
			return 0, fmt.Errorf("invalid primary ledger key %x", key)
		}
	}
	if !empty {
		return 0, fmt.Errorf("corrupt store: no SuperBlock in non-empty store")
	}
	return 0, nil
}

// getRecord reads the serialized payload at key.
func (s *Store) getRecord(key ledgerKey) ([]byte, bool, error) {
	value, exists, err := s.table.Get(key.bytes())
	if err != nil {
		return nil, false, fmt.Errorf("s.table.Get(%x): %w", key.bytes(), err)
	}
	return value, exists, nil
}

// readSuffix reads complete primary records from the latest AppQC onward.
func (s *Store) readSuffix() (types.Suffix, error) {
	it, err := s.table.Iterator(true)
	if err != nil {
		return types.Suffix{}, fmt.Errorf("s.table.Iterator(true): %w", err)
	}
	defer func() { _ = it.Close() }()
	var suffix types.Suffix

	var first types.RoadIndex
	gotAppQC := false
	for {
		ok, err := it.Next()
		if err != nil {
			return types.Suffix{}, fmt.Errorf("it.Next(): %w", err)
		}
		if !ok {
			break
		}
		rawKey, isPrimary, err := it.GetKey()
		if err != nil {
			return types.Suffix{}, fmt.Errorf("it.GetKey(): %w", err)
		}
		if !isPrimary {
			continue
		}
		key, err := decodeLedgerKey(rawKey)
		if err != nil {
			return types.Suffix{}, fmt.Errorf("decodeLedgerKey(): %w", err)
		}
		var index types.RoadIndex
		switch key.kind {
		case kindBlock:
			key, err := key.decodeBlock()
			if err != nil {
				return types.Suffix{}, fmt.Errorf("key.decodeBlock(): %w", err)
			}
			index = key.Index
		case kindAppQC:
			key, err := key.decodeAppQC()
			if err != nil {
				return types.Suffix{}, fmt.Errorf("key.decodeAppQC(): %w", err)
			}
			index = key.Index
			if !gotAppQC {
				first = index
				gotAppQC = true
			}
		case kindAppProp:
			key, err := key.decodeAppProposal()
			if err != nil {
				return types.Suffix{}, fmt.Errorf("key.decodeAppProposal(): %w", err)
			}
			index = key.Index
		default:
			return types.Suffix{}, fmt.Errorf("invalid primary ledger key %x", key)
		}
		if !gotAppQC || index >= first {
			value, err := it.GetValue()
			if err != nil {
				return types.Suffix{}, fmt.Errorf("it.GetValue(): %w", err)
			}
			switch key.kind {
			case kindBlock:
				block, err := decodeBlock(value)
				if err != nil {
					return types.Suffix{}, fmt.Errorf("decodeBlock(): %w", err)
				}
				suffix.Blocks = append(suffix.Blocks, block)
			case kindAppQC:
				qc, err := decodeAppQC(value)
				if err != nil {
					return types.Suffix{}, fmt.Errorf("decodeAppQC(): %w", err)
				}
				if qc.Proposal().Index() != index {
					return types.Suffix{}, types.ErrUnexpectedIndex
				}
				suffix.AppQCs = append(suffix.AppQCs, qc)
			case kindAppProp:
				proposal, err := decodeAppProposal(value)
				if err != nil {
					return types.Suffix{}, fmt.Errorf("decodeAppProposal(): %w", err)
				}
				if proposal.Index() != index {
					return types.Suffix{}, types.ErrUnexpectedIndex
				}
				suffix.AppProposals = append(suffix.AppProposals, proposal)
			}
		}
		if gotAppQC && key.kind == kindBlock && index <= first {
			break
		}
	}
	suffix.Blocks = slices.DeleteFunc(suffix.Blocks, func(block *types.RoadBlock) bool { return gotAppQC && block.QC().Index() < first })
	suffix.AppQCs = slices.DeleteFunc(suffix.AppQCs, func(qc *types.AppQC) bool { return gotAppQC && qc.Proposal().Index() < first })
	suffix.AppProposals = slices.DeleteFunc(suffix.AppProposals, func(proposal *types.AppProposal) bool { return gotAppQC && proposal.Index() < first })
	slices.Reverse(suffix.Blocks)
	slices.Reverse(suffix.AppQCs)
	slices.Reverse(suffix.AppProposals)
	return suffix, nil
}

// Flush makes all previously returned writes durable.
func (s *Store) Flush() error {
	if err := s.table.Flush(); err != nil {
		return fmt.Errorf("s.table.Flush(): %w", err)
	}
	return nil
}

// Close releases the underlying LittDB.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("s.db.Close(): %w", err)
	}
	return nil
}

func (s *Store) WriteBlock(block *types.RoadBlock) error {
	index := block.QC().Index()

	for status := range s.status.Lock() {
		current, ok := status.Get()
		if !ok {
			current = types.SuffixRange{
				First:           index,
				NextBlock:       index,
				NextAppQC:       index,
				NextAppProposal: index,
			}
		}
		if current.NextBlock != index {
			return fmt.Errorf("block index %d, expected %d: %w", index, current.NextBlock, types.ErrUnexpectedIndex)
		}
		record := encodeBlock(block)
		if err := s.table.Put(record.Key.bytes(), record.Value, record.Aliases...); err != nil {
			return fmt.Errorf("s.table.Put(%x): %w", record.Key.bytes(), err)
		}
		if !ok {
			// Reads below the first QC must return ErrPruned before and after reopening.
			s.setPruneWatermark(index)
		}
		current.NextBlock = index + 1
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

func (s *Store) WriteAppQC(appQC *types.AppQC) error {
	index := appQC.Proposal().Index()
	for status := range s.status.Lock() {
		current, ok := status.Get()
		if !ok || current.NextAppProposal <= index {
			return fmt.Errorf("AppQC %d: %w", index, types.ErrMissingAppProposal)
		}
		if current.NextAppQC != index {
			return fmt.Errorf("AppQC.Index() = %d, want %d: %w",
				index, current.NextAppQC, types.ErrUnexpectedIndex)
		}

		record := encodeAppQC(appQC)
		if err := s.table.Put(record.Key.bytes(), record.Value, record.Aliases...); err != nil {
			return fmt.Errorf("s.table.Put(%x): %w", record.Key.bytes(), err)
		}
		current.First = index
		current.NextAppQC = index + 1
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

func (s *Store) WriteAppProposal(appProposal *types.AppProposal) error {
	index := appProposal.Index()
	if index == utils.Max[types.RoadIndex]() {
		return fmt.Errorf("road index cannot advance: %w", types.ErrUnexpectedIndex)
	}
	for status := range s.status.Lock() {
		current, ok := status.Get()
		if !ok || current.NextBlock <= index {
			return fmt.Errorf("AppProposal %d: %w", index, types.ErrMissingQC)
		}
		if current.NextAppProposal != index {
			return fmt.Errorf("AppProposal.Index() = %v, want %v: %w",
				index, current.NextAppProposal, types.ErrUnexpectedIndex)
		}

		record := encodeAppProposal(appProposal)
		if err := s.table.Put(record.Key.bytes(), record.Value, record.Aliases...); err != nil {
			return fmt.Errorf("s.table.Put(%x): %w", record.Key.bytes(), err)
		}
		current.NextAppProposal = index + 1
		*status = utils.Some(current)
		return nil
	}
	panic("unreachable")
}

// ReadSuffix returns a validated recovery snapshot.
func (s *Store) ReadSuffix() (types.Suffix, error) {
	// Exclude writes and pruning until the snapshot is validated.
	for range s.status.Lock() {
		suffix, err := s.readSuffix()
		if err != nil {
			return types.Suffix{}, fmt.Errorf("s.readSuffix(): %w", err)
		}
		return validateSuffix(suffix)
	}
	panic("unreachable")
}

func (s *Store) ReadLaneBlock(pos types.LaneBlockPos) (utils.Option[*types.Block], error) {
	if pos.Index < s.First() {
		return utils.None[*types.Block](), types.ErrPruned
	}
	value, exists, err := s.getRecord((laneBlockKey{Index: pos.Index, Offset: pos.Offset}).encode())
	if err != nil {
		return utils.None[*types.Block](), fmt.Errorf("s.getRecord(): %w", err)
	}
	if !exists {
		return utils.None[*types.Block](), nil
	}
	block, err := decodeLaneBlock(value)
	if err != nil {
		return utils.None[*types.Block](), fmt.Errorf("decodeLaneBlock(): %w", err)
	}
	return utils.Some(block), nil
}

func (s *Store) ReadBlock(n types.RoadIndex) (utils.Option[*types.RoadBlock], error) {
	if n < s.First() {
		return utils.None[*types.RoadBlock](), types.ErrPruned
	}
	value, exists, err := s.getRecord((blockKey{Index: n}).encode())
	if err != nil {
		return utils.None[*types.RoadBlock](), fmt.Errorf("s.getRecord(): %w", err)
	}
	if !exists {
		return utils.None[*types.RoadBlock](), nil
	}
	block, err := decodeBlock(value)
	if err != nil {
		return utils.None[*types.RoadBlock](), fmt.Errorf("decodeBlock(): %w", err)
	}
	return utils.Some(block), nil
}

func (s *Store) ReadBlockByHash(hash types.RoadBlockHeaderHash) (utils.Option[*types.RoadBlock], error) {
	value, exists, err := s.getRecord((blockHashKey{Hash: hash}).encode())
	if err != nil {
		return utils.None[*types.RoadBlock](), fmt.Errorf("s.getRecord(): %w", err)
	}
	if !exists {
		return utils.None[*types.RoadBlock](), nil
	}
	// Pruned hash lookups do not decode values that are no longer served.
	if len(value) >= 8 && types.RoadIndex(binary.BigEndian.Uint64(value)) < s.First() {
		return utils.None[*types.RoadBlock](), nil
	}
	block, err := decodeBlock(value)
	if err != nil {
		return utils.None[*types.RoadBlock](), fmt.Errorf("decodeBlock(): %w", err)
	}
	if block.QC().Index() < s.First() {
		return utils.None[*types.RoadBlock](), nil
	}
	if block.Header().Hash() != hash {
		return utils.None[*types.RoadBlock](), types.ErrQCBlocksMismatch
	}
	return utils.Some(block), nil
}

func (s *Store) ReadQC(n types.RoadIndex) (utils.Option[*types.FullCommitQC], error) {
	// Below-watermark blocks are not served, so neither is their covering QC.
	if n < s.First() {
		return utils.None[*types.FullCommitQC](), types.ErrPruned
	}
	value, exists, err := s.getRecord((qcKey{Index: n}).encode())
	if err != nil {
		return utils.None[*types.FullCommitQC](), fmt.Errorf("s.getRecord(): %w", err)
	}
	if !exists {
		return utils.None[*types.FullCommitQC](), nil
	}
	qc, err := decodeQC(value)
	if err != nil {
		return utils.None[*types.FullCommitQC](), fmt.Errorf("decodeQC(): %w", err)
	}
	return utils.Some(qc), nil
}

func (s *Store) ReadAppProposal(n types.RoadIndex) (utils.Option[*types.AppProposal], error) {
	if n < s.First() {
		return utils.None[*types.AppProposal](), types.ErrPruned
	}
	value, exists, err := s.getRecord((appProposalKey{Index: n}).encode())
	if err != nil {
		return utils.None[*types.AppProposal](), fmt.Errorf("s.getRecord(): %w", err)
	}
	if !exists {
		return utils.None[*types.AppProposal](), nil
	}
	appProposal, err := decodeAppProposal(value)
	if err != nil {
		return utils.None[*types.AppProposal](), fmt.Errorf("decodeAppProposal(): %w", err)
	}
	return utils.Some(appProposal), nil
}

func (s *Store) ReadAppQC(n types.RoadIndex) (utils.Option[*types.AppQC], error) {
	if n < s.First() {
		return utils.None[*types.AppQC](), types.ErrPruned
	}
	value, exists, err := s.getRecord((appQCKey{Index: n}).encode())
	if err != nil {
		return utils.None[*types.AppQC](), fmt.Errorf("s.getRecord(): %w", err)
	}
	if !exists {
		return utils.None[*types.AppQC](), nil
	}
	appQC, err := decodeAppQC(value)
	if err != nil {
		return utils.None[*types.AppQC](), fmt.Errorf("decodeAppQC(): %w", err)
	}
	return utils.Some(appQC), nil
}

func (s *Store) Status() utils.Option[types.SuffixRange] {
	for status := range s.status.Lock() {
		return *status
	}
	panic("unreachable")
}

// restore initializes the cursors and floor from retained records.
func (s *Store) restore(first types.RoadIndex, suffix types.Suffix) error {
	suffix, err := validateSuffix(suffix)
	if err != nil {
		return err
	}
	for status := range s.status.Lock() {
		*status = suffix.Status
		s.first.Store(uint64(first))
		Global.pruneWatermarkAt().Set(utils.Clamp[int64](first))
	}
	return nil
}

// validateSuffix checks that recovery streams are contiguous and have their prerequisites.
func validateSuffix(suffix types.Suffix) (types.Suffix, error) {
	if len(suffix.AppQCs) > 1 {
		return types.Suffix{}, fmt.Errorf("too large suffix")
	}
	if len(suffix.Blocks) == 0 {
		if len(suffix.AppQCs) > 0 || len(suffix.AppProposals) > 0 {
			return types.Suffix{}, types.ErrMissingQC
		}
		suffix.Status = utils.None[types.SuffixRange]()
		return suffix, nil
	}
	first := suffix.Blocks[0].QC().Index()
	status := types.SuffixRange{First: first, NextBlock: first, NextAppProposal: first, NextAppQC: first}
	for _, block := range suffix.Blocks {
		if block.QC().Index() != status.NextBlock {
			return types.Suffix{}, types.ErrUnexpectedIndex
		}
		status.NextBlock++
	}
	for _, proposal := range suffix.AppProposals {
		if proposal.Index() >= status.NextBlock {
			return types.Suffix{}, types.ErrMissingQC
		}
		if proposal.Index() != status.NextAppProposal {
			return types.Suffix{}, types.ErrUnexpectedIndex
		}
		status.NextAppProposal++
	}
	for _, qc := range suffix.AppQCs {
		index := qc.Proposal().Index()
		if index >= status.NextAppProposal {
			return types.Suffix{}, types.ErrMissingAppProposal
		}
		if index != status.NextAppQC {
			return types.Suffix{}, types.ErrUnexpectedIndex
		}
		status.First = index
		status.NextAppQC++
	}
	suffix.Status = utils.Some(status)
	return suffix, nil
}
