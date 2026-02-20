package types

import (
	"time"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// RequestLoadLatest is the plain Go replacement for the legacy protobuf message.
type RequestLoadLatest struct{}

// ResponseLoadLatest is the plain Go replacement for the legacy protobuf message.
type ResponseLoadLatest struct{}

// RequestListSnapshots is the plain Go replacement for the legacy protobuf message.
type RequestListSnapshots struct{}

// ResponseListSnapshots is the plain Go replacement for the legacy protobuf message.
type ResponseListSnapshots struct {
	Snapshots []*Snapshot
}

// RequestOfferSnapshot is the plain Go replacement for the legacy protobuf message.
type RequestOfferSnapshot struct {
	Snapshot *Snapshot
	AppHash  []byte
}

// ResponseOfferSnapshot is the plain Go replacement for the legacy protobuf message.
type ResponseOfferSnapshot struct {
	Result ResponseOfferSnapshot_Result
}

// ResponseOfferSnapshot_Result mirrors the historical protobuf enum.
type ResponseOfferSnapshot_Result int32

const (
	ResponseOfferSnapshot_UNKNOWN       ResponseOfferSnapshot_Result = 0
	ResponseOfferSnapshot_ACCEPT        ResponseOfferSnapshot_Result = 1
	ResponseOfferSnapshot_ABORT         ResponseOfferSnapshot_Result = 2
	ResponseOfferSnapshot_REJECT        ResponseOfferSnapshot_Result = 3
	ResponseOfferSnapshot_REJECT_FORMAT ResponseOfferSnapshot_Result = 4
	ResponseOfferSnapshot_REJECT_SENDER ResponseOfferSnapshot_Result = 5
)

// RequestLoadSnapshotChunk is the plain Go replacement for the legacy protobuf message.
type RequestLoadSnapshotChunk struct {
	Height uint64
	Format uint32
	Chunk  uint32
}

// ResponseLoadSnapshotChunk is the plain Go replacement for the legacy protobuf message.
type ResponseLoadSnapshotChunk struct {
	Chunk []byte
}

// RequestApplySnapshotChunk is the plain Go replacement for the legacy protobuf message.
type RequestApplySnapshotChunk struct {
	Index  uint32
	Chunk  []byte
	Sender string
}

// ResponseApplySnapshotChunk is the plain Go replacement for the legacy protobuf message.
type ResponseApplySnapshotChunk struct {
	Result        ResponseApplySnapshotChunk_Result
	RefetchChunks []uint32
	RejectSenders []string
}

// ResponseApplySnapshotChunk_Result mirrors the historical protobuf enum.
type ResponseApplySnapshotChunk_Result int32

const (
	ResponseApplySnapshotChunk_UNKNOWN         ResponseApplySnapshotChunk_Result = 0
	ResponseApplySnapshotChunk_ACCEPT          ResponseApplySnapshotChunk_Result = 1
	ResponseApplySnapshotChunk_ABORT           ResponseApplySnapshotChunk_Result = 2
	ResponseApplySnapshotChunk_RETRY           ResponseApplySnapshotChunk_Result = 3
	ResponseApplySnapshotChunk_RETRY_SNAPSHOT  ResponseApplySnapshotChunk_Result = 4
	ResponseApplySnapshotChunk_REJECT_SNAPSHOT ResponseApplySnapshotChunk_Result = 5
)

// ResponseProcessProposal is the plain Go replacement for the legacy protobuf message.
type ResponseProcessProposal struct {
	Status                ResponseProcessProposal_ProposalStatus
	AppHash               []byte
	TxResults             []*ExecTxResult
	ValidatorUpdates      []ValidatorUpdate
	ConsensusParamUpdates *tmproto.ConsensusParams
}

// ResponseProcessProposal_ProposalStatus mirrors the historical protobuf enum.
type ResponseProcessProposal_ProposalStatus int32

const (
	ResponseProcessProposal_UNKNOWN ResponseProcessProposal_ProposalStatus = 0
	ResponseProcessProposal_ACCEPT  ResponseProcessProposal_ProposalStatus = 1
	ResponseProcessProposal_REJECT  ResponseProcessProposal_ProposalStatus = 2
)

func (s ResponseProcessProposal_ProposalStatus) String() string {
	switch s {
	case ResponseProcessProposal_UNKNOWN:
		return "UNKNOWN"
	case ResponseProcessProposal_ACCEPT:
		return "ACCEPT"
	case ResponseProcessProposal_REJECT:
		return "REJECT"
	default:
		return "UNKNOWN"
	}
}

// RequestProcessProposal is the plain Go replacement for the legacy protobuf message.
type RequestProcessProposal struct {
	Txs                   [][]byte
	ProposedLastCommit    CommitInfo
	ByzantineValidators   []Misbehavior
	Hash                  []byte
	Height                int64
	Time                  time.Time
	NextValidatorsHash    []byte
	ProposerAddress       []byte
	AppHash               []byte
	ValidatorsHash        []byte
	ConsensusHash         []byte
	DataHash              []byte
	EvidenceHash          []byte
	LastBlockHash         []byte
	LastBlockPartSetTotal int64
	LastBlockPartSetHash  []byte
	LastCommitHash        []byte
	LastResultsHash       []byte
}

func (m *RequestProcessProposal) GetTxs() [][]byte {
	if m == nil {
		return nil
	}
	return m.Txs
}

func (m *RequestProcessProposal) GetProposedLastCommit() CommitInfo {
	if m == nil {
		return CommitInfo{}
	}
	return m.ProposedLastCommit
}

func (m *RequestProcessProposal) GetByzantineValidators() []Misbehavior {
	if m == nil {
		return nil
	}
	return m.ByzantineValidators
}

func (m *RequestProcessProposal) GetHash() []byte {
	if m == nil {
		return nil
	}
	return m.Hash
}

func (m *RequestProcessProposal) GetHeight() int64 {
	if m == nil {
		return 0
	}
	return m.Height
}

func (m *RequestProcessProposal) GetTime() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.Time
}

