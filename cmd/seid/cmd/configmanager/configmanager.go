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
	reportAdvisory(cmd)
	return server.InterceptConfigsPreRunHandler(cmd, customAppConfigTemplate, customAppConfig)
}

// reportAdvisory runs the advisory pass and reports what it found, containing a panic
// from either step.
//
// The recover has to cover the reporting and not just the pass. Everything here is
// advisory, so the one promise this manager makes is that it cannot refuse a boot the
// legacy path would have allowed, and a panic escaping the reporter would do exactly
// that by propagating out of Apply into PersistentPreRunE. Keeping the pass's own
// recover and adding none here would leave that hole open, since the pass returns
// normally and the reporter runs after it.
func reportAdvisory(cmd *cobra.Command) {
	defer func() {
		if recover() != nil {
			// Deliberately the smallest possible call: this runs after something already
			// panicked, so it does not touch the value that may have caused it.
			logger.Error("config validation reporting panicked (advisory; recovered, node will boot)")
		}
	}()
	logAdvisory(validateAdvisory(cmd))
}

// advisoryOutcome is what the validation pass saw. The pass reports rather than logs
// so that it is observable, which nothing else here can be: a channel-parity or
// never-refuses-boot assertion holds just as well when the read always fails or the
// validation has quietly become a no-op, so something has to be able to see that the
// pass ran and what it found. reportAdvisory does the logging.
type advisoryOutcome struct {
	// Home is the directory the pass read, empty when it never got that far.
	Home string
	// Stage names where the pass stopped, and is stageNone when it completed. It is
	// what keeps the two failures separately reportable.
	Stage stage
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

// stage names how far the advisory pass got. It is a typed constant rather than a
// string because it is an internal discriminator matched by name, so a mistyped value
// should be a compile error instead of a case that silently drops an operator warning.
type stage int

const (
	stageNone    stage = iota // the pass completed
	stageResolve              // stopped resolving the home dir
	stageRead                 // stopped reading the config
)

// String names the stage for a log line.
func (s stage) String() string {
	switch s {
	case stageNone:
		return "none"
	case stageResolve:
		return "resolve"
	case stageRead:
		return "read"
	default:
		return fmt.Sprintf("stage(%d)", int(s))
	}
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
		out.Stage, out.Err = stageResolve, err
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
		out.Stage, out.Err = stageRead, err
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
	case out.Stage == stageResolve:
		logger.Warn("could not resolve home dir for config validation (advisory)", "error", out.Err)
	// Any other non-completing stage still reports, so a stage added without its own
	// case above logs something rather than nothing. The phrasing is neutral for the
	// same reason: naming a step here would misreport a stage added later, and the
	// stage attribute carries which one it actually was.
	case out.Stage != stageNone:
		logger.Warn("config validation stopped early (advisory)",
			"stage", out.Stage, "error", out.Err)
	// An unresolved home is closer to a misconfiguration than to a quiet default, and
	// it is reported so an operator who opted into v2 can tell a declined pass from a
	// pass that ran and found nothing. Home is what separates the two skips: it is
	// empty only when the home never resolved, and the missing-config skip below it
	// is the ordinary fresh-node case, which stays quiet.
	case out.Skipped && out.Home == "":
		logger.Info("config validation skipped: no home dir resolved (advisory)")
	}

	if len(out.Diagnostics) == 0 {
		return
	}
	shown, omitted := capDiagnostics(out.Diagnostics)
	// The home is reported because a resolveHomeDir that drifted from the legacy
	// handler would have these diagnostics describe a directory the node is not
	// booting on, and without the path in the line there is no way to tell from a log.
	logger.Warn("advisory config validation diagnostics (not enforced; node will boot)",
		"home", out.Home, "count", len(out.Diagnostics), "diagnostics", shown, "omitted", omitted)
}

// capDiagnostics splits a diagnostic list into the part to render and the number left
// out. It is separate from logAdvisory so the arithmetic can be asserted directly:
// an off-by-one or an inverted omitted count is not visible in a log line anyone reads.
func capDiagnostics(diags []string) (shown []string, omitted int) {
	if len(diags) <= maxLoggedDiagnostics {
		return diags, 0
	}
	return diags[:maxLoggedDiagnostics], len(diags) - maxLoggedDiagnostics
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
