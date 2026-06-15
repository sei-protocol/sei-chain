package seiv3

import (
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/params"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Config captures the sei-v3 executor knobs needed by the EVM-only path.
type Config struct {
	OCCWorkers           int
	FlushBatchSize       int
	DisableNonceCheck    bool
	DisableGasPriceCheck bool
	MinGasPrice          *big.Int
	ChainConfig          *params.ChainConfig
	CustomPrecompiles    precompiles.Registry
}

func DefaultConfig() Config {
	return Config{
		OCCWorkers:     int(math.Min(12, float64(runtimeCPU()))),
		FlushBatchSize: 100,
		MinGasPrice:    big.NewInt(1_000_000_000),
	}
}

func (c Config) WithDefaults() Config {
	defaults := DefaultConfig()
	if c.OCCWorkers == 0 {
		c.OCCWorkers = defaults.OCCWorkers
	}
	if c.FlushBatchSize == 0 {
		c.FlushBatchSize = defaults.FlushBatchSize
	}
	if c.MinGasPrice == nil {
		c.MinGasPrice = new(big.Int).Set(defaults.MinGasPrice)
	}
	return c
}
