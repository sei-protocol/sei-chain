package types

import (
	"encoding/json"
	"fmt"
	"strings"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/internal/jsontypes"
	tmquery "github.com/tendermint/tendermint/internal/pubsub/query"
	"github.com/tendermint/tendermint/proto/tendermint/types"
)

// Reserved event types (alphabetically sorted).
const (
	// Block level events for mass consumption by users.
	// These events are triggered from the state package,
	// after a block has been committed.
	// These are also used by the tx indexer for async indexing.
	// All of this data can be fetched through the rpc.
	EventNewBlockValue            = "NewBlock"
	EventNewBlockHeaderValue      = "NewBlockHeader"
	EventNewEvidenceValue         = "NewEvidence"
	EventTxValue                  = "Tx"
	EventValidatorSetUpdatesValue = "ValidatorSetUpdates"

	// Internal consensus events.
	// These are used for testing the consensus state machine.
	// They can also be used to build real-time consensus visualizers.
	EventCompleteProposalValue = "CompleteProposal"
	// The BlockSyncStatus event will be emitted when the node switching
	// state sync mechanism between the consensus reactor and the blocksync reactor.
	EventBlockSyncStatusValue = "BlockSyncStatus"
	EventLockValue            = "Lock"
	EventNewRoundValue        = "NewRound"
	EventNewRoundStepValue    = "NewRoundStep"
	EventPolkaValue           = "Polka"
	EventRelockValue          = "Relock"
	EventStateSyncStatusValue = "StateSyncStatus"
	EventTimeoutProposeValue  = "TimeoutPropose"
	EventTimeoutWaitValue     = "TimeoutWait"
	EventValidBlockValue      = "ValidBlock"
	EventVoteValue            = "Vote"

	// Events emitted by the evidence reactor when evidence is validated
	// and before it is committed
	EventEvidenceValidatedValue = "EvidenceValidated"
)

// Pre-populated ABCI Tendermint-reserved events
var (
	EventNewBlock = abci.Event{
		Type: strings.Split(EventTypeKey, ".")[0],
		Attributes: []abci.EventAttribute{
			{
				Key:   []byte(strings.Split(EventTypeKey, ".")[1]),
				Value: []byte(EventNewBlockValue),
			},
		},
	}

	EventNewBlockHeader = abci.Event{
		Type: strings.Split(EventTypeKey, ".")[0],
		Attributes: []abci.EventAttribute{
			{
				Key:   []byte(strings.Split(EventTypeKey, ".")[1]),
				Value: []byte(EventNewBlockHeaderValue),
			},
		},
	}

	EventNewEvidence = abci.Event{
		Type: strings.Split(EventTypeKey, ".")[0],
		Attributes: []abci.EventAttribute{
			{
				Key:   []byte(strings.Split(EventTypeKey, ".")[1]),
				Value: []byte(EventNewEvidenceValue),
			},
		},
	}

	EventTx = abci.Event{
		Type: strings.Split(EventTypeKey, ".")[0],
		Attributes: []abci.EventAttribute{
			{
				Key:   []byte(strings.Split(EventTypeKey, ".")[1]),
				Value: []byte(EventTxValue),
			},
		},
	}
)

// ENCODING / DECODING

// EventData is satisfied by types that can be published as event data.
//
// Implementations of this interface that contain ABCI event metadata should
// also implement the eventlog.ABCIEventer extension interface to expose those
// metadata to the event log machinery. Event data that do not contain ABCI
// metadata can safely omit this.
type EventData interface {
	// The value must support encoding as a type-tagged JSON object.
	jsontypes.Tagged
	ToLegacy() LegacyEventData
}

type LegacyEventData interface {
	jsontypes.Tagged
}

