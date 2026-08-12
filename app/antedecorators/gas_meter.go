package antedecorators

import storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"

type noConsumptionGasMeter struct {
	storetypes.GasMeter
}

// NewNoConsumptionGasMeter preserves the wrapped meter's reporting metadata while
// suppressing execution gas. Fee-exempt transactions may declare zero gas, but a
// non-zero declared limit must still be reported consistently to proposal builders.
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
