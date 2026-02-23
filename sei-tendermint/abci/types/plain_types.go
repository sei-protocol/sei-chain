package types

import (
	"time"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// RequestListSnapshots is emitted at the start of state sync to ask the application
// which previously committed snapshots are available for peers to download.
type RequestListSnapshots struct{}

// ResponseListSnapshots returns the snapshot metadata advertised by the
// application so Tendermint can decide which snapshot to fetch.
type ResponseListSnapshots struct {
	Snapshots []*Snapshot
}

// RequestOfferSnapshot propagates a snapshot offered by a peer so the
// application can inspect the metadata and the peer's app hash before accepting.
type RequestOfferSnapshot struct {
	Snapshot *Snapshot
	AppHash  []byte
}

// ResponseOfferSnapshot instructs Tendermint how to proceed with the offered
// snapshot (accept, reject, retry, etc.).
type ResponseOfferSnapshot struct {
	Result ResponseOfferSnapshot_Result
}

// ResponseOfferSnapshot_Result enumerates the possible decisions an
// application can take when a snapshot offer is received.
type ResponseOfferSnapshot_Result int32

const (
	ResponseOfferSnapshot_UNKNOWN ResponseOfferSnapshot_Result = iota
	ResponseOfferSnapshot_ACCEPT
	ResponseOfferSnapshot_ABORT
	ResponseOfferSnapshot_REJECT
	ResponseOfferSnapshot_REJECT_FORMAT
	ResponseOfferSnapshot_REJECT_SENDER
)

func (r ResponseOfferSnapshot_Result) String() string {
	switch r {
	case ResponseOfferSnapshot_UNKNOWN:
		return "UNKNOWN" //nolint:goconst
	case ResponseOfferSnapshot_ACCEPT:
		return "ACCEPT" //nolint:goconst
	case ResponseOfferSnapshot_ABORT:
		return "ABORT"
	case ResponseOfferSnapshot_REJECT:
		return "REJECT"
	case ResponseOfferSnapshot_REJECT_FORMAT:
		return "REJECT_FORMAT"
	case ResponseOfferSnapshot_REJECT_SENDER:
		return "REJECT_SENDER"
	default:
		return "UNKNOWN" //nolint:goconst
	}
}

// RequestLoadSnapshotChunk asks the application to load a specific chunk from
// the accepted snapshot so Tendermint can forward it to peers.
type RequestLoadSnapshotChunk struct {
	Height uint64
	Format uint32
	Chunk  uint32
}

// ResponseLoadSnapshotChunk carries the raw bytes for the requested snapshot chunk.
type ResponseLoadSnapshotChunk struct {
	Chunk []byte
}

// RequestApplySnapshotChunk delivers a snapshot chunk to the application so it
// can reconstruct state during state sync.
type RequestApplySnapshotChunk struct {
	Index  uint32
	Chunk  []byte
	Sender string
}

// ResponseApplySnapshotChunk lets the application signal whether it accepted
// the chunk or needs Tendermint to resend or reject certain senders/chunks.
type ResponseApplySnapshotChunk struct {
	Result        ResponseApplySnapshotChunk_Result
	RefetchChunks []uint32
	RejectSenders []string
}

// ResponseApplySnapshotChunk_Result captures the application-side outcome when
// applying a chunk (accept, abort, retry, etc.).
type ResponseApplySnapshotChunk_Result int32

const (
	ResponseApplySnapshotChunk_UNKNOWN ResponseApplySnapshotChunk_Result = iota
	ResponseApplySnapshotChunk_ACCEPT
	ResponseApplySnapshotChunk_ABORT
	ResponseApplySnapshotChunk_RETRY
	ResponseApplySnapshotChunk_RETRY_SNAPSHOT
	ResponseApplySnapshotChunk_REJECT_SNAPSHOT
)

func (r ResponseApplySnapshotChunk_Result) String() string {
	switch r {
	case ResponseApplySnapshotChunk_UNKNOWN:
		return "UNKNOWN" //nolint:goconst
	case ResponseApplySnapshotChunk_ACCEPT:
		return "ACCEPT" //nolint:goconst
	case ResponseApplySnapshotChunk_ABORT:
		return "ABORT"
	case ResponseApplySnapshotChunk_RETRY:
		return "RETRY"
	case ResponseApplySnapshotChunk_RETRY_SNAPSHOT:
		return "RETRY_SNAPSHOT"
	case ResponseApplySnapshotChunk_REJECT_SNAPSHOT:
		return "REJECT_SNAPSHOT"
	default:
		return "UNKNOWN" //nolint:goconst
	}
}

// ResponseProcessProposal communicates the application's decision after
// evaluating a proposed block before votes are cast in the ProcessProposal step.
type ResponseProcessProposal struct {
	Status                ResponseProcessProposal_ProposalStatus
	AppHash               []byte
	TxResults             []*ExecTxResult
	ValidatorUpdates      []ValidatorUpdate
	ConsensusParamUpdates *tmproto.ConsensusParams
}

// ResponseProcessProposal_ProposalStatus lists the possible verdicts when an
// application inspects a proposed block (accept, reject, or unknown).
type ResponseProcessProposal_ProposalStatus int32

const (
	ResponseProcessProposal_UNKNOWN ResponseProcessProposal_ProposalStatus = iota
	ResponseProcessProposal_ACCEPT
	ResponseProcessProposal_REJECT
)

func (s ResponseProcessProposal_ProposalStatus) String() string {
	switch s {
	case ResponseProcessProposal_UNKNOWN:
		return "UNKNOWN" //nolint:goconst
	case ResponseProcessProposal_ACCEPT:
		return "ACCEPT" //nolint:goconst
	case ResponseProcessProposal_REJECT:
		return "REJECT"
	default:
		return "UNKNOWN" //nolint:goconst
	}
}

// RequestProcessProposal bundles all of the proposed block data that the
// application can inspect to decide whether the block should move forward in
// consensus.
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

// RequestFinalizeBlock is emitted after a proposal is committed so the
// application can run FinalizeBlock logic over the same block data it agreed to.
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
