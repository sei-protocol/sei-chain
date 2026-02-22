package config

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

const (
	DefaultSCHistoricalProofMaxInFlight = 1
	DefaultSCHistoricalProofRateLimit   = 1.0 // req/s, <=0 disables rate limit
	DefaultSCHistoricalProofBurst       = 1
)

// StateCommitConfig defines configuration for the state commit (SC) layer.
type StateCommitConfig struct {
	// Enable defines if the state-commit(SeiDB) should be enabled.
	// If true, it will replace the existing IAVL db backend with memIAVL.
	// defaults to true.
	Enable bool `mapstructure:"enable"`

	// Directory defines the state-commit store directory
	// If not explicitly set, default to application home directory
	Directory string `mapstructure:"directory"`

	// AsyncCommitBuffer defines the size of asynchronous commit queue
	// this greatly improve block catching-up performance, <= 0 means synchronous commit.
	// defaults to 100
	AsyncCommitBuffer int `mapstructure:"async-commit-buffer"`

	// WriteMode defines the write routing mode for EVM data
	// Valid values: cosmos_only, dual_write, split_write, evm_only
	// defaults to cosmos_only
	WriteMode WriteMode `mapstructure:"write_mode"`

	// ReadMode defines the read routing mode for EVM data
	// Valid values: cosmos_only, evm_first, split_read
	// defaults to cosmos_only
	ReadMode ReadMode `mapstructure:"read_mode"`

	// MemIAVLConfig is the configuration for the MemIAVL (Cosmos) backend
	MemIAVLConfig memiavl.Config

	// FlatKVConfig is the configuration for the FlatKV (EVM) backend
	FlatKVConfig flatkv.Config

	// Max concurrent historical proof queries (RPC /store path).
	HistoricalProofMaxInFlight int `mapstructure:"historical-proof-max-inflight"`

	// Token bucket rate (req/sec) for historical proof queries.
	// <= 0 disables rate limiting.
	HistoricalProofRateLimit float64 `mapstructure:"historical-proof-rate-limit"`

	// Token bucket burst for historical proof queries.
	HistoricalProofBurst int `mapstructure:"historical-proof-burst"`
}

// DefaultStateCommitConfig returns the default StateCommitConfig
func DefaultStateCommitConfig() StateCommitConfig {
	return StateCommitConfig{
		Enable:        true,
		WriteMode:     CosmosOnlyWrite,
		ReadMode:      CosmosOnlyRead,
		MemIAVLConfig: memiavl.DefaultConfig(),
		FlatKVConfig:  flatkv.DefaultConfig(),

		HistoricalProofMaxInFlight: DefaultSCHistoricalProofMaxInFlight,
		HistoricalProofRateLimit:   DefaultSCHistoricalProofRateLimit,
		HistoricalProofBurst:       DefaultSCHistoricalProofBurst,
	}
}

// Validate checks if the StateCommitConfig is valid
func (c StateCommitConfig) Validate() error {
	if !c.WriteMode.IsValid() {
		return fmt.Errorf("invalid write_mode: %s", c.WriteMode)
	}
	if !c.ReadMode.IsValid() {
		return fmt.Errorf("invalid read_mode: %s", c.ReadMode)
	}
	return nil
}
