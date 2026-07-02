package consensus

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	tmmetrics "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"

	cstypes "github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const (
	// MetricsNamespace is the namespace shared by all Tendermint Prometheus metrics.
	MetricsNamespace = "tendermint"
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	MetricsSubsystem = "consensus"
)

//go:generate go run ../../scripts/metricsgen -struct=Metrics

// Metrics contains metrics exposed by this package.
type Metrics struct {
	// Height of the chain.
	Height *tmmetrics.GaugeIntVec

	// Last height signed by this validator if the node is a validator.
	ValidatorLastSignedHeight *tmmetrics.GaugeIntVec `metrics_labels:"validator_address"`

	// Number of rounds.
	Rounds *tmmetrics.GaugeIntVec

	// Histogram of round duration.
	RoundDuration *prometheus.HistogramVec `metrics_buckettype:"exprange" metrics_bucketsizes:"0.1, 100, 8"`

	// Number of validators.
	Validators *tmmetrics.GaugeIntVec
	// Total power of all validators.
	ValidatorsPower *tmmetrics.GaugeIntVec
	// Power of a validator.
	ValidatorPower *tmmetrics.GaugeIntVec `metrics_labels:"validator_address"`
	// Amount of blocks missed per validator.
	ValidatorMissedBlocks *tmmetrics.GaugeIntVec `metrics_labels:"validator_address"`
	// Number of validators who did not sign.
	MissingValidators *tmmetrics.GaugeIntVec
	// Total power of the missing validators.
	MissingValidatorsPower *tmmetrics.GaugeIntVec `metrics_labels:"validator_address"`
	// Number of validators who tried to double sign.
	ByzantineValidators *tmmetrics.GaugeIntVec
	// Total power of the byzantine validators.
	ByzantineValidatorsPower *tmmetrics.GaugeIntVec

	// Time in seconds between this and the last block.
	BlockIntervalSeconds *prometheus.HistogramVec `metrics_buckettype:"exp" metrics_bucketsizes:"0.1, 1.3, 20"`

	// Number of transactions.
	NumTxs *tmmetrics.GaugeIntVec
	// Size of the block.
	BlockSizeBytes *prometheus.HistogramVec `metrics_buckettype:"exp" metrics_bucketsizes:"1000, 1.5, 25"`
	// Total number of transactions.
	TotalTxs *tmmetrics.GaugeIntVec
	// The latest block height.
	CommittedHeight *tmmetrics.GaugeIntVec `metrics_name:"latest_block_height"`
	// Whether or not a node is block syncing. 1 if yes, 0 if no.
	BlockSyncing *tmmetrics.GaugeIntVec
	// Whether or not a node is state syncing. 1 if yes, 0 if no.
	StateSyncing *tmmetrics.GaugeIntVec

	// Number of block parts transmitted by each peer.
	BlockParts *tmmetrics.CounterIntVec `metrics_labels:"peer_id"`

	// Histogram of durations for each step in the consensus protocol.
	StepDuration *prometheus.HistogramVec `metrics_labels:"step" metrics_buckettype:"exprange" metrics_bucketsizes:"0.1, 100, 8"`
	stepStart    time.Time

	// Histogram of time taken to receive a block in seconds, measured between when a new block is first
	// discovered to when the block is completed.
	BlockGossipReceiveLatency *prometheus.HistogramVec `metrics_buckettype:"exprange" metrics_bucketsizes:"0.1, 100, 8"`
	blockGossipStart          time.Time

	// Number of block parts received by the node, separated by whether the part
	// was relevant to the block the node is trying to gather or not.
	BlockGossipPartsReceived *tmmetrics.CounterIntVec `metrics_labels:"matches_current"`

	// Number of proposal blocks created on propose received.
	ProposalBlockCreatedOnPropose *tmmetrics.CounterIntVec `metrics_labels:"success"`

	// Number of txs in a proposal.
	ProposalTxs *prometheus.GaugeVec

	// Number of missing txs when trying to create proposal.
	ProposalMissingTxs *tmmetrics.GaugeIntVec

	//Number of missing txs when a proposal is received
	MissingTxs *prometheus.GaugeVec `metrics_labels:"proposer_address"`

	// QuroumPrevoteMessageDelay is the interval in seconds between the proposal
	// timestamp and the timestamp of the earliest prevote that achieved a quorum
	// during the prevote step.
	//
	// To compute it, sum the voting power over each prevote received, in increasing
	// order of timestamp. The timestamp of the first prevote to increase the sum to
	// be above 2/3 of the total voting power of the network defines the endpoint
	// the endpoint of the interval. Subtract the proposal timestamp from this endpoint
	// to obtain the quorum delay.
	//metrics:Interval in seconds between the proposal timestamp and the timestamp of the earliest prevote that achieved a quorum.
	QuorumPrevoteDelay *prometheus.GaugeVec `metrics_labels:"proposer_address"`

	// FullPrevoteDelay is the interval in seconds between the proposal
	// timestamp and the timestamp of the latest prevote in a round where 100%
	// of the voting power on the network issued prevotes.
	//metrics:Interval in seconds between the proposal timestamp and the timestamp of the latest prevote in a round where all validators voted.
	FullPrevoteDelay *prometheus.GaugeVec `metrics_labels:"proposer_address"`

	// ProposalTimestampDifference is the difference between the timestamp in
	// the proposal message and the local time of the validator at the time
	// that the validator received the message.
	//metrics:Difference between the timestamp in the proposal message and the local time of the validator at the time it received the message.
	ProposalTimestampDifference *prometheus.HistogramVec `metrics_labels:"is_timely" metrics_bucketsizes:"-10, -.5, -.025, 0, .1, .5, 1, 1.5, 2, 10"`

	// ProposalReceiveCount is the total number of proposals received by this node
	// since process start.
	// The metric is annotated by the status of the proposal from the application,
	// either 'accepted' or 'rejected'.
	//metrics:Total number of proposals received by the node since process start labeled by application response status.
	ProposalReceiveCount *tmmetrics.CounterIntVec `metrics_labels:"status"`

	// ProposalCreationCount is the total number of proposals created by this node
	// since process start.
	//metrics:Total number of proposals created by the node since process start.
	ProposalCreateCount *tmmetrics.CounterIntVec

	// RoundVotingPowerPercent is the percentage of the total voting power received
	// with a round. The value begins at 0 for each round and approaches 1.0 as
	// additional voting power is observed. The metric is labeled by vote type.
	//metrics:A value between 0 and 1.0 representing the percentage of the total voting power per vote type received within a round.
	RoundVotingPowerPercent *prometheus.GaugeVec `metrics_labels:"vote_type"`

	// LateVotes stores the number of votes that were received by this node that
	// correspond to earlier heights and rounds than this node is currently
	// in.
	//metrics:Number of votes received by the node since process start that correspond to earlier heights and rounds than this node is currently in.
	LateVotes *tmmetrics.CounterIntVec `metrics_labels:"validator_address"`

	// FinalRound stores the final round id the proposal block reach consensus in.
	//metrics:The final round number for where the proposal block reach consensus in, starting at 0.
	FinalRound *prometheus.HistogramVec `metrics_labels:"proposer_address" metrics_bucketsizes:"0,1,2,3,5,10"`

	// ProposeLatency stores the latency in seconds from when the initial round
	// starts till the proposal is created and received
	//metrics:Number of seconds from when the consensus round started till the proposal receive time
	ProposeLatency *prometheus.HistogramVec `metrics_labels:"proposer_address" metrics_buckettype:"exprange" metrics_bucketsizes:"0.01, 10, 10"`

	// PrevoteLatency is measuring the relative delay in seconds from when the first vote arrive in each round
	// till all remaining following prevote arrives from different validators to reach consensus.
	//metrics:Number of seconds from when first prevote arrive till other remaining prevote arrives for each validator
	PrevoteLatency *prometheus.HistogramVec `metrics_labels:"validator_address" metrics_buckettype:"exprange" metrics_bucketsizes:"0.01, 10, 10"`

	// ConsensusTime the metric to track how long the consensus takes in each block
	//metrics: Number of seconds spent on consensus
	ConsensusTime *prometheus.HistogramVec `metrics_buckettype:"exp" metrics_bucketsizes:"0.01, 1.3, 25"`

	// CompleteProposalTime measures how long it takes between receiving a proposal and finishing
	// processing all of its parts. Note that this means it also includes network latency from
	// block parts gossip
	CompleteProposalTime *prometheus.HistogramVec `metrics_buckettype:"exp" metrics_bucketsizes:"0.01, 1.3, 25"`

	// ApplyBlockLatency measures how long it takes to execute ApplyBlock in finalize commit step
	ApplyBlockLatency *prometheus.HistogramVec `metrics_buckettype:"exp" metrics_bucketsizes:"0.01, 1.3, 25"`

	StepLatency                 *prometheus.GaugeVec `metrics_labels:"step"`
	lastRecordedStepLatencyNano int64
	StepCount                   *tmmetrics.GaugeIntVec `metrics_labels:"step"`
}

