// Package blockstore provides LittDB-backed storage for Autobahn consensus.
package blockstore

import (
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"

	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"
	littdb "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// tableName is the single table holding every record kind, despite the name.
//
// It is persisted layout rather than just an identifier: littdb stores a table's data at
// <root>/<tableName>/segments, so changing it makes New open a fresh empty table and leaves
// data under the old name unreachable.
const tableName = "blocks"

var _ controllertypes.PrunableStore = (*Store)(nil)

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

// open opens the retained record table and restores its reclamation floor.
func open(config *Config) (*Store, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid block db config: %w", err)
	}
	db, err := littbuilder.NewDB(config.Litt)
	if err != nil {
		return nil, fmt.Errorf("failed to open litt db: %w", err)
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
		return nil, fmt.Errorf("failed to build ledger table: %w", err)
	}
	s.table = table
	first, err := s.recoverWatermark()
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to recover watermark: %w", err)
	}
	if err := s.restore(first, types.Suffix{}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// New opens a typed consensus block store and restores its write cursors.
func New(config *Config) (*Store, error) {
	s, err := open(config)
	if err != nil {
		return nil, err
	}
	suffix, err := s.readSuffix()
	if err == nil {
		err = s.restore(s.First(), suffix)
	}
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("recover block store: %w", err)
	}
	return s, nil
}

