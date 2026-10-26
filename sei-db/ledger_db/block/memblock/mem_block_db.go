package memblock

import (
	"slices"
	"sync"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

var _ blocktypes.BlockDB = (*blockDB)(nil)

type blockDB struct {
	mu        sync.RWMutex
	suffix    blocktypes.Suffix
	blocks    map[blocktypes.BlockNumber]blocktypes.BlockRecord
	hashes    map[blocktypes.BlockHash]blocktypes.BlockNumber
	appQCs    map[blocktypes.BlockNumber]blocktypes.AppQCRecord
	proposals map[blocktypes.BlockNumber]blocktypes.AppProposalRecord
	watermark blocktypes.BlockNumber
}

// NewBlockDB returns an in-memory blocktypes.BlockDB.
func NewBlockDB() blocktypes.BlockDB {
	return &blockDB{
		blocks:    make(map[blocktypes.BlockNumber]blocktypes.BlockRecord),
		hashes:    make(map[blocktypes.BlockHash]blocktypes.BlockNumber),
		appQCs:    make(map[blocktypes.BlockNumber]blocktypes.AppQCRecord),
		proposals: make(map[blocktypes.BlockNumber]blocktypes.AppProposalRecord),
	}
}

func (s *blockDB) PutBlock(record blocktypes.BlockRecord) error {
	record.QC = slices.Clone(record.QC)
	record.LaneBlocks = slices.Clone(record.LaneBlocks)
	for i, block := range record.LaneBlocks {
		record.LaneBlocks[i] = slices.Clone(block)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suffix.Blocks = append(s.suffix.Blocks, record)
	s.blocks[record.Index] = record
	s.hashes[record.Hash] = record.Index
	return nil
}

func (s *blockDB) PutAppQC(record blocktypes.AppQCRecord) error {
	record.Data = slices.Clone(record.Data)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suffix.AppQCs = append(s.suffix.AppQCs, record)
	s.appQCs[record.Index] = record
	return nil
}

func (s *blockDB) PutAppProposal(record blocktypes.AppProposalRecord) error {
	record.Data = slices.Clone(record.Data)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suffix.AppProposals = append(s.suffix.AppProposals, record)
	s.proposals[record.Index] = record
	return nil
}

func (s *blockDB) GetQC(n blocktypes.BlockNumber) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.blocks[n]
	return record.QC, ok, nil
}

func (s *blockDB) GetBlockByNumber(n blocktypes.BlockNumber) (blocktypes.BlockRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.blocks[n]
	return record, ok, nil
}

func (s *blockDB) GetBlockByHash(hash blocktypes.BlockHash) (blocktypes.BlockRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index, ok := s.hashes[hash]
	if !ok {
		return blocktypes.BlockRecord{}, false, nil
	}
	return s.blocks[index], true, nil
}

func (s *blockDB) GetLaneBlock(n blocktypes.BlockNumber, offset uint64) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.blocks[n]
	if !ok || offset >= uint64(len(record.LaneBlocks)) {
		return nil, false, nil
	}
	return record.LaneBlocks[offset], true, nil
}

func (s *blockDB) GetAppQC(n blocktypes.BlockNumber) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.appQCs[n]
	return record.Data, ok, nil
}

func (s *blockDB) GetAppProposal(n blocktypes.BlockNumber) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.proposals[n]
	return record.Data, ok, nil
}

func (s *blockDB) ReadSuffix() (blocktypes.Suffix, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var first blocktypes.BlockNumber
	if len(s.suffix.AppQCs) > 0 {
		first = s.suffix.AppQCs[len(s.suffix.AppQCs)-1].Index
	}
	var suffix blocktypes.Suffix
	for _, record := range s.suffix.Blocks {
		if record.Index >= first {
			suffix.Blocks = append(suffix.Blocks, record)
		}
	}
	for _, record := range s.suffix.AppQCs {
		if record.Index >= first {
			suffix.AppQCs = append(suffix.AppQCs, record)
		}
	}
	for _, record := range s.suffix.AppProposals {
		if record.Index >= first {
			suffix.AppProposals = append(suffix.AppProposals, record)
		}
	}
	return suffix, nil
}

func (s *blockDB) GetPruneWatermark() blocktypes.BlockNumber {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.watermark
}

func (s *blockDB) SetPruneWatermark(n blocktypes.BlockNumber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= s.watermark {
		return
	}
	s.watermark = n
	s.suffix.Blocks = slices.DeleteFunc(s.suffix.Blocks, func(record blocktypes.BlockRecord) bool {
		if record.Index >= n {
			return false
		}
		delete(s.blocks, record.Index)
		delete(s.hashes, record.Hash)
		return true
	})
	s.suffix.AppQCs = slices.DeleteFunc(s.suffix.AppQCs, func(record blocktypes.AppQCRecord) bool {
		if record.Index >= n {
			return false
		}
		delete(s.appQCs, record.Index)
		return true
	})
	s.suffix.AppProposals = slices.DeleteFunc(s.suffix.AppProposals, func(record blocktypes.AppProposalRecord) bool {
		if record.Index >= n {
			return false
		}
		delete(s.proposals, record.Index)
		return true
	})
}
func (s *blockDB) Flush() error { return nil }
func (s *blockDB) Close() error { return nil }