// RecordConsMetrics uses for recording the block related metrics during fast-sync.
func (m *Metrics) RecordConsMetrics(block *types.Block) {
	m.NumTxsAt().Set(int64(len(block.Txs)))
	m.TotalTxsAt().Add(int64(len(block.Txs)))
	m.BlockSizeBytesAt().Observe(float64(block.Size()))
	m.CommittedHeightAt().Set(block.Height)
}

func (m *Metrics) MarkBlockGossipStarted() {
	m.blockGossipStart = time.Now()
}

func (m *Metrics) MarkBlockGossipComplete() {
	m.BlockGossipReceiveLatencyAt().Observe(time.Since(m.blockGossipStart).Seconds())
}

func (m *Metrics) MarkProposalProcessed(accepted bool) {
	status := "accepted"
	if !accepted {
		status = "rejected"
	}
	m.ProposalReceiveCountAt(status).Add(1)
}

func (m *Metrics) MarkVoteReceived(vt tmproto.SignedMsgType, power, totalPower int64) {
	p := float64(power) / float64(totalPower)
	n := strings.ToLower(strings.TrimPrefix(vt.String(), "SIGNED_MSG_TYPE_"))
	m.RoundVotingPowerPercentAt(n).Add(p)
}

func (m *Metrics) MarkRound(r int32, st time.Time) {
	m.RoundsAt().Set(int64(r))
	roundTime := time.Since(st).Seconds()
	m.RoundDurationAt().Observe(roundTime)

	pvt := tmproto.PrevoteType
	pvn := strings.ToLower(strings.TrimPrefix(pvt.String(), "SIGNED_MSG_TYPE_"))
	m.RoundVotingPowerPercentAt(pvn).Set(0)

	pct := tmproto.PrecommitType
	pcn := strings.ToLower(strings.TrimPrefix(pct.String(), "SIGNED_MSG_TYPE_"))
	m.RoundVotingPowerPercentAt(pcn).Set(0)
}

