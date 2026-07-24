package evmonly

import (
	"math/big"
	"runtime"

	"github.com/ethereum/go-ethereum/params"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Config captures the sei-v3 executor knobs needed by the EVM-only path.
type Config struct {
	DisableNonceCheck    bool
	DisableGasPriceCheck bool
	MinGasPrice          *big.Int
	// ChainConfig defaults to params.AllDevChainProtocolChanges when nil. Test
	// and scaffold callers can use the default, but production wiring should pass
	// the chain's explicit config.
	ChainConfig       *params.ChainConfig
	CustomPrecompiles precompiles.Registry
	OCCWorkers        int
	// ParseWorkers controls parallel transaction decoding and sender recovery.
	// Values <= 0 default to GOMAXPROCS.
	ParseWorkers int
	// BlockResultPoolSize enables a bounded reusable output pool. Callers that
	// enable it must call BlockResult.Release when they are done with returned
	// results. Result sinks receive a retained result and must release it after
	// they finish async persistence.
	BlockResultPoolSize int
}

func DefaultConfig() Config {
	return Config{
		MinGasPrice:  big.NewInt(1_000_000_000),
		ParseWorkers: runtime.GOMAXPROCS(0),
	}
}

func (c Config) WithDefaults() Config {
	defaults := DefaultConfig()
	if c.MinGasPrice == nil {
		c.MinGasPrice = new(big.Int).Set(defaults.MinGasPrice)
	}
	if c.ParseWorkers <= 0 {
		c.ParseWorkers = defaults.ParseWorkers
	}
	return c
}
