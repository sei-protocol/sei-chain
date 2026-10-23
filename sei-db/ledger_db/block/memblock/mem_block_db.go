package memblock

import (
	"math"
	"slices"
	"sync"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

var _ blocktypes.BlockDB = (*blockDB)(nil)

type blockDB struct {
	mu        sync.RWMutex
	suffix    blocktypes.Suffix
	groups    []blocktypes.QCGroupRecord
	qcs       map[blocktypes.BlockNumber]blocktypes.QCRecord
	blocks    map[blocktypes.BlockNumber]blocktypes.BlockRecord
	hashes    map[string]blocktypes.BlockRecord
	appQCs    map[blocktypes.BlockNumber]blocktypes.AppQCRecord
	proposals map[blocktypes.BlockNumber]blocktypes.AppProposalRecord
	watermark blocktypes.BlockNumber
}

// NewBlockDB returns an in-memory blocktypes.BlockDB.
func NewBlockDB() blocktypes.BlockDB {
	return &blockDB{
		qcs: make(map[blocktypes.BlockNumber]blocktypes.QCRecord), blocks: make(map[blocktypes.BlockNumber]blocktypes.BlockRecord), hashes: make(map[string]blocktypes.BlockRecord), appQCs: make(map[blocktypes.BlockNumber]blocktypes.AppQCRecord), proposals: make(map[blocktypes.BlockNumber]blocktypes.AppProposalRecord),
	}
}
func (s *blockDB) PutQC(qc blocktypes.QCGroupRecord) error {
	if err := qc.Validate(); err != nil {
		panic(err)
	}
	qc.QC.Data = slices.Clone(qc.QC.Data)
	qc.Blocks = slices.Clone(qc.Blocks)
	for i := range qc.Blocks {
		qc.Blocks[i].Hash = slices.Clone(qc.Blocks[i].Hash)
		qc.Blocks[i].Value = slices.Clone(qc.Blocks[i].Value)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups = append(s.groups, qc)
	for _, block := range qc.Blocks {
		s.qcs[block.Number] = qc.QC
		s.blocks[block.Number] = block
		s.hashes[string(block.Hash)] = block
	}
	return nil
}
func (s *blockDB) PutAppQC(qc blocktypes.AppQCRecord) error {
	if uint64(len(qc.Data))+16 >= math.MaxUint32 {
		panic("app record exceeds value size limit")
	}
	qc.Data = slices.Clone(qc.Data)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suffix.AppQCs = append(s.suffix.AppQCs, qc)
	for n := qc.First; n < qc.Next; n++ {
		s.appQCs[n] = qc
	}
	return nil
}
func (s *blockDB) PutAppProposal(proposal blocktypes.AppProposalRecord) error {
	if uint64(len(proposal.Data))+16 >= math.MaxUint32 {
		panic("app record exceeds value size limit")
	}
	proposal.Data = slices.Clone(proposal.Data)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suffix.AppProposals = append(s.suffix.AppProposals, proposal)
	for n := proposal.First; n < proposal.Next; n++ {
		s.proposals[n] = proposal
	}
	return nil
}
func (s *blockDB) GetQC(n blocktypes.BlockNumber) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	qc, ok := s.qcs[n]
	return qc.Data, ok, nil
}
func (s *blockDB) GetBlock(n blocktypes.BlockNumber) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	block, ok := s.blocks[n]
	return block.Value, ok, nil
}
func (s *blockDB) GetBlockByHash(hash blocktypes.BlockHash) (blocktypes.BlockNumber, []byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	block, ok := s.hashes[string(hash)]
	return block.Number, block.Value, ok, nil
}
func (s *blockDB) GetAppQC(n blocktypes.BlockNumber) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	qc, ok := s.appQCs[n]
	return qc.Data, ok, nil
}
func (s *blockDB) GetAppProposal(n blocktypes.BlockNumber) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	proposal, ok := s.proposals[n]
	return proposal.Data, ok, nil
}
func (s *blockDB) ReadSuffix() (blocktypes.Suffix, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var first blocktypes.BlockNumber
	if len(s.suffix.AppQCs) > 0 {
		first = s.suffix.AppQCs[len(s.suffix.AppQCs)-1].First
	}
	var suffix blocktypes.Suffix
	for _, qc := range s.groups {
		if qc.Blocks[0].Number >= first {
			suffix.QCs = append(suffix.QCs, qc.QC)
			for _, block := range qc.Blocks {
				suffix.Blocks = append(suffix.Blocks, blocktypes.BlockRecord{Number: block.Number, Value: block.Value})
			}
		}
	}
	for _, qc := range s.suffix.AppQCs {
		if qc.First >= first {
			suffix.AppQCs = append(suffix.AppQCs, qc)
		}
	}
	for _, proposal := range s.suffix.AppProposals {
		if proposal.First >= first {
			suffix.AppProposals = append(suffix.AppProposals, proposal)
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
	s.groups = slices.DeleteFunc(s.groups, func(qc blocktypes.QCGroupRecord) bool {
		for _, block := range qc.Blocks {
			if block.Number >= n {
				return false
			}
		}
		for _, block := range qc.Blocks {
			delete(s.qcs, block.Number)
			delete(s.blocks, block.Number)
			delete(s.hashes, string(block.Hash))
		}
		return true
	})
	s.suffix.AppQCs = slices.DeleteFunc(s.suffix.AppQCs, func(qc blocktypes.AppQCRecord) bool {
		if qc.Next > n {
			return false
		}
		for i := qc.First; i < qc.Next; i++ {
			delete(s.appQCs, i)
		}
		return true
	})
	s.suffix.AppProposals = slices.DeleteFunc(s.suffix.AppProposals, func(proposal blocktypes.AppProposalRecord) bool {
		if proposal.Next > n {
			return false
		}
		for i := proposal.First; i < proposal.Next; i++ {
			delete(s.proposals, i)
		}
		return true
	})
}
func (s *blockDB) Flush() error { return nil }
func (s *blockDB) Close() error { return nil }
