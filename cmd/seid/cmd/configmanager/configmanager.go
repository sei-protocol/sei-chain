package configmanager

import (
	"errors"
	"fmt"
	"os"
	"path"
	"runtime/debug"
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
	logAdvisory(validateAdvisory(cmd))
	return server.InterceptConfigsPreRunHandler(cmd, customAppConfigTemplate, customAppConfig)
}

// advisoryOutcome is what the validation pass saw. The pass reports rather than logs
// so that it is observable, which nothing else here can be: a channel-parity or
// never-refuses-boot assertion holds just as well when the read always fails or the
// validation has quietly become a no-op, so something has to be able to see that the
// pass ran and what it found. Apply does the logging.
type advisoryOutcome struct {
	// Home is the directory the pass read, empty when it never got that far.
	Home string
	// Stage names where the pass stopped, "resolve" or "read", and is empty when it
	// completed. It is what keeps the two failures separately reportable.
	Stage string
	// Skipped records that there was nothing to validate: no home resolved, or no
	// config on disk yet, which is the normal case on a fresh node.
	Skipped bool
	// Diagnostics are the rendered validation findings.
	Diagnostics []string
	// Err is a resolve or read failure, advisory like everything else here.
	Err error
	// Panic is a recovered panic value and Stack its origin. sei-config's read and
	// validate fidelity is still being hardened, so a panic here is the case most
	// likely to need debugging from a node's logs, and the value alone gives no origin.
	Panic any
	Stack []byte
}

// validateAdvisory resolves the home dir, reads the on-disk config and validates it,
// reporting what it saw. Every outcome is advisory: a failure, or a panic in the
// sei-config read or validate, is captured and returned rather than propagated, so
// the pass can never change what the node boots on. Keeping this a distinct step from
// Apply is what lets the generate path add its authoring/render step as a sibling.
//
// It runs before the legacy handler, and that ordering is deliberate: what it reports
// on is the configuration a node operator authored, not the configuration seid just
// generated for itself. The consequence is that a brand-new node is not validated on
// its first boot, since there is nothing on disk yet and the handler writes the files
// afterwards, so the earliest a diagnostic can appear is the second start.
//
// Running it after re-entry as well would close that, and it is deliberately not done
// yet, because sei-config today reports an error against a freshly generated default
// config (the pruning read-mapping gap the design tracks). Validating generated files
// before that gap closes would mean every fresh node logging an error about a file
// seid itself had just written, which is worse than validating a boot late. This is
// worth revisiting when validation goes fatal, and the two decisions belong together.
func validateAdvisory(cmd *cobra.Command) (out advisoryOutcome) {
	defer func() {
		if r := recover(); r != nil {
			out.Panic, out.Stack = r, debug.Stack()
		}
	}()

	home, err := resolveHomeDir(cmd)
	if err != nil {
		out.Stage, out.Err = "resolve", err
		return out
	}
	// An unresolved home would send the read at ./config relative to the process
	// working directory, so it could validate some unrelated node's files and report
	// diagnostics that have nothing to do with what this node boots on. The legacy
	// reader treats an empty home the same way, so this is not a parity break, but a
	// pass whose whole purpose is operator-facing diagnostics has to decline instead.
	if home == "" {
		out.Skipped = true
		return out
	}
	out.Home = home

	cfg, err := seiconfig.ReadConfigFromDir(home)
	if err != nil {
		// A missing config is the normal fresh-node case: the legacy handler creates it.
		if errors.Is(err, os.ErrNotExist) {
			out.Skipped = true
			return out
		}
		out.Stage, out.Err = "read", err
		return out
	}

	for _, d := range seiconfig.Validate(cfg).Diagnostics {
		out.Diagnostics = append(out.Diagnostics, d.String())
	}
	return out
}

// maxLoggedDiagnostics bounds the rendered list in one log line. A badly broken
// config can produce a diagnostic per field, and count is what an operator alerts
// on, so the full set is left to be re-derived from the file rather than emitted as
// one unbounded line.
const maxLoggedDiagnostics = 10

// logAdvisory reports an outcome through seilog. Nothing here refuses boot.
func logAdvisory(out advisoryOutcome) {
	switch {
	case out.Panic != nil:
		logger.Error("config validation panicked (advisory; recovered, node will boot)",
			"panic", out.Panic, "stack", string(out.Stack))
	case out.Stage == "resolve":
		logger.Warn("could not resolve home dir for config validation (advisory)", "error", out.Err)
	case out.Stage == "read":
		logger.Warn("could not read config for validation (advisory)", "error", out.Err)
	}

	if len(out.Diagnostics) == 0 {
		return
	}
	shown, omitted := out.Diagnostics, 0
	if len(shown) > maxLoggedDiagnostics {
		omitted = len(shown) - maxLoggedDiagnostics
		shown = shown[:maxLoggedDiagnostics]
	}
	logger.Warn("advisory config validation diagnostics (not enforced; node will boot)",
		"count", len(out.Diagnostics), "diagnostics", shown, "omitted", omitted)
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
