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
	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
)

var logger = seilog.NewLogger("cmd", "seid", "configmanager")

// EnvVar gates which configuration manager seid uses.
const EnvVar = "SEI_CONFIG_MANAGER"

// ConfigManager resolves a seid node's configuration during PersistentPreRunE.
// An implementation must leave serverCtx.Config and serverCtx.Viper populated
// exactly as the legacy path does. The Apply signature matches
// server.InterceptConfigsPreRunHandler so the legacy manager forwards verbatim.
type ConfigManager interface {
	Apply(cmd *cobra.Command, customAppConfigTemplate string, customAppConfig any) error
}

// LegacyConfigManager is the default manager. It forwards to the legacy handler
// unchanged, leaving the legacy path byte-for-byte unaffected.
type LegacyConfigManager struct{}

// Apply forwards to the legacy interception handler unchanged.
func (LegacyConfigManager) Apply(cmd *cobra.Command, customAppConfigTemplate string, customAppConfig any) error {
	return server.InterceptConfigsPreRunHandler(cmd, customAppConfigTemplate, customAppConfig)
}

// SeiConfigManager validates the config through the sei-config library, then
// re-enters the legacy handler on the operator's original files. It never
// writes, migrates, or refuses boot.
type SeiConfigManager struct{}

// Apply runs the advisory validation pass, then re-enters the legacy handler on
// the operator's original files. Nothing in the validation pass refuses boot.
func (SeiConfigManager) Apply(cmd *cobra.Command, customAppConfigTemplate string, customAppConfig any) error {
	validateAdvisory(cmd)
	return server.InterceptConfigsPreRunHandler(cmd, customAppConfigTemplate, customAppConfig)
}

// validateAdvisory resolves the home dir, reads the on-disk config, and logs any
// validation diagnostics via seilog at Warn. Every step is advisory: a failure —
// or a panic in the sei-config read/validate, whose fidelity is still being
// hardened — is logged and swallowed so the pass can never change what the node
// boots on. A missing config file is normal (the legacy handler creates it) and
// is not surfaced. Keeping this a distinct step from Apply is what lets the
// generate path add its authoring/render step as a sibling.
func validateAdvisory(cmd *cobra.Command) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn("config validation panicked (advisory; recovered, node will boot)", "panic", r)
		}
	}()

	home, err := resolveHomeDir(cmd)
	if err != nil {
		logger.Warn("could not resolve home dir for config validation (advisory)", "error", err)
		return
	}
	cfg, err := seiconfig.ReadConfigFromDir(home)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Warn("could not read config for validation (advisory)", "error", err)
		}
		return
	}
	if diags := seiconfig.Validate(cfg).Diagnostics; len(diags) > 0 {
		msgs := make([]string, len(diags))
		for i, d := range diags {
			msgs[i] = d.String()
		}
		logger.Warn("advisory config validation diagnostics (not enforced; node will boot)",
			"count", len(diags), "diagnostics", msgs)
	}
}

// resolveHomeDir resolves --home the same way the legacy handler does
// (sei-cosmos/server/util.go), so v2 validates the directory the handler reads.
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

// Select maps SEI_CONFIG_MANAGER to a manager: unset or "legacy" -> Legacy,
// "v2" -> Sei, anything else -> error. The value is matched exactly (no
// trimming or case-folding) and never falls back silently. getenv is injected
// for tests; callers pass os.Getenv.
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