func init() {
	jsontypes.MustRegister(EventDataBlockSyncStatus{})
	jsontypes.MustRegister(EventDataCompleteProposal{})
	jsontypes.MustRegister(EventDataNewBlock{})
	jsontypes.MustRegister(EventDataNewBlockHeader{})
	jsontypes.MustRegister(EventDataNewEvidence{})
	jsontypes.MustRegister(EventDataNewRound{})
	jsontypes.MustRegister(EventDataRoundState{})
	jsontypes.MustRegister(EventDataStateSyncStatus{})
	jsontypes.MustRegister(EventDataTx{})
	jsontypes.MustRegister(EventDataValidatorSetUpdates{})
	jsontypes.MustRegister(EventDataVote{})
	jsontypes.MustRegister(EventDataEvidenceValidated{})
	jsontypes.MustRegister(LegacyEventDataNewBlock{})
	jsontypes.MustRegister(LegacyEventDataTx{})
	jsontypes.MustRegister(EventDataString(""))
}

// Most event messages are basic types (a block, a transaction)
// but some (an input to a call tx or a receive) are more exotic

type EventDataNewBlock struct {
	Block   *Block  `json:"block"`
	BlockID BlockID `json:"block_id"`

	ResultFinalizeBlock abci.ResponseFinalizeBlock `json:"result_finalize_block"`
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataNewBlock) TypeTag() string { return "tendermint/event/NewBlock_new" }

// ABCIEvents implements the eventlog.ABCIEventer interface.
func (e EventDataNewBlock) ABCIEvents() []abci.Event {
	base := []abci.Event{eventWithAttr(BlockHeightKey, fmt.Sprint(e.Block.Header.Height))}
	return append(base, e.ResultFinalizeBlock.Events...)
}

type LegacyEventDataNewBlock struct {
	Block            *LegacyBlock            `json:"block"`
	ResultBeginBlock abci.ResponseBeginBlock `json:"result_begin_block"`
	ResultEndBlock   LegacyResponseEndBlock  `json:"result_end_block"`
}

func (LegacyEventDataNewBlock) TypeTag() string { return "tendermint/event/NewBlock" }

type LegacyEvidence struct {
	Evidence EvidenceList `json:"evidence"`
}

type LegacyBlock struct {
	Header     `json:"header"`
	Data       `json:"data"`
	Evidence   LegacyEvidence `json:"evidence"`
	LastCommit *Commit        `json:"last_commit"`
}

type LegacyResponseEndBlock struct {
	ValidatorUpdates      []abci.ValidatorUpdate `json:"validator_updates"`
	ConsensusParamUpdates *LegacyConsensusParams `json:"consensus_param_updates,omitempty"`
	Events                []abci.Event           `json:"events,omitempty"`
}

type LegacyConsensusParams struct {
	Block     *LegacyBlockParams     `json:"block,omitempty"`
	Evidence  *LegacyEvidenceParams  `json:"evidence,omitempty"`
	Validator *types.ValidatorParams `json:"validator,omitempty"`
	Version   *LegacyVersionParams   `json:"version,omitempty"`
}

type LegacyBlockParams struct {
	MaxBytes string `json:"max_bytes,omitempty"`
	MaxGas   string `json:"max_gas,omitempty"`
}

type LegacyEvidenceParams struct {
	MaxAgeNumBlocks string `json:"max_age_num_blocks,omitempty"`
	MaxAgeDuration  string `json:"max_age_duration"`
	MaxBytes        string `json:"max_bytes,omitempty"`
}

type LegacyVersionParams struct {
	AppVersion string `json:"app_version,omitempty"`
}

