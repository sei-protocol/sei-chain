package blockstore

import (
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore/internal/blocktest"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

type recordStore struct{ *Store }

// OpenRecords opens the record view used by persistence tests.
func OpenRecords(config *Config) (blocktest.DB, error) {
	s, err := open(config)
	if err != nil {
		return nil, err
	}
	return recordStore{s}, nil
}

func (s recordStore) PutBlock(r blocktest.BlockRecord) error {
	record := encodeFixtureBlock(r)
	if err := s.table.Put(record.Key.bytes(), record.Value, record.Aliases...); err != nil {
		return fmt.Errorf("failed to put ledger record: %w", err)
	}
	return nil
}
func (s recordStore) PutAppQC(r blocktest.AppQCRecord) error {
	record := record{Key: (appQCKey{Index: r.Index}).encode()}
	record.appendRawBytes(r.Data)
	if err := s.table.Put(record.Key.bytes(), record.Value, record.Aliases...); err != nil {
		return fmt.Errorf("failed to put ledger record: %w", err)
	}
	return nil
}
func (s recordStore) PutAppProposal(r blocktest.AppProposalRecord) error {
	record := record{Key: (appProposalKey{Index: r.Index}).encode()}
	record.appendRawBytes(r.Data)
	if err := s.table.Put(record.Key.bytes(), record.Value, record.Aliases...); err != nil {
		return fmt.Errorf("failed to put ledger record: %w", err)
	}
	return nil
}
func (s recordStore) GetQC(n types.RoadIndex) ([]byte, bool, error)    { return s.getQC(n) }
func (s recordStore) GetAppQC(n types.RoadIndex) ([]byte, bool, error) { return s.getAppQC(n) }
func (s recordStore) GetAppProposal(n types.RoadIndex) ([]byte, bool, error) {
	return s.getAppProposal(n)
}
func (s recordStore) GetBlockByNumber(n types.RoadIndex) (blocktest.BlockRecord, bool, error) {
	return s.getFixtureBlock((blockKey{Index: n}).encode())
}
func (s recordStore) GetBlockByHash(h types.RoadBlockHeaderHash) (blocktest.BlockRecord, bool, error) {
	return s.getFixtureBlock((blockHashKey{Hash: h}).encode())
}
func (s recordStore) GetLaneBlock(n types.RoadIndex, o uint64) ([]byte, bool, error) {
	return s.getLaneBlock(n, o)
}

func (s recordStore) GetPruneWatermark() types.RoadIndex {
	return types.RoadIndex(s.First())
}
func (s recordStore) SetPruneWatermark(n types.RoadIndex) {
	if n <= types.RoadIndex(s.First()) {
		return
	}
	if err := s.restore(types.RoadIndex(n), types.Suffix{}); err != nil {
		panic(err)
	}
}

func (s recordStore) getFixtureBlock(key ledgerKey) (blocktest.BlockRecord, bool, error) {
	value, exists, err := s.getRecord(key)
	if err != nil || !exists {
		return blocktest.BlockRecord{}, exists, err
	}
	block, err := decodeFixtureBlock(value)
	if err != nil {
		return blocktest.BlockRecord{}, false, err
	}
	return block, true, nil
}

func encodeFixtureBlock(block blocktest.BlockRecord) record {
	size := uint64(12) + uint64(len(block.Hash)) + uint64(len(block.QC))
	for _, lane := range block.LaneBlocks {
		size += 4 + uint64(len(lane))
	}
	r := record{Key: (blockKey{Index: block.Index}).encode(), Value: make([]byte, 0, checkedSize(size))}
	r.appendUint64(uint64(block.Index))
	r.appendBlockHash(block.Hash)
	r.appendBytes(block.QC, (qcKey{Index: block.Index}).encode())
	for i, lane := range block.LaneBlocks {
		r.appendBytes(lane, (laneBlockKey{Index: block.Index, Offset: uint64(i)}).encode())
	}
	r.addAlias((blockHashKey{Hash: block.Hash}).encode())
	return r
}

// decodeFixtureBlock decodes a SuperBlock record, including an empty set of lane blocks.
func decodeFixtureBlock(value []byte) (blocktest.BlockRecord, error) {
	if len(value) < 8 {
		return blocktest.BlockRecord{}, fmt.Errorf("truncated road index")
	}
	block := blocktest.BlockRecord{Index: types.RoadIndex(binary.BigEndian.Uint64(value))}
	value = value[8:]
	var err error
	if len(value) < len(block.Hash) {
		return blocktest.BlockRecord{}, fmt.Errorf("truncated block hash")
	}
	copy(block.Hash[:], value[:len(block.Hash)])
	value = value[len(block.Hash):]
	block.QC, value, err = consumeValue(value)
	if err != nil {
		return blocktest.BlockRecord{}, err
	}
	for len(value) > 0 {
		var laneBlock []byte
		laneBlock, value, err = consumeValue(value)
		if err != nil {
			return blocktest.BlockRecord{}, err
		}
		block.LaneBlocks = append(block.LaneBlocks, laneBlock)
	}
	return block, nil
}

// readRecords reads complete primary records from the latest AppQC onward.
func (s recordStore) ReadSuffix() (blocktest.Suffix, error) {
	it, err := s.table.Iterator(true)
	if err != nil {
		return blocktest.Suffix{}, fmt.Errorf("failed to open record iterator: %w", err)
	}
	defer func() { _ = it.Close() }()
	var suffix blocktest.Suffix

	var first types.RoadIndex
	gotAppQC := false
	for {
		ok, err := it.Next()
		if err != nil {
			return blocktest.Suffix{}, fmt.Errorf("failed to advance record iterator: %w", err)
		}
		if !ok {
			break
		}
		rawKey, isPrimary, err := it.GetKey()
		if err != nil {
			return blocktest.Suffix{}, fmt.Errorf("failed to read record key: %w", err)
		}
		if !isPrimary {
			continue
		}
		key, err := decodeLedgerKey(rawKey)
		if err != nil {
			return blocktest.Suffix{}, fmt.Errorf("failed to parse record key: %w", err)
		}
		var index types.RoadIndex
		switch key.kind {
		case kindBlock:
			key, err := key.decodeBlock()
			if err != nil {
				return blocktest.Suffix{}, err
			}
			index = key.Index
		case kindAppQC:
			key, err := key.decodeAppQC()
			if err != nil {
				return blocktest.Suffix{}, err
			}
			index = key.Index
			if !gotAppQC {
				first = index
				gotAppQC = true
			}
		case kindAppProp:
			key, err := key.decodeAppProposal()
			if err != nil {
				return blocktest.Suffix{}, err
			}
			index = key.Index
		default:
			return blocktest.Suffix{}, fmt.Errorf("invalid primary ledger key %x", key)
		}
		if !gotAppQC || index >= first {
			value, err := it.GetValue()
			if err != nil {
				return blocktest.Suffix{}, fmt.Errorf("failed to read record value: %w", err)
			}
			switch key.kind {
			case kindBlock:
				block, err := decodeFixtureBlock(value)
				if err != nil {
					return blocktest.Suffix{}, err
				}
				suffix.Blocks = append(suffix.Blocks, block)
			case kindAppQC:
				suffix.AppQCs = append(suffix.AppQCs, blocktest.AppQCRecord{Index: index, Data: value})
			case kindAppProp:
				suffix.AppProposals = append(suffix.AppProposals, blocktest.AppProposalRecord{Index: index, Data: value})
			}
		}
		if gotAppQC && key.kind == kindBlock && index <= first {
			break
		}
	}
	suffix.Blocks = slices.DeleteFunc(suffix.Blocks, func(block blocktest.BlockRecord) bool { return gotAppQC && block.Index < first })
	suffix.AppQCs = slices.DeleteFunc(suffix.AppQCs, func(qc blocktest.AppQCRecord) bool { return gotAppQC && qc.Index < first })
	suffix.AppProposals = slices.DeleteFunc(suffix.AppProposals, func(proposal blocktest.AppProposalRecord) bool { return gotAppQC && proposal.Index < first })
	slices.Reverse(suffix.Blocks)
	slices.Reverse(suffix.AppQCs)
	slices.Reverse(suffix.AppProposals)
	return suffix, nil
}
