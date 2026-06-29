package config

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

const (
	DefaultSCHistoricalProofMaxInFlight = 1
	DefaultSCHistoricalProofRateLimit   = 1.0 // req/s, <=0 disables rate limit
	DefaultSCHistoricalProofBurst       = 1
)

// StateCommitConfig defines configuration for the state commit (SC) layer.
type StateCommitConfig struct {
	// Enable defines if the state-commit (SeiDB) should be enabled.
	// defaults to true.
	Enable bool `mapstructure:"enable"`

	// Directory defines the state-commit store directory
	// If not explicitly set, default to application home directory
	Directory string `mapstructure:"directory"`

	// AsyncCommitBuffer defines the size of asynchronous commit queue
	// this greatly improve block catching-up performance, <= 0 means synchronous commit.
	// defaults to 100
	AsyncCommitBuffer int `mapstructure:"async-commit-buffer"`

	// WriteMode defines the write routing mode for EVM data.
	// Valid values: memiavl_only, migrate_evm, evm_migrated, migrate_all_but_bank,
	// all_migrated_but_bank, migrate_bank, flatkv_only, test_only_dual_write, auto.
	// defaults to migrate_evm. While the NumKeysToMigratePerBlock gov param is 0
	// (the default), migrate_evm is paused and produces the same app hash as
	// memiavl_only; raising the param via governance is the sole migration trigger.
	WriteMode types.WriteMode `mapstructure:"write-mode"`

	// MemIAVLConfig is the configuration for the MemIAVL (Cosmos) backend
	MemIAVLConfig memiavl.Config

	// FlatKVConfig is the configuration for the FlatKV (EVM) backend
	FlatKVConfig config.Config

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
		Enable:                     true,
		WriteMode:                  types.MigrateEVM,
		MemIAVLConfig:              memiavl.DefaultConfig(),
		FlatKVConfig:               *config.DefaultConfig(),
		HistoricalProofMaxInFlight: DefaultSCHistoricalProofMaxInFlight,
		HistoricalProofRateLimit:   DefaultSCHistoricalProofRateLimit,
		HistoricalProofBurst:       DefaultSCHistoricalProofBurst,
	}
}

// Validate checks if the StateCommitConfig is valid
func (c StateCommitConfig) Validate() error {
	if !c.WriteMode.IsValid() {
		return fmt.Errorf("invalid write-mode: %s", c.WriteMode)
	}
	return nil
}