func (m *Metrics) MarkLateVote(vote *types.Vote) {
	validator := vote.ValidatorAddress.String()
	m.LateVotesAt(validator).Add(1)
}

func (m *Metrics) MarkFinalRound(round int32, proposer string) {
	m.FinalRoundAt(proposer).Observe(float64(round))
}

func (m *Metrics) MarkProposeLatency(proposer string, latency time.Duration) {
	m.ProposeLatencyAt(proposer).Observe(latency.Seconds())
}

func (m *Metrics) MarkPrevoteLatency(validator string, latency time.Duration) {
	m.PrevoteLatencyAt(validator).Observe(latency.Seconds())
}

func (m *Metrics) MarkCompleteProposalTime(latency time.Duration) {
	m.CompleteProposalTimeAt().Observe(latency.Seconds())
}

func (m *Metrics) MarkConsensusTime(latency time.Duration) {
	m.ConsensusTimeAt().Observe(latency.Seconds())
}

func (m *Metrics) MarkApplyBlockLatency(latency time.Duration) {
	m.ApplyBlockLatencyAt().Observe(latency.Seconds())
}

func (m *Metrics) MarkStep(s cstypes.RoundStepType) {
	if !m.stepStart.IsZero() {
		stepTime := time.Since(m.stepStart).Seconds()
		stepName := strings.TrimPrefix(s.String(), "RoundStep")
		m.StepDurationAt(stepName).Observe(stepTime)
		m.StepCountAt(s.String()).Add(1)
	}
	m.stepStart = time.Now()
}

func (m *Metrics) MarkStepLatency(s cstypes.RoundStepType) {
	now := time.Now().UnixNano()
	m.StepLatencyAt(s.String()).Add(float64(now - m.lastRecordedStepLatencyNano))
	m.lastRecordedStepLatencyNano = now
}

func (m *Metrics) ClearStepMetrics() {
	for _, st := range []cstypes.RoundStepType{
		cstypes.RoundStepNewHeight,
		cstypes.RoundStepNewRound,
		cstypes.RoundStepPropose,
		cstypes.RoundStepPrevote,
		cstypes.RoundStepPrevoteWait,
		cstypes.RoundStepPrecommit,
		cstypes.RoundStepPrecommitWait,
		cstypes.RoundStepCommit,
	} {
		m.StepCountAt(st.String()).Set(0)
		m.StepLatencyAt(st.String()).Set(0)
	}
}
