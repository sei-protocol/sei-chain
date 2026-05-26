package baseapp

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_baseapp")

	// finerGrainedBuckets units are in seconds
	finerGrainedBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)

	// queryDurationBuckets units are in seconds. Archive store queries can run 10–60 seconds range, hence adding more buckets compared to finerGrainedBuckets
	queryDurationBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10, 30, 60, 120,
	)

	baseappMetrics = struct {
		midBlockDuration          metric.Float64Histogram
		endBlockDuration          metric.Float64Histogram
		deliverTxDuration         metric.Float64Histogram
		txCount                   metric.Int64Counter
		txResult                  metric.Int64Counter
		txGasUsed                 metric.Int64Gauge
		txGasWanted               metric.Int64Gauge
		commitDuration            metric.Float64Histogram
		abciQueryDuration         metric.Float64Histogram
		processProposalDuration   metric.Float64Histogram
		finalizeBlockDuration     metric.Float64Histogram
		getTxPriorityHintDuration metric.Float64Histogram
		runTxDuration             metric.Float64Histogram
		runMsgsDuration           metric.Float64Histogram
		runMsgLatency             metric.Float64Histogram
	}{
		midBlockDuration: must(meter.Float64Histogram(
			"mid_block_duration",
			metric.WithDescription("Duration of mid-block execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		endBlockDuration: must(meter.Float64Histogram(
			"end_block_duration",
			metric.WithDescription("Duration of end-block execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		deliverTxDuration: must(meter.Float64Histogram(
			"deliver_tx_duration",
			metric.WithDescription("Duration of DeliverTx execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		txCount: must(meter.Int64Counter(
			"tx",
			metric.WithDescription("Total number of transactions delivered"),
			metric.WithUnit("{count}"),
		)),
		txResult: must(meter.Int64Counter(
			"tx_result",
			metric.WithDescription("Number of delivered transactions by result"),
			metric.WithUnit("{count}"),
		)),
		txGasUsed: must(meter.Int64Gauge(
			"tx_gas_used",
			metric.WithDescription("Gas used by the last delivered transaction"),
			metric.WithUnit("{gas}"),
		)),
		txGasWanted: must(meter.Int64Gauge(
			"tx_gas_wanted",
			metric.WithDescription("Gas wanted by the last delivered transaction"),
			metric.WithUnit("{gas}"),
		)),
		commitDuration: must(meter.Float64Histogram(
			"commit_duration",
			metric.WithDescription("Duration of ABCI Commit in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		abciQueryDuration: must(meter.Float64Histogram(
			"abci_query_duration",
			metric.WithDescription("Duration of ABCI Query by bounded route label in seconds"),
			queryDurationBuckets,
			metric.WithUnit("s"),
		)),
		processProposalDuration: must(meter.Float64Histogram(
			"process_proposal_duration",
			metric.WithDescription("Duration of ProcessProposal execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		finalizeBlockDuration: must(meter.Float64Histogram(
			"finalize_block_duration",
			metric.WithDescription("Duration of FinalizeBlock execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		getTxPriorityHintDuration: must(meter.Float64Histogram(
			"get_tx_priority_hint_duration",
			metric.WithDescription("Duration of GetTxPriorityHint execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		runTxDuration: must(meter.Float64Histogram(
			"run_tx_duration",
			metric.WithDescription("Duration of runTx by mode in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		runMsgsDuration: must(meter.Float64Histogram(
			"run_msgs_duration",
			metric.WithDescription("Duration of RunMsgs execution in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		runMsgLatency: must(meter.Float64Histogram(
			"run_msg_latency",
			metric.WithDescription("Latency of individual message handler execution by message type in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
