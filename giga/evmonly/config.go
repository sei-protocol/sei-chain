package evmonly

import (
	"math/big"

	"github.com/ethereum/go-ethereum/params"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Config captures the sei-v3 executor knobs needed by the EVM-only path.
type Config struct {
	DisableNonceCheck    bool
	DisableGasPriceCheck bool
	MinGasPrice          *big.Int
	ChainConfig          *params.ChainConfig
	CustomPrecompiles    precompiles.Registry
	OCCWorkers           int
	// BlockResultPoolSize enables a bounded reusable output pool. Callers that
	// enable it must call BlockResult.Release when they are done with returned
	// results. Result sinks receive a retained result and must release it after
	// they finish async persistence.
	BlockResultPoolSize int
}

func DefaultConfig() Config {
	return Config{
		MinGasPrice: big.NewInt(1_000_000_000),
	}
}

func (c Config) WithDefaults() Config {
	defaults := DefaultConfig()
	if c.MinGasPrice == nil {
		c.MinGasPrice = new(big.Int).Set(defaults.MinGasPrice)
	}
	return c
}
