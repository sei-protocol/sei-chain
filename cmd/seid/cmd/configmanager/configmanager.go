package configmanager

import (
	"errors"
	"fmt"
	"log/slog"
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

// seiTomlName is the configuration file this manager reads resolved values from.
//
// Named here rather than taken from the command package, so the boot does not depend on the verbs that
// write the file. A test holds the two spellings against each other.
const seiTomlName = "sei.toml"

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
type SeiConfigManager struct {
	// logger reports the advisory outcome, and a nil one means the package logger.
	// Select and every other caller build the zero value, so the nil case is the
	// production path rather than a fallback: the accessor below is what keeps it
	// from being a nil dereference, which in Apply would refuse a boot the legacy
	// path allowed. It exists so a test can read what Apply reported without
	// reassigning package state that a parallel test could race.
	logger *slog.Logger
}

// log returns the logger to report through, and never returns nil.
func (m SeiConfigManager) log() *slog.Logger {
	if m.logger != nil {
		return m.logger
	}
	return logger
}

// Apply validates the operator's config, re-enters the legacy handler on the original
// files, then reports what the validation found. Validation runs before re-entry so it
// reads the files the operator authored rather than the ones the handler generates. The
// reporting runs after so its lines are emitted at the log level the handler applies
// (seilog.SetDefaultLevel), not the pre-config default: without this, a node with
// log_level = "error" still gets the advisory lines and a higher default would drop them,
// which for a pass whose only output is operator-facing defeats it. The outcome is
// reported even when the handler errors, so a boot that fails still gets the advisory.
// Nothing in either step refuses a boot the legacy path would have allowed.
//
// The report is deliberately not deferred. Deferring it as written would be harmless,
// because reportAdvisory's recover sits in a closure one frame too deep to see a panic
// raised here. It stops being harmless the moment that recover is hoisted into
// reportAdvisory's own body, which is a plausible edit given validateAdvisory below uses
// exactly that idiom: a deferred report would then recover a panic from the legacy
// handler and return nil, turning a boot the legacy path aborts into a successful one.
// TestApplyPropagatesALegacyHandlerPanic fails on that combination.
func (m SeiConfigManager) Apply(cmd *cobra.Command, customAppConfigTemplate string, customAppConfig any) error {
	// Before the handler, because the handler copies configuration values into flags and marks them
	// changed. Afterwards there is no way to tell a flag the operator typed from a key their app.toml
	// holds, and treating the second as the first would put app.toml above sei.toml.
	typed := TypedFlags(cmd)
	out := validateAdvisory(cmd)
	err := server.InterceptConfigsPreRunHandler(cmd, customAppConfigTemplate, customAppConfig)
	reportAdvisory(m.log(), out)
	if err != nil {
		return err
	}
	// After the handler, because the source it builds is the one the resolved values go into, and it
	// does not exist before. Nothing this does can refuse boot.
	installResolved(cmd, typed, m.log())
	return nil
}

// reportAdvisory logs an advisory outcome, containing a panic from the logging itself.
//
// Everything here is advisory, so the one promise this manager makes is that it cannot
// refuse a boot the legacy path would have allowed, and a panic escaping this reporter
// would do exactly that by propagating out of Apply into PersistentPreRunE. The pass that
// produced out has its own recover, so the remaining exposure is the log call, which the
// deferred recover below contains. logAdvisory is proven panic-free on every outcome, so
// this only fires for a logger broken independent of its arguments.
func reportAdvisory(lg *slog.Logger, out advisoryOutcome) {
	// The recover below stays inside this closure. Hoisted into reportAdvisory's own body
	// it would see a panic unwinding a caller's frame whenever this function is deferred,
	// so a deferred report in Apply would recover the legacy handler's panic and boot a
	// node the legacy path refuses. TestApplyPropagatesALegacyHandlerPanic fails on that
	// pair, and neither half fails on its own.
	defer func() {
		if r := recover(); r != nil {
			// A second panic, from logging the first, must not escape. The nested recover
			// makes recording the recovered value and stack safe, and it is worth
			// recording: a reporter that swallows its own failure blind is the case least
			// debuggable from a node's logs. This mirrors what the pass captures for a
			// panic in validateAdvisory.
			defer func() { _ = recover() }()
			lg.Error("config validation reporting panicked (advisory; recovered, node will boot)",
				"panic", r, "stack", string(debug.Stack()))
		}
	}()
	logAdvisory(lg, out)
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
// Running it after re-entry as well would also validate on boot #1, and it is
// deliberately not done yet, because sei-config today reports an error against a freshly
// generated default config: its pruning keys are read under names the generator does not
// write, so the value it looks for is never there. Note this
// ordering DEFERS that spurious warning by one boot rather than avoiding it: from the
// second boot on, the pre-handler pass reads the same seid-generated, operator-untouched
// config and logs the pruning-gap diagnostic until the gap closes, so a v2 node that
// never touches its config still warns on every start. Validating after re-entry too
// would only add the same warning on boot #1. Closing the pruning gap is therefore a
// prerequisite for wider v2 rollout, not just for making validation fatal; revisit the
// two together.
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
func logAdvisory(lg *slog.Logger, out advisoryOutcome) {
	switch {
	case out.Panic != nil:
		lg.Error("config validation panicked (advisory; recovered, node will boot)",
			"panic", out.Panic, "stack", string(out.Stack))
	case out.Stage == stageResolve:
		lg.Warn("could not resolve home dir for config validation (advisory)", "error", out.Err)
	// Any other non-completing stage still reports, so a stage added without its own
	// case above logs something rather than nothing. The phrasing is neutral for the
	// same reason: naming a step here would misreport a stage added later, and the
	// stage attribute carries which one it actually was.
	case out.Stage != stageNone:
		lg.Warn("config validation stopped early (advisory)",
			"stage", out.Stage, "error", out.Err)
	// An unresolved home is closer to a misconfiguration than to a quiet default, and
	// it is reported so an operator who opted into v2 can tell a declined pass from a
	// pass that ran and found nothing. Home is what separates the two skips: it is
	// empty only when the home never resolved, and the missing-config skip below it
	// is the ordinary fresh-node case, which stays quiet.
	case out.Skipped && out.Home == "":
		lg.Info("config validation skipped: no home dir resolved (advisory)")
	// The pass completed and found nothing. Report at Info so an operator who opted into
	// v2 can see it ran clean, distinct from the quiet fresh-node skip (Skipped with a
	// home), which stays silent because the legacy handler is about to write those files.
	case !out.Skipped && out.Stage == stageNone && len(out.Diagnostics) == 0:
		lg.Info("config validation passed: no advisories (node will boot)", "home", out.Home)
	}

	if len(out.Diagnostics) == 0 {
		return
	}
	shown, omitted := capDiagnostics(out.Diagnostics)
	// The home is reported because a resolveHomeDir that drifted from the legacy
	// handler would have these diagnostics describe a directory the node is not
	// booting on, and without the path in the line there is no way to tell from a log.
	lg.Warn("advisory config validation diagnostics (not enforced; node will boot)",
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
//
// TODO: this re-implements ~15 lines of the legacy handler's viper bootstrap
// (sei-cosmos/server/util.go). Extract an exported server.ResolveHomeDir(cmd) and call
// it from both sides once the resolver lands and this is load-bearing for more than
// diagnostics; until then TestResolveHomeDirAgreesWithTheLegacyHandler guards the drift.
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
