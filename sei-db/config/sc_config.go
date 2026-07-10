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

	// WriteMode is the fixed write routing mode used only when WriteModeEnableAuto
	// is false. Valid values: memiavl_only, migrate_evm, evm_migrated,
	// migrate_all_but_bank, all_migrated_but_bank, migrate_bank, flatkv_only,
	// test_only_dual_write, auto. Defaults to memiavl_only — the safe
	// pre-migration fallback for a node that has explicitly opted out of auto.
	WriteMode types.WriteMode `mapstructure:"write-mode"`

	// WriteModeEnableAuto, when true (the default), forces the node to run in
	// auto and ignores any explicit WriteMode: the effective mode is derived at
	// startup from the persisted migration metadata (memiavl_only before
	// migration has started, migrate_evm while EVM keys are still draining,
	// evm_migrated once complete), and raising the NumKeysToMigratePerBlock gov
	// param above 0 advances an auto store from memiavl_only to migrate_evm.
	//
	// Defaulting to true (and treating an absent config key as true) is what
	// lets the existing fleet participate: nodes provisioned by older binaries
	// carry an explicit sc-write-mode = "memiavl_only" but no
	// sc-write-mode-enable-auto key, so they still resolve to auto and follow a
	// governance-driven migration without any app.toml edit.
	//
	// Set this to false to honor the explicit WriteMode (memiavl_only,
	// flatkv_only, test_only_dual_write, ...) as a deliberate pin. A node pinned
	// this way does not participate in a governance-driven migration and will
	// diverge once the chain migrates.
	WriteModeEnableAuto bool `mapstructure:"write-mode-enable-auto"`

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

	// HashLogger configures the per-block hash logger (a debugging/forensics tool). Enabled by default.
	// Loaded via explicit sc-hash-logger-* flag reads in app.parseSCConfigs, not mapstructure.
	HashLogger HashLoggerConfig
}

// DefaultStateCommitConfig returns the default StateCommitConfig
func DefaultStateCommitConfig() StateCommitConfig {
	return StateCommitConfig{
		Enable:                     true,
		WriteMode:                  types.MemiavlOnly,
		WriteModeEnableAuto:        true,
		MemIAVLConfig:              memiavl.DefaultConfig(),
		FlatKVConfig:               *config.DefaultConfig(),
		HistoricalProofMaxInFlight: DefaultSCHistoricalProofMaxInFlight,
		HistoricalProofRateLimit:   DefaultSCHistoricalProofRateLimit,
		HistoricalProofBurst:       DefaultSCHistoricalProofBurst,
		HashLogger:                 DefaultHashLoggerConfig(),
	}
}

// ApplyWriteModeAuto resolves the effective write mode from the
// sc-write-mode-enable-auto flag and the explicitly configured sc-write-mode.
//
// When auto is enabled the node always runs in auto, regardless of any explicit
// sc-write-mode — the explicit value is ignored. To pin an explicit mode
// (memiavl_only, flatkv_only, test_only_dual_write, ...) the operator must set
// sc-write-mode-enable-auto = false; only then is the configured sc-write-mode
// honored.
func ApplyWriteModeAuto(enableAuto bool, mode types.WriteMode) types.WriteMode {
	if enableAuto {
		return types.Auto
	}
	return mode
}

// ParseSCWriteMode converts the configured state-commit write mode to the
// current SC write-mode enum. v6.4/v6.5 app.toml files used "cosmos_only" for
// the same memIAVL-only routing that v6.6 calls "memiavl_only".
func ParseSCWriteMode(wm string) (types.WriteMode, error) {
	if wm == legacySCWriteModeCosmosOnly {
		return types.MemiavlOnly, nil
	}
	return types.ParseWriteMode(wm)
}

// Validate checks if the StateCommitConfig is valid
func (c StateCommitConfig) Validate() error {
	if !c.WriteMode.IsValid() {
		return fmt.Errorf("invalid write-mode: %s", c.WriteMode)
	}
	return nil
}

// AlignFlatKVWithMemIAVL applies the FlatKV-follows-memIAVL policy that the app
// config layer imposes because FlatKV's snapshot knobs are not independently
// exposed in app.toml. It is applied by app config parsing after the raw flags
// are read, so the underlying config layers stay a faithful mapping of
// app.toml/flags:
//   - the memIAVL snapshot-keep-recent is floored to memiavl.MinSnapshotKeepRecent
//     (a configured 0, "keep only the current snapshot", becomes 1); and
//   - FlatKV's snapshot cadence mirrors the (floored) memIAVL settings so both
//     backends checkpoint and prune in lockstep.
func (c *StateCommitConfig) AlignFlatKVWithMemIAVL() {
	c.MemIAVLConfig.SnapshotKeepRecent = memiavl.NormalizeSnapshotKeepRecent(c.MemIAVLConfig.SnapshotKeepRecent)
	c.FlatKVConfig.SnapshotInterval = c.MemIAVLConfig.SnapshotInterval
	c.FlatKVConfig.SnapshotKeepRecent = c.MemIAVLConfig.SnapshotKeepRecent
}