// recoverWatermark restores the floor from the oldest surviving SuperBlock.
// A non-empty store without a SuperBlock cannot supply a recovery anchor.
func (s *Store) recoverWatermark() (types.RoadIndex, error) {
	it, err := s.table.Iterator(false)
	if err != nil {
		return 0, fmt.Errorf("failed to open record iterator: %w", err)
	}
	defer func() { _ = it.Close() }()
	empty := true
	for {
		ok, err := it.Next()
		if err != nil {
			return 0, fmt.Errorf("failed to advance record iterator: %w", err)
		}
		if !ok {
			break
		}
		key, isPrimary, err := it.GetKey()
		if err != nil {
			return 0, fmt.Errorf("failed to read record key: %w", err)
		}
		if !isPrimary {
			continue
		}
		recordKey, err := decodeLedgerKey(key)
		if err != nil {
			return 0, fmt.Errorf("failed to parse record key: %w", err)
		}
		empty = false
		switch recordKey.kind {
		case kindBlock:
			key, err := recordKey.decodeBlock()
			if err != nil {
				return 0, err
			}
			return key.Index, nil
		case kindAppQC:
			if _, err := recordKey.decodeAppQC(); err != nil {
				return 0, err
			}
		case kindAppProp:
			if _, err := recordKey.decodeAppProposal(); err != nil {
				return 0, err
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
	return s.table.Get(key.bytes())
}

// getLaneBlock returns the lane block at a road index and header offset.
func (s *Store) getLaneBlock(n types.RoadIndex, offset uint64) ([]byte, bool, error) {
	return s.getRecord((laneBlockKey{Index: n, Offset: offset}).encode())
}

// readSuffix reads complete primary records from the latest AppQC onward.
func (s *Store) readSuffix() (types.Suffix, error) {
	it, err := s.table.Iterator(true)
	if err != nil {
		return types.Suffix{}, fmt.Errorf("failed to open record iterator: %w", err)
	}
	defer func() { _ = it.Close() }()
	var suffix types.Suffix

	var first types.RoadIndex
	gotAppQC := false
	for {
		ok, err := it.Next()
		if err != nil {
			return types.Suffix{}, fmt.Errorf("failed to advance record iterator: %w", err)
		}
		if !ok {
			break
		}
		rawKey, isPrimary, err := it.GetKey()
		if err != nil {
			return types.Suffix{}, fmt.Errorf("failed to read record key: %w", err)
		}
		if !isPrimary {
			continue
		}
		key, err := decodeLedgerKey(rawKey)
		if err != nil {
			return types.Suffix{}, fmt.Errorf("failed to parse record key: %w", err)
		}
		var index types.RoadIndex
		switch key.kind {
		case kindBlock:
			key, err := key.decodeBlock()
			if err != nil {
				return types.Suffix{}, err
			}
			index = key.Index
		case kindAppQC:
			key, err := key.decodeAppQC()
			if err != nil {
				return types.Suffix{}, err
			}
			index = key.Index
			if !gotAppQC {
				first = index
				gotAppQC = true
			}
		case kindAppProp:
			key, err := key.decodeAppProposal()
			if err != nil {
				return types.Suffix{}, err
			}
			index = key.Index
		default:
			return types.Suffix{}, fmt.Errorf("invalid primary ledger key %x", key)
		}
		if !gotAppQC || index >= first {
			value, err := it.GetValue()
			if err != nil {
				return types.Suffix{}, fmt.Errorf("failed to read record value: %w", err)
			}
			switch key.kind {
			case kindBlock:
				block, err := decodeBlock(value)
				if err != nil {
					return types.Suffix{}, err
				}
				suffix.Blocks = append(suffix.Blocks, block)
			case kindAppQC:
				qc, err := decodeAppQC(value)
				if err != nil {
					return types.Suffix{}, err
				}
				if qc.Proposal().Index() != index {
					return types.Suffix{}, types.ErrUnexpectedIndex
				}
				suffix.AppQCs = append(suffix.AppQCs, qc)
			case kindAppProp:
				proposal, err := decodeAppProposal(value)
				if err != nil {
					return types.Suffix{}, err
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

// reclaimable reports whether a key is eligible for reclamation below watermark.
// Hash aliases are always eligible; their primary record and numbered aliases govern retention.
func (k ledgerKey) reclaimable(watermark types.RoadIndex) (bool, error) {
	var index types.RoadIndex
	switch k.kind {
	case kindBlock:
		key, err := k.decodeBlock()
		if err != nil {
			return false, err
		}
		index = key.Index
	case kindQC:
		key, err := k.decodeQC()
		if err != nil {
			return false, err
		}
		index = key.Index
	case kindAppQC:
		key, err := k.decodeAppQC()
		if err != nil {
			return false, err
		}
		index = key.Index
	case kindAppProp:
		key, err := k.decodeAppProposal()
		if err != nil {
			return false, err
		}
		index = key.Index
	case kindLaneBlock:
		key, err := k.decodeLaneBlock()
		if err != nil {
			return false, err
		}
		index = key.Index
	case kindBlockHash:
		_, err := k.decodeBlockHash()
		return err == nil, err
	default:
		return false, fmt.Errorf("unknown ledger key kind %d", k.kind)
	}
	return index < watermark, nil
}

// gcFilter reports whether a ledger key is below the reclamation floor.
func (s *Store) gcFilter(key []byte, _ bool) (bool, error) {
	parsed, err := decodeLedgerKey(key)
	if err != nil {
		return false, fmt.Errorf("failed to parse ledger key: %w", err)
	}
	return parsed.reclaimable(s.First())
}

// Flush makes all previously returned writes durable.
func (s *Store) Flush() error {
	if err := s.table.Flush(); err != nil {
		return fmt.Errorf("failed to flush ledger table: %w", err)
	}
	return nil
}

// Close releases the underlying LittDB.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("failed to close litt db: %w", err)
	}
	return nil
}

// ForceGC runs synchronous garbage collection for this store.
func (s *Store) ForceGC() error {
	managed, ok := s.table.(littdb.ManagedTable)
	if !ok {
		return fmt.Errorf("table %q is not a ManagedTable", s.table.Name())
	}
	if err := managed.RunGC(); err != nil {
		return fmt.Errorf("failed to run GC on table %q: %w", s.table.Name(), err)
	}
	return nil
}

// getQC returns the serialized QC at road n.
func (s *Store) getQC(n types.RoadIndex) ([]byte, bool, error) {
	return s.getRecord((qcKey{Index: n}).encode())
}

// getAppQC returns the serialized application QC at road n.
func (s *Store) getAppQC(n types.RoadIndex) ([]byte, bool, error) {
	return s.getRecord((appQCKey{Index: n}).encode())
}

// getAppProposal returns the serialized application proposal at road n.
func (s *Store) getAppProposal(n types.RoadIndex) ([]byte, bool, error) {
	return s.getRecord((appProposalKey{Index: n}).encode())
}
