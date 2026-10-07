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
	// ChainConfig defaults to DefaultChainConfig() when nil. Test
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
	// they finish async persistence. Pool exhaustion allocates an unpooled result
	// instead of blocking block execution.
	BlockResultPoolSize int
	// RejectUnappliableTxs records a transaction that fails ApplyMessage's
	// pre-checks (spent nonce, insufficient funds, block gas exhausted) as a
	// failed receipt with zero gas and no state change, instead of failing the
	// block. Off by default, which keeps geth's rule that such a block is invalid.
	// Unlike DisableNonceCheck, the transaction does not run.
	RejectUnappliableTxs bool
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

// DefaultChainConfig returns params.AllDevChainProtocolChanges through Prague, with the base fee paid to the coinbase.
func DefaultChainConfig() *params.ChainConfig {
	cfg := *params.AllDevChainProtocolChanges
	cfg.OsakaTime = nil
	cfg.BPO1Time = nil
	cfg.BPO2Time = nil
	cfg.BPO3Time = nil
	cfg.BPO4Time = nil
	cfg.BPO5Time = nil
	cfg.AmsterdamTime = nil
	cfg.BogotaTime = nil
	cfg.UBTTime = nil
	cfg.SeiCoinbaseReceivesBaseFee = true
	return &cfg
}
