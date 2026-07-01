// Package configmanager is the selection seam between the legacy config
// loader and the sei-config-backed manager, gated by the SEI_CONFIG_MANAGER
// environment variable.
//
// LegacyConfigManager re-enters the unchanged legacy handler verbatim (the
// default). SeiConfigManager reads the existing config through the sei-config
// library to *validate* it, then re-enters the same reader on the operator's
// original files (it does not rewrite them), so both channels are produced
// identically to legacy. See PLT-775 and the canonical design
// (bdchatham-designs designs/config-manager/DESIGN.md).
package configmanager

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	seiconfig "github.com/sei-protocol/sei-config"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
)

// EnvVar is the experimental gate that selects the configuration manager.
const EnvVar = "SEI_CONFIG_MANAGER"

// ConfigManager resolves a seid node's configuration during PersistentPreRunE.
//
// Load-bearing contract: an implementation must leave both channels the app
// consumes — serverCtx.Config and serverCtx.Viper — populated exactly as the
// legacy path does. The boot path does not re-render the legacy files: v2 reads
// the config to validate it, then re-enters the unchanged legacy reader on the
// operator's existing files, so the channels are identical to legacy by
// construction. (Authoring the canonical sei.toml and rendering the legacy
// files from it is the generate path; any implementation that writes config
// must be all-or-nothing on disk.)
//
// The Apply signature matches server.InterceptConfigsPreRunHandler so the
// legacy implementation forwards verbatim. This is an internal, single-consumer
// contract (only root.go calls it) and is free to grow when the generate path
// lands; the node dir is resolvable from cmd.
type ConfigManager interface {
	Apply(cmd *cobra.Command, customAppConfigTemplate string, customAppConfig any) error
}

// LegacyConfigManager is the default manager: it re-enters the unchanged
// legacy handler verbatim, so the legacy path stays byte-for-byte unaffected.
type LegacyConfigManager struct{}

// Apply delegates to the legacy interception handler unchanged.
func (LegacyConfigManager) Apply(cmd *cobra.Command, customAppConfigTemplate string, customAppConfig any) error {
	return server.InterceptConfigsPreRunHandler(cmd, customAppConfigTemplate, customAppConfig)
}

// SeiConfigManager resolves configuration through the sei-config library,
// selected by SEI_CONFIG_MANAGER=v2. It reads the config through the unified
// SeiConfig model and surfaces validation diagnostics, then re-enters the
// legacy reader on the operator's original files — it does NOT rewrite them,
// migrate (that is the explicit `seid config migrate`), or author sei.toml (the
// generate path). So the produced config is identical to legacy by
// construction; v2's boot value-add is the validation pass.
//
// Validation is ADVISORY in this MVP: issues are logged, not enforced, and a
// read/validate problem never refuses boot. sei-config's read fidelity for a
// real seid config is still being hardened, so a model gap must not break an
// otherwise-valid node. Promoting validation to fatal (the design's
// refuse-on-error criterion) is the un-defer once the read round-trips real
// fixtures.
type SeiConfigManager struct{}

// Apply surfaces sei-config validation diagnostics (advisory) and re-enters the
// legacy handler on the operator's original files. It does not write to disk,
// and never refuses boot on a read or validate problem.
//
// Validation runs against the on-disk config.toml/app.toml only (via
// ReadConfigFromDir) — NOT the fully-merged AppOptions the node boots on (no
// flag/env/in-code-default layering). So a default node logs a benign advisory
// today (e.g. `storage.pruning` empty, because the template omits the key that
// cosmos defaults in code); this is expected, not a regression. That lower
// fidelity is acceptable precisely because validation is advisory here — parity
// comes from re-entry, not from the validated struct.
func (SeiConfigManager) Apply(cmd *cobra.Command, customAppConfigTemplate string, customAppConfig any) error {
	if home, err := resolveHomeDir(cmd); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "config-manager v2: could not resolve home dir for validation (advisory): %v\n", err)
	} else if cfg, err := seiconfig.ReadConfigFromDir(home); err != nil {
		// A missing config.toml/app.toml (fresh home, or a partial home with one
		// file absent) is normal — the legacy handler creates it. Any other read
		// error is advisory, not fatal.
		if !errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "config-manager v2: could not read config for validation (advisory): %v\n", err)
		}
	} else if diags := seiconfig.Validate(cfg).Diagnostics; len(diags) > 0 {
		// Advisory in this MVP: the node still boots. Surface ALL diagnostics —
		// each carries its own [ERROR]/[WARNING]/[INFO] severity — so warnings
		// are not silently dropped alongside errors. SeverityError findings (e.g.
		// sc-write-mode) are the class legacy panics on later at app.New();
		// surfacing them here is earlier warning, not enforcement. Fatal
		// refuse-on-error is the un-defer.
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "config-manager v2: ADVISORY config validation diagnostics (not enforced; the node will boot, but SeverityError items may surface later, e.g. at app.New()): %v\n", diags)
	}

	// Re-enter the unchanged legacy reader on the operator's original files.
	return server.InterceptConfigsPreRunHandler(cmd, customAppConfigTemplate, customAppConfig)
}

// resolveHomeDir mirrors the legacy handler's home resolution exactly
// (sei-cosmos/server/util.go: BindPFlags over the command's flags + the SEID_
// env prefix + AutomaticEnv, then GetString(flags.FlagHome)), so the directory
// we materialize into is the same one the re-entered handler reads from.
// Resolving this single value the same way is not reimplementing the read tail
// — the read/merge/log-level tail stays delegated to InterceptConfigsPreRunHandler.
func resolveHomeDir(cmd *cobra.Command) (string, error) {
	v := viper.New()
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return "", err
	}
	if err := v.BindPFlags(cmd.PersistentFlags()); err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	v.SetEnvPrefix(path.Base(exe))
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	return v.GetString(flags.FlagHome), nil
}

// Select returns the ConfigManager chosen by the SEI_CONFIG_MANAGER value:
// unset or "legacy" -> LegacyConfigManager (the default); "v2" ->
// SeiConfigManager; any other value -> error (never a silent fallback).
// getenv is injected for testability; production callers pass os.Getenv.
//
// The value is matched exactly — no trimming or case-folding. This is
// deliberate: normalizing would broaden the gate, and the error names the
// legal tokens so an operator can self-correct.
func Select(getenv func(string) string) (ConfigManager, error) {
	switch v := getenv(EnvVar); v {
	case "", "legacy":
		return LegacyConfigManager{}, nil
	case "v2":
		return SeiConfigManager{}, nil
	default:
		return nil, fmt.Errorf("invalid %s=%q (want unset, \"legacy\", or \"v2\")", EnvVar, v)
	}
}
