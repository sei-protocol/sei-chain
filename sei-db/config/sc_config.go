package config

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

const (
	DefaultSCHistoricalProofMaxInFlight = 1
	DefaultSCHistoricalProofRateLimit   = 1.0 // req/s, <=0 disables rate limit
	DefaultSCHistoricalProofBurst       = 1

	legacySCWriteModeCosmosOnly = "cosmos_only"
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
	// all_migrated_but_bank, migrate_bank, flatkv_only, test_only_dual_write.
	// defaults to memiavl_only.
	WriteMode WriteMode `mapstructure:"write-mode"`

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

	// The number of keys to migrate from memiavl to flatkv per block. Ignored if not in a migration mode.
	KeysToMigratePerBlock int `mapstructure:"keys-to-migrate-per-block"`
}

// DefaultStateCommitConfig returns the default StateCommitConfig
func DefaultStateCommitConfig() StateCommitConfig {
	return StateCommitConfig{
		Enable:                     true,
		WriteMode:                  MemiavlOnly,
		MemIAVLConfig:              memiavl.DefaultConfig(),
		FlatKVConfig:               *config.DefaultConfig(),
		HistoricalProofMaxInFlight: DefaultSCHistoricalProofMaxInFlight,
		HistoricalProofRateLimit:   DefaultSCHistoricalProofRateLimit,
		HistoricalProofBurst:       DefaultSCHistoricalProofBurst,
		KeysToMigratePerBlock:      1024,
	}
}

// ParseSCWriteMode converts the configured state-commit write mode to the
// current SC write-mode enum. v6.4/v6.5 app.toml files used "cosmos_only" for
// the same memIAVL-only routing that v6.6 calls "memiavl_only".
func ParseSCWriteMode(wm string) (WriteMode, error) {
	if wm == legacySCWriteModeCosmosOnly {
		return MemiavlOnly, nil
	}
	return ParseWriteMode(wm)
}

// Validate checks if the StateCommitConfig is valid
func (c StateCommitConfig) Validate() error {
	if !c.WriteMode.IsValid() {
		return fmt.Errorf("invalid write-mode: %s", c.WriteMode)
	}
	if c.KeysToMigratePerBlock <= 0 {
		return fmt.Errorf("keys-to-migrate-per-block must be greater than 0")
	}
	return nil
}