func (e EventDataNewBlock) ToLegacy() LegacyEventData {
	block := &LegacyBlock{}
	if e.Block != nil {
		block = &LegacyBlock{
			Header:     e.Block.Header,
			Data:       e.Block.Data,
			Evidence:   LegacyEvidence{Evidence: e.Block.Evidence},
			LastCommit: e.Block.LastCommit,
		}
	}
	consensusParamUpdates := &LegacyConsensusParams{}
	if e.ResultFinalizeBlock.ConsensusParamUpdates != nil {
		if e.ResultFinalizeBlock.ConsensusParamUpdates.Block != nil {
			consensusParamUpdates.Block = &LegacyBlockParams{
				MaxBytes: fmt.Sprintf("%d", e.ResultFinalizeBlock.ConsensusParamUpdates.Block.MaxBytes),
				MaxGas:   fmt.Sprintf("%d", e.ResultFinalizeBlock.ConsensusParamUpdates.Block.MaxGas),
			}
		}
		if e.ResultFinalizeBlock.ConsensusParamUpdates.Evidence != nil {
			consensusParamUpdates.Evidence = &LegacyEvidenceParams{
				MaxAgeNumBlocks: fmt.Sprintf("%d", e.ResultFinalizeBlock.ConsensusParamUpdates.Evidence.MaxAgeNumBlocks),
				MaxAgeDuration:  fmt.Sprintf("%d", e.ResultFinalizeBlock.ConsensusParamUpdates.Evidence.MaxAgeDuration),
				MaxBytes:        fmt.Sprintf("%d", e.ResultFinalizeBlock.ConsensusParamUpdates.Evidence.MaxBytes),
			}
		}
		if e.ResultFinalizeBlock.ConsensusParamUpdates.Validator != nil {
			consensusParamUpdates.Validator = &types.ValidatorParams{
				PubKeyTypes: e.ResultFinalizeBlock.ConsensusParamUpdates.Validator.PubKeyTypes,
			}
		}
		if e.ResultFinalizeBlock.ConsensusParamUpdates.Version != nil {
			consensusParamUpdates.Version = &LegacyVersionParams{
				AppVersion: fmt.Sprintf("%d", e.ResultFinalizeBlock.ConsensusParamUpdates.Version.AppVersion),
			}
		}
	}
	return &LegacyEventDataNewBlock{
		Block:            block,
		ResultBeginBlock: abci.ResponseBeginBlock{Events: e.ResultFinalizeBlock.Events},
		ResultEndBlock: LegacyResponseEndBlock{
			ValidatorUpdates:      e.ResultFinalizeBlock.ValidatorUpdates,
			Events:                []abci.Event{},
			ConsensusParamUpdates: consensusParamUpdates,
		},
	}
}

type EventDataNewBlockHeader struct {
	Header Header `json:"header"`

	NumTxs              int64                      `json:"num_txs,string"` // Number of txs in a block
	ResultFinalizeBlock abci.ResponseFinalizeBlock `json:"result_finalize_block"`
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataNewBlockHeader) TypeTag() string { return "tendermint/event/NewBlockHeader" }

// ABCIEvents implements the eventlog.ABCIEventer interface.
func (e EventDataNewBlockHeader) ABCIEvents() []abci.Event {
	base := []abci.Event{eventWithAttr(BlockHeightKey, fmt.Sprint(e.Header.Height))}
	return append(base, e.ResultFinalizeBlock.Events...)
}

func (e EventDataNewBlockHeader) ToLegacy() LegacyEventData {
	return e
}

type EventDataNewEvidence struct {
	Evidence Evidence `json:"evidence"`

	Height int64 `json:"height,string"`
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataNewEvidence) TypeTag() string { return "tendermint/event/NewEvidence" }

func (e EventDataNewEvidence) ToLegacy() LegacyEventData {
	return e
}

// All txs fire EventDataTx
type EventDataTx struct {
	abci.TxResult
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataTx) TypeTag() string { return "tendermint/event/Tx_new" }

// ABCIEvents implements the eventlog.ABCIEventer interface.
func (e EventDataTx) ABCIEvents() []abci.Event {
	base := []abci.Event{
		eventWithAttr(TxHashKey, fmt.Sprintf("%X", Tx(e.Tx).Hash())),
		eventWithAttr(TxHeightKey, fmt.Sprintf("%d", e.Height)),
	}
	return append(base, e.Result.Events...)
}

type LegacyEventDataTx struct {
	TxResult LegacyTxResult `json:"TxResult"`
}

