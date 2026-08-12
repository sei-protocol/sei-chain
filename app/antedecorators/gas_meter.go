package antedecorators

import storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"

type noConsumptionGasMeter struct {
	storetypes.GasMeter
}

type reportingGasMeter struct {
	storetypes.GasMeter
	reportedLimit storetypes.Gas
}

// NewReportingGasMeter returns a meter with an independent reported limit.
func NewReportingGasMeter(meter storetypes.GasMeter, reportedLimit storetypes.Gas) storetypes.GasMeter {
	return &reportingGasMeter{GasMeter: meter, reportedLimit: reportedLimit}
}

func (m *reportingGasMeter) Limit() storetypes.Gas {
	return m.reportedLimit
}

// NewNoConsumptionGasMeter returns a meter that reports zero consumption and never runs out of gas.
func NewNoConsumptionGasMeter(meter storetypes.GasMeter) storetypes.GasMeter {
	return &noConsumptionGasMeter{GasMeter: meter}
}

func (m *noConsumptionGasMeter) GasConsumed() storetypes.Gas {
	return 0
}

func (m *noConsumptionGasMeter) GasConsumedToLimit() storetypes.Gas {
	return 0
}

func (m *noConsumptionGasMeter) ConsumeGas(storetypes.Gas, string) {}

func (m *noConsumptionGasMeter) RefundGas(storetypes.Gas, string) {}

func (m *noConsumptionGasMeter) IsPastLimit() bool {
	return false
}

func (m *noConsumptionGasMeter) IsOutOfGas() bool {
	return false
}
