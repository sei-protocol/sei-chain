// Package configmanager is the selection seam between the legacy config
// loader and the (forthcoming) sei-config-backed manager, gated by the
// SEI_CONFIG_MANAGER environment variable.
//
// PR1 ships the seam only: LegacyConfigManager re-enters the unchanged
// legacy handler verbatim (the default), and SeiConfigManager is a
// not-yet-implemented stub that does not import the sei-config library.
// The v2 body lands in a follow-up PR. See PLT-775 and the canonical
// design (bdchatham-designs designs/config-manager/DESIGN.md).
package configmanager

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
)

// EnvVar is the experimental gate that selects the configuration manager.
const EnvVar = "SEI_CONFIG_MANAGER"

// ConfigManager resolves a seid node's configuration during PersistentPreRunE.
//
// Load-bearing contract: an implementation must leave both channels the app
// consumes — serverCtx.Config and serverCtx.Viper — populated exactly as the
// legacy path does, and must be all-or-nothing with respect to on-disk state
// (no partial config.toml/app.toml materialization on error). A v2 manager
// that authors the two files from the sei-config library must write BOTH
// atomically and then re-enter the legacy read tail so the channels are
// produced identically — it must not feed app.New from an in-memory struct.
//
// The Apply signature matches server.InterceptConfigsPreRunHandler so the
// legacy implementation forwards verbatim. This is an internal,
// single-consumer contract (only root.go calls it) and is free to grow — an
// explicit home dir or a Prepare/Apply split — when the v2 write-then-re-enter
// body lands in a follow-up PR; the node dir is resolvable from cmd, so the
// signature is sufficient for PR1's seam.
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

// SeiConfigManager will resolve configuration through the sei-config library
// behind SEI_CONFIG_MANAGER=v2. PR1 ships the seam only; the real body lands
// in a follow-up PR. It intentionally does not import sei-config.
type SeiConfigManager struct{}

// Apply is not yet implemented in PR1. It returns a hard error rather than
// silently falling back to the legacy path, so a v2 invocation is observable.
func (SeiConfigManager) Apply(_ *cobra.Command, _ string, _ any) error {
	return fmt.Errorf("%s=v2 not yet implemented (PR1 seam only)", EnvVar)
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
