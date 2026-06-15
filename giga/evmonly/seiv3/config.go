package seiv3

import "math"

// Config captures the sei-v3 executor knobs needed by the EVM-only path.
type Config struct {
	OCCWorkers           int
	FlushBatchSize       int
	DisableNonceCheck    bool
	DisableGasPriceCheck bool
}

func DefaultConfig() Config {
	return Config{
		OCCWorkers:     int(math.Min(12, float64(runtimeCPU()))),
		FlushBatchSize: 100,
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
	return c
}
