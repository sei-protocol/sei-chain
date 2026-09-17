package main

import (
	"go.opentelemetry.io/otel"

	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// pipelineMeterName scopes the stages that run ahead of the executor. The executor publishes its
// own phases under evmonly_block.
const pipelineMeterName = "evmonly-loadtest"

// Stages of the pipeline that run before execution. Together with the executor's own phases they
// account for the whole of a block's cost.
const (
	// phaseBuildBlock covers constructing and signing a block's transactions.
	phaseBuildBlock = "build_block"
	// phaseRecoverSenders covers PrepareBlock, which is dominated by ECDSA sender recovery.
	phaseRecoverSenders = "recover_senders"
	// phaseWaitingForWork covers time a stage is blocked rather than working: waiting for input, or
	// for room downstream. Dashboards exclude it so the working stages sum to the whole.
	phaseWaitingForWork = "waiting"
)

// newPipelinePhases returns a factory whose timers publish
// evmonly_pipeline_phase_duration_seconds_total. Build one timer per goroutine: a timer tracks a
// single current phase and is not safe to share.
func newPipelinePhases() *seidbmetrics.PhaseTimerFactory {
	return seidbmetrics.NewPhaseTimerFactory(otel.Meter(pipelineMeterName), "evmonly_pipeline")
}