type LegacyTxResult struct {
	Height string       `json:"height,omitempty"`
	Index  uint32       `json:"index,omitempty"`
	Tx     []byte       `json:"tx,omitempty"`
	Result LegacyResult `json:"result"`
}

type LegacyResult struct {
	Log       string       `json:"log,omitempty"`
	GasWanted string       `json:"gas_wanted,omitempty"`
	GasUsed   string       `json:"gas_used,omitempty"`
	Events    []abci.Event `json:"events,omitempty"`
}

func (LegacyEventDataTx) TypeTag() string {
	return "tendermint/event/Tx"
}

func (e EventDataTx) ToLegacy() LegacyEventData {
	return LegacyEventDataTx{
		TxResult: LegacyTxResult{
			Height: fmt.Sprintf("%d", e.Height),
			Index:  e.Index,
			Tx:     e.Tx,
			Result: LegacyResult{
				Log:       e.Result.Log,
				GasWanted: fmt.Sprintf("%d", e.Result.GasWanted),
				GasUsed:   fmt.Sprintf("%d", e.Result.GasUsed),
				Events:    e.Result.Events,
			},
		},
	}
}

// NOTE: This goes into the replay WAL
type EventDataRoundState struct {
	Height int64  `json:"height,string"`
	Round  int32  `json:"round"`
	Step   string `json:"step"`
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataRoundState) TypeTag() string { return "tendermint/event/RoundState" }

func (e EventDataRoundState) ToLegacy() LegacyEventData {
	return e
}

type ValidatorInfo struct {
	Address Address `json:"address"`
	Index   int32   `json:"index"`
}

type EventDataNewRound struct {
	Height int64  `json:"height,string"`
	Round  int32  `json:"round"`
	Step   string `json:"step"`

	Proposer ValidatorInfo `json:"proposer"`
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataNewRound) TypeTag() string { return "tendermint/event/NewRound" }

func (e EventDataNewRound) ToLegacy() LegacyEventData {
	return e
}

type EventDataCompleteProposal struct {
	Height int64  `json:"height,string"`
	Round  int32  `json:"round"`
	Step   string `json:"step"`

	BlockID BlockID `json:"block_id"`
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataCompleteProposal) TypeTag() string { return "tendermint/event/CompleteProposal" }

func (e EventDataCompleteProposal) ToLegacy() LegacyEventData {
	return e
}

type EventDataVote struct {
	Vote *Vote
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataVote) TypeTag() string { return "tendermint/event/Vote" }

func (e EventDataVote) ToLegacy() LegacyEventData {
	return e
}

type EventDataString string

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataString) TypeTag() string { return "tendermint/event/ProposalString" }

func (e EventDataString) ToLegacy() LegacyEventData {
	return e
}

type EventDataValidatorSetUpdates struct {
	ValidatorUpdates []*Validator `json:"validator_updates"`
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataValidatorSetUpdates) TypeTag() string { return "tendermint/event/ValidatorSetUpdates" }

func (e EventDataValidatorSetUpdates) ToLegacy() LegacyEventData {
	return e
}

// EventDataBlockSyncStatus shows the fastsync status and the
// height when the node state sync mechanism changes.
type EventDataBlockSyncStatus struct {
	Complete bool  `json:"complete"`
	Height   int64 `json:"height,string"`
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataBlockSyncStatus) TypeTag() string { return "tendermint/event/FastSyncStatus" }

func (e EventDataBlockSyncStatus) ToLegacy() LegacyEventData {
	return e
}

// EventDataStateSyncStatus shows the statesync status and the
// height when the node state sync mechanism changes.
type EventDataStateSyncStatus struct {
	Complete bool  `json:"complete"`
	Height   int64 `json:"height,string"`
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataStateSyncStatus) TypeTag() string { return "tendermint/event/StateSyncStatus" }

func (e EventDataStateSyncStatus) ToLegacy() LegacyEventData {
	return e
}

type EventDataEvidenceValidated struct {
	Evidence Evidence `json:"evidence"`

	Height int64 `json:"height,string"`
}

