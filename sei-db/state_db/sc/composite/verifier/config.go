// Package verifier implements the mainnet-shadow FlatKV correctness harness
// described in docs/flatkv-shadow-runbook.md.
//
// It treats memiavl as source of truth and runs up to six layered oracles
// against a CompositeCommitStore running with WriteMode=dual_write. The
// harness never mutates the composite store; it only reads from both
// backends and emits metrics / logs when they disagree.
package verifier

import (
	"os"
	"strconv"
)

// Env var names. Kept stable so ops can flip them without code changes.
const (
	envOracleWrite        = "FLATKV_ORACLE_WRITE"
	envOracleWritePanic   = "FLATKV_ORACLE_WRITE_PANIC"
	envOracleScanInterval = "FLATKV_ORACLE_SCAN_INTERVAL"
	envOracleLtHashEvery  = "FLATKV_ORACLE_LTHASH_INTERVAL"
	envOracleHistEvery    = "FLATKV_ORACLE_HIST_INTERVAL"
	envOracleHistLag      = "FLATKV_ORACLE_HIST_LAG"
	envOracleSampleLimit  = "FLATKV_ORACLE_SAMPLE_LIMIT"

	defaultHistLag = 1000
)

// Config controls which oracles run and at what cadence. Built from env vars
// so ops can change behavior on restart without a config schema change.
type Config struct {
	// WriteEnabled enables Oracle 1 (per-block write-time diff).
	WriteEnabled bool

	// WritePanic causes Oracle 1 to panic on any mismatch instead of
	// logging. Only meaningful when WriteEnabled.
	WritePanic bool

	// ScanInterval runs Oracle 2 (forward-subset full scan) every N
	// commits. 0 disables.
	ScanInterval int64

	// LtHashInterval runs Oracle 3 (LtHash self-consistency) every N
	// commits. 0 disables.
	LtHashInterval int64

	// HistInterval runs Oracle 4 (historical-version diff) every N
	// commits. 0 disables.
	HistInterval int64

	// HistLag is the number of commits to lag behind the current version
	// when running Oracle 4; ensures the SS async buffer has drained and
	// memiavl snapshot for that version exists.
	HistLag int64

	// SampleLimit caps the number of FlatKV rows Oracle 2 examines per
	// run. 0 means full scan.
	SampleLimit int64
}

// LoadFromEnv builds a Config from process env vars. All fields default
// to disabled so merely deploying the binary has no runtime cost.
func LoadFromEnv() Config {
	return Config{
		WriteEnabled:   envBool(envOracleWrite),
		WritePanic:     envBool(envOracleWritePanic),
		ScanInterval:   envInt64(envOracleScanInterval, 0),
		LtHashInterval: envInt64(envOracleLtHashEvery, 0),
		HistInterval:   envInt64(envOracleHistEvery, 0),
		HistLag:        envInt64(envOracleHistLag, defaultHistLag),
		SampleLimit:    envInt64(envOracleSampleLimit, 0),
	}
}

// AnyEnabled returns true if at least one oracle is turned on. Used to
// short-circuit construction when nothing is configured.
func (c Config) AnyEnabled() bool {
	return c.WriteEnabled ||
		c.ScanInterval > 0 ||
		c.LtHashInterval > 0 ||
		c.HistInterval > 0
}

func envBool(name string) bool {
	v := os.Getenv(name)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false
	}
	return b
}

func envInt64(name string, def int64) int64 {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return def
	}
	return n
}
