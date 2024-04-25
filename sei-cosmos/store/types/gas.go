package types

import (
	"fmt"
	"math"
	"sync"

	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
)

// Gas consumption descriptors.
const (
	GasIterNextCostFlatDesc = "IterNextFlat"
	GasValuePerByteDesc     = "ValuePerByte"
	GasWritePerByteDesc     = "WritePerByte"
	GasReadPerByteDesc      = "ReadPerByte"
	GasWriteCostFlatDesc    = "WriteFlat"
	GasReadCostFlatDesc     = "ReadFlat"
	GasHasDesc              = "Has"
	GasDeleteDesc           = "Delete"
)

// Gas measured by the SDK
type Gas = uint64

// ErrorNegativeGasConsumed defines an error thrown when the amount of gas refunded results in a
// negative gas consumed amount.
type ErrorNegativeGasConsumed struct {
	Descriptor string
}

// ErrorOutOfGas defines an error thrown when an action results in out of gas.
type ErrorOutOfGas struct {
	Descriptor string
}

// ErrorGasOverflow defines an error thrown when an action results gas consumption
// unsigned integer overflow.
type ErrorGasOverflow struct {
	Descriptor string
}

// GasMeter interface to track gas consumption
type GasMeter interface {
	GasConsumed() Gas
	GasConsumedToLimit() Gas
	Limit() Gas
	ConsumeGas(amount Gas, descriptor string)
	RefundGas(amount Gas, descriptor string)
	IsPastLimit() bool
	IsOutOfGas() bool
	String() string
	Multiplier() (numerator uint64, denominator uint64)
}

type basicGasMeter struct {
	limit    Gas
	consumed Gas
	lock     *sync.Mutex
}

// NewGasMeter returns a reference to a new basicGasMeter.
func NewGasMeter(limit Gas) GasMeter {
	return &basicGasMeter{
		limit:    limit,
		consumed: 0,
		lock:     &sync.Mutex{},
	}
}

func (g *basicGasMeter) GasConsumed() Gas {
	g.lock.Lock()
	defer g.lock.Unlock()

	return g.consumed
}

func (g *basicGasMeter) Limit() Gas {
	g.lock.Lock()
	defer g.lock.Unlock()

	return g.limit
}

func (g *basicGasMeter) GasConsumedToLimit() Gas {
	g.lock.Lock()
	defer g.lock.Unlock()

	if g.consumed > g.limit {
		return g.limit
	}
	return g.consumed
}

// addUint64Overflow performs the addition operation on two uint64 integers and
// returns a boolean on whether or not the result overflows.
func addUint64Overflow(a, b uint64) (uint64, bool) {
	if math.MaxUint64-a < b {
		return 0, true
	}

	return a + b, false
}

func (g *basicGasMeter) ConsumeGas(amount Gas, descriptor string) {
	g.lock.Lock()
	defer g.lock.Unlock()

	var overflow bool
	g.consumed, overflow = addUint64Overflow(g.consumed, amount)
	if overflow {
		g.consumed = math.MaxUint64
		g.incrGasExceededCounter("overflow", descriptor)
		panic(ErrorGasOverflow{descriptor})
	}
	if g.consumed > g.limit {
		g.incrGasExceededCounter("out_of_gas", descriptor)
		panic(ErrorOutOfGas{descriptor})
	}
}

// cosmos_tx_gas_exceeded
func (g *basicGasMeter) incrGasExceededCounter(errorType string, descriptor string) {
	telemetry.IncrCounterWithLabels(
		[]string{"gas", "exceeded"},
		1,
		// descriptor is a label to distinguish between different gas meters (e.g block vs tx)
		[]metrics.Label{telemetry.NewLabel("error", errorType), telemetry.NewLabel("descriptor", descriptor)},
	)
}