// TypeTag implements the required method of jsontypes.Tagged.
func (EventDataEvidenceValidated) TypeTag() string { return "tendermint/event/EvidenceValidated" }

func (e EventDataEvidenceValidated) ToLegacy() LegacyEventData {
	return e
}

// PUBSUB

const (
	// EventTypeKey is a reserved composite key for event name.
	EventTypeKey = "tm.event"

	// TxHashKey is a reserved key, used to specify transaction's hash.
	// see EventBus#PublishEventTx
	TxHashKey = "tx.hash"

	// TxHeightKey is a reserved key, used to specify transaction block's height.
	// see EventBus#PublishEventTx
	TxHeightKey = "tx.height"

	// BlockHeightKey is a reserved key used for indexing FinalizeBlock events.
	BlockHeightKey = "block.height"
)

var (
	EventQueryCompleteProposal    = QueryForEvent(EventCompleteProposalValue)
	EventQueryLock                = QueryForEvent(EventLockValue)
	EventQueryNewBlock            = QueryForEvent(EventNewBlockValue)
	EventQueryNewBlockHeader      = QueryForEvent(EventNewBlockHeaderValue)
	EventQueryNewEvidence         = QueryForEvent(EventNewEvidenceValue)
	EventQueryNewRound            = QueryForEvent(EventNewRoundValue)
	EventQueryNewRoundStep        = QueryForEvent(EventNewRoundStepValue)
	EventQueryPolka               = QueryForEvent(EventPolkaValue)
	EventQueryRelock              = QueryForEvent(EventRelockValue)
	EventQueryTimeoutPropose      = QueryForEvent(EventTimeoutProposeValue)
	EventQueryTimeoutWait         = QueryForEvent(EventTimeoutWaitValue)
	EventQueryTx                  = QueryForEvent(EventTxValue)
	EventQueryValidatorSetUpdates = QueryForEvent(EventValidatorSetUpdatesValue)
	EventQueryValidBlock          = QueryForEvent(EventValidBlockValue)
	EventQueryVote                = QueryForEvent(EventVoteValue)
	EventQueryBlockSyncStatus     = QueryForEvent(EventBlockSyncStatusValue)
	EventQueryStateSyncStatus     = QueryForEvent(EventStateSyncStatusValue)
	EventQueryEvidenceValidated   = QueryForEvent(EventEvidenceValidatedValue)
)

func EventQueryTxFor(tx Tx) *tmquery.Query {
	return tmquery.MustCompile(fmt.Sprintf("%s='%s' AND %s='%X'", EventTypeKey, EventTxValue, TxHashKey, tx.Hash()))
}

func QueryForEvent(eventValue string) *tmquery.Query {
	return tmquery.MustCompile(fmt.Sprintf("%s='%s'", EventTypeKey, eventValue))
}

// BlockEventPublisher publishes all block related events
type BlockEventPublisher interface {
	PublishEventNewBlock(EventDataNewBlock) error
	PublishEventNewBlockHeader(EventDataNewBlockHeader) error
	PublishEventNewEvidence(EventDataNewEvidence) error
	PublishEventTx(EventDataTx) error
	PublishEventValidatorSetUpdates(EventDataValidatorSetUpdates) error
}

type TxEventPublisher interface {
	PublishEventTx(EventDataTx) error
}

// eventWithAttr constructs a single abci.Event with a single attribute.
// The type of the event and the name of the attribute are obtained by
// splitting the event type on period (e.g., "foo.bar").
func eventWithAttr(etype, value string) abci.Event {
	parts := strings.SplitN(etype, ".", 2)
	return abci.Event{
		Type: parts[0],
		Attributes: []abci.EventAttribute{{
			Key: []byte(parts[1]), Value: []byte(value),
		}},
	}
}

func TryUnmarshalEventData(data json.RawMessage) (EventData, error) {
	var eventData EventData
	err := jsontypes.Unmarshal(data, &eventData)
	if err != nil {
		return nil, err
	}
	return eventData, nil
}