func (m *RequestProcessProposal) GetNextValidatorsHash() []byte {
	if m == nil {
		return nil
	}
	return m.NextValidatorsHash
}

func (m *RequestProcessProposal) GetProposerAddress() []byte {
	if m == nil {
		return nil
	}
	return m.ProposerAddress
}

func (m *RequestProcessProposal) GetAppHash() []byte {
	if m == nil {
		return nil
	}
	return m.AppHash
}

func (m *RequestProcessProposal) GetValidatorsHash() []byte {
	if m == nil {
		return nil
	}
	return m.ValidatorsHash
}

func (m *RequestProcessProposal) GetConsensusHash() []byte {
	if m == nil {
		return nil
	}
	return m.ConsensusHash
}

func (m *RequestProcessProposal) GetDataHash() []byte {
	if m == nil {
		return nil
	}
	return m.DataHash
}

func (m *RequestProcessProposal) GetEvidenceHash() []byte {
	if m == nil {
		return nil
	}
	return m.EvidenceHash
}

func (m *RequestProcessProposal) GetLastBlockHash() []byte {
	if m == nil {
		return nil
	}
	return m.LastBlockHash
}

func (m *RequestProcessProposal) GetLastBlockPartSetTotal() int64 {
	if m == nil {
		return 0
	}
	return m.LastBlockPartSetTotal
}

func (m *RequestProcessProposal) GetLastBlockPartSetHash() []byte {
	if m == nil {
		return nil
	}
	return m.LastBlockPartSetHash
}

func (m *RequestProcessProposal) GetLastCommitHash() []byte {
	if m == nil {
		return nil
	}
	return m.LastCommitHash
}

func (m *RequestProcessProposal) GetLastResultsHash() []byte {
	if m == nil {
		return nil
	}
	return m.LastResultsHash
}

// RequestFinalizeBlock is the plain Go replacement for the legacy protobuf message.
type RequestFinalizeBlock struct {
	Txs                   [][]byte
	DecidedLastCommit     CommitInfo
	ByzantineValidators   []Misbehavior
	Hash                  []byte
	Height                int64
	Time                  time.Time
	NextValidatorsHash    []byte
	ProposerAddress       []byte
	AppHash               []byte
	ValidatorsHash        []byte
	ConsensusHash         []byte
	DataHash              []byte
	EvidenceHash          []byte
	LastBlockHash         []byte
	LastBlockPartSetTotal int64
	LastBlockPartSetHash  []byte
	LastCommitHash        []byte
	LastResultsHash       []byte
}

func (m *RequestFinalizeBlock) GetTxs() [][]byte {
	if m == nil {
		return nil
	}
	return m.Txs
}

func (m *RequestFinalizeBlock) GetDecidedLastCommit() CommitInfo {
	if m == nil {
		return CommitInfo{}
	}
	return m.DecidedLastCommit
}

func (m *RequestFinalizeBlock) GetByzantineValidators() []Misbehavior {
	if m == nil {
		return nil
	}
	return m.ByzantineValidators
}

func (m *RequestFinalizeBlock) GetHash() []byte {
	if m == nil {
		return nil
	}
	return m.Hash
}

func (m *RequestFinalizeBlock) GetHeight() int64 {
	if m == nil {
		return 0
	}
	return m.Height
}

func (m *RequestFinalizeBlock) GetTime() time.Time {
	if m == nil {
		return time.Time{}
	}
	return m.Time
}

func (m *RequestFinalizeBlock) GetNextValidatorsHash() []byte {
	if m == nil {
		return nil
	}
	return m.NextValidatorsHash
}

func (m *RequestFinalizeBlock) GetProposerAddress() []byte {
	if m == nil {
		return nil
	}
	return m.ProposerAddress
}

func (m *RequestFinalizeBlock) GetAppHash() []byte {
	if m == nil {
		return nil
	}
	return m.AppHash
}

func (m *RequestFinalizeBlock) GetValidatorsHash() []byte {
	if m == nil {
		return nil
	}
	return m.ValidatorsHash
}

func (m *RequestFinalizeBlock) GetConsensusHash() []byte {
	if m == nil {
		return nil
	}
	return m.ConsensusHash
}

func (m *RequestFinalizeBlock) GetDataHash() []byte {
	if m == nil {
		return nil
	}
	return m.DataHash
}

func (m *RequestFinalizeBlock) GetEvidenceHash() []byte {
	if m == nil {
		return nil
	}
	return m.EvidenceHash
}

func (m *RequestFinalizeBlock) GetLastBlockHash() []byte {
	if m == nil {
		return nil
	}
	return m.LastBlockHash
}

func (m *RequestFinalizeBlock) GetLastBlockPartSetTotal() int64 {
	if m == nil {
		return 0
	}
	return m.LastBlockPartSetTotal
}

func (m *RequestFinalizeBlock) GetLastBlockPartSetHash() []byte {
	if m == nil {
		return nil
	}
	return m.LastBlockPartSetHash
}

func (m *RequestFinalizeBlock) GetLastCommitHash() []byte {
	if m == nil {
		return nil
	}
	return m.LastCommitHash
}

func (m *RequestFinalizeBlock) GetLastResultsHash() []byte {
	if m == nil {
		return nil
	}
	return m.LastResultsHash
}