// RefundGas will deduct the given amount from the gas consumed. If the amount is greater than the
// gas consumed, the function will panic.
//
// Use case: This functionality enables refunding gas to the transaction or block gas pools so that
// EVM-compatible chains can fully support the go-ethereum StateDb interface.
// See https://github.com/cosmos/cosmos-sdk/pull/9403 for reference.
func (g *basicGasMeter) RefundGas(amount Gas, descriptor string) {
	g.lock.Lock()
	defer g.lock.Unlock()

	if g.consumed < amount {
		panic(ErrorNegativeGasConsumed{Descriptor: descriptor})
	}

	g.consumed -= amount
}

func (g *basicGasMeter) IsPastLimit() bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	return g.consumed > g.limit
}

func (g *basicGasMeter) IsOutOfGas() bool {
	g.lock.Lock()
	defer g.lock.Unlock()

	return g.consumed >= g.limit
}

func (g *basicGasMeter) String() string {
	return fmt.Sprintf("BasicGasMeter:\n  limit: %d\n  consumed: %d", g.limit, g.consumed)
}

func (g *basicGasMeter) Multiplier() (numerator uint64, denominator uint64) {
	return 1, 1
}

type multiplierGasMeter struct {
	basicGasMeter
	multiplierNumerator   uint64
	multiplierDenominator uint64
}

func NewMultiplierGasMeter(limit Gas, multiplierNumerator uint64, multiplierDenominator uint64) GasMeter {
	return &multiplierGasMeter{
		basicGasMeter: basicGasMeter{
			limit:    limit,
			consumed: 0,
			lock:     &sync.Mutex{},
		},
		multiplierNumerator:   multiplierNumerator,
		multiplierDenominator: multiplierDenominator,
	}
}

func (g *multiplierGasMeter) adjustGas(original Gas) Gas {
	return original * g.multiplierNumerator / g.multiplierDenominator
}

func (g *multiplierGasMeter) ConsumeGas(amount Gas, descriptor string) {
	g.basicGasMeter.ConsumeGas(g.adjustGas(amount), descriptor)
}

func (g *multiplierGasMeter) RefundGas(amount Gas, descriptor string) {
	g.basicGasMeter.RefundGas(g.adjustGas(amount), descriptor)
}

func (g *multiplierGasMeter) Multiplier() (numerator uint64, denominator uint64) {
	return g.multiplierNumerator, g.multiplierDenominator
}

type infiniteGasMeter struct {
	consumed Gas
	lock     *sync.Mutex
}

// NewInfiniteGasMeter returns a reference to a new infiniteGasMeter.
func NewInfiniteGasMeter() GasMeter {
	return &infiniteGasMeter{
		consumed: 0,
		lock:     &sync.Mutex{},
	}
}

func (g *infiniteGasMeter) GasConsumed() Gas {
	g.lock.Lock()
	defer g.lock.Unlock()

	return g.consumed
}

func (g *infiniteGasMeter) GasConsumedToLimit() Gas {
	g.lock.Lock()
	defer g.lock.Unlock()

	return g.consumed
}

func (g *infiniteGasMeter) Limit() Gas {
	g.lock.Lock()
	defer g.lock.Unlock()

	return 0
}

func (g *infiniteGasMeter) ConsumeGas(amount Gas, descriptor string) {
	g.lock.Lock()
	defer g.lock.Unlock()

	var overflow bool
	// TODO: Should we set the consumed field after overflow checking?
	g.consumed, overflow = addUint64Overflow(g.consumed, amount)
	if overflow {
		panic(ErrorGasOverflow{descriptor})
	}
}

// RefundGas will deduct the given amount from the gas consumed. If the amount is greater than the
// gas consumed, the function will panic.
//
// Use case: This functionality enables refunding gas to the trasaction or block gas pools so that
// EVM-compatible chains can fully support the go-ethereum StateDb interface.
// See https://github.com/cosmos/cosmos-sdk/pull/9403 for reference.
func (g *infiniteGasMeter) RefundGas(amount Gas, descriptor string) {
	g.lock.Lock()
	defer g.lock.Unlock()

	if g.consumed < amount {
		panic(ErrorNegativeGasConsumed{Descriptor: descriptor})
	}

	g.consumed -= amount
}

