package oracle

import (
	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
)

type PriceType string
type FailureReason string

const (
	PriceTypeCandle PriceType = "candle"
	PriceTypeTicker PriceType = "ticker"

	FailureReasonTimeout   FailureReason = "timeout"
	FailureReasonError     FailureReason = "error"
	FailureReasonDeviation FailureReason = "deviation"
)

type ProviderFailureMetricLabels struct {
	Reason   FailureReason
	Type     PriceType
	Base     string
	Provider string
}

func IncrProviderFailureMetric(labels ProviderFailureMetricLabels) {
	labelArr := []metrics.Label{
		{Name: "reason", Value: string(labels.Reason)},
		{Name: "provider", Value: labels.Provider},
	}
	if labels.Type != "" {
		labelArr = append(labelArr, metrics.Label{Name: "type", Value: string(labels.Type)})
	}
	if labels.Base != "" {
		labelArr = append(labelArr, metrics.Label{Name: "base", Value: labels.Base})
	}
	telemetry.IncrCounterWithLabels([]string{"failure", "provider"}, 1, labelArr)
}