func (g *infiniteGasMeter) IsPastLimit() bool {
	return false
}

func (g *infiniteGasMeter) IsOutOfGas() bool {
	return false
}

func (g *infiniteGasMeter) String() string {
	g.lock.Lock()
	defer g.lock.Unlock()

	return fmt.Sprintf("InfiniteGasMeter:\n  consumed: %d", g.consumed)
}

func (g *infiniteGasMeter) Multiplier() (numerator uint64, denominator uint64) {
	return 1, 1
}

type infiniteMultiplierGasMeter struct {
	infiniteGasMeter
	multiplierNumerator   uint64
	multiplierDenominator uint64
}

func NewInfiniteMultiplierGasMeter(multiplierNumerator uint64, multiplierDenominator uint64) GasMeter {
	return &infiniteMultiplierGasMeter{
		infiniteGasMeter: infiniteGasMeter{
			consumed: 0,
			lock:     &sync.Mutex{},
		},
		multiplierNumerator:   multiplierNumerator,
		multiplierDenominator: multiplierDenominator,
	}
}

func (g *infiniteMultiplierGasMeter) adjustGas(original Gas) Gas {
	return original * g.multiplierNumerator / g.multiplierDenominator
}

func (g *infiniteMultiplierGasMeter) ConsumeGas(amount Gas, descriptor string) {
	g.infiniteGasMeter.ConsumeGas(g.adjustGas(amount), descriptor)
}

func (g *infiniteMultiplierGasMeter) RefundGas(amount Gas, descriptor string) {
	g.infiniteGasMeter.RefundGas(g.adjustGas(amount), descriptor)
}

func (g *infiniteMultiplierGasMeter) Multiplier() (numerator uint64, denominator uint64) {
	return g.multiplierNumerator, g.multiplierDenominator
}

type noConsumptionInfiniteGasMeter struct {
	infiniteGasMeter
}

func NewNoConsumptionInfiniteGasMeter() GasMeter {
	return &noConsumptionInfiniteGasMeter{
		infiniteGasMeter: infiniteGasMeter{
			consumed: 0,
			lock:     &sync.Mutex{},
		},
	}
}

func (g *noConsumptionInfiniteGasMeter) GasConsumed() Gas {
	return 0
}

func (g *noConsumptionInfiniteGasMeter) GasConsumedToLimit() Gas {
	return 0
}

func (g *noConsumptionInfiniteGasMeter) ConsumeGas(amount Gas, descriptor string) {}

func (g *noConsumptionInfiniteGasMeter) RefundGas(amount Gas, descriptor string) {}

// GasConfig defines gas cost for each operation on KVStores
type GasConfig struct {
	HasCost          Gas
	DeleteCost       Gas
	ReadCostFlat     Gas
	ReadCostPerByte  Gas
	WriteCostFlat    Gas
	WriteCostPerByte Gas
	IterNextCostFlat Gas
}

// KVGasConfig returns a default gas config for KVStores.
func KVGasConfig() GasConfig {
	return GasConfig{
		HasCost:          1000,
		DeleteCost:       1000,
		ReadCostFlat:     1000,
		ReadCostPerByte:  3,
		WriteCostFlat:    2000,
		WriteCostPerByte: 30,
		IterNextCostFlat: 30,
	}
}

// TransientGasConfig returns a default gas config for TransientStores.
func TransientGasConfig() GasConfig {
	return GasConfig{
		HasCost:          100,
		DeleteCost:       100,
		ReadCostFlat:     100,
		ReadCostPerByte:  0,
		WriteCostFlat:    200,
		WriteCostPerByte: 3,
		IterNextCostFlat: 3,
	}
}
