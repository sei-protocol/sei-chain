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

// loggerSegments name this package's logger; loggerName derives from them.
var loggerSegments = []string{"cmd", "seid", "configmanager"}

var logger = seilog.NewLogger(loggerSegments[0], loggerSegments[1:]...)

// loggerName is the name the logger above is registered under.
var loggerName = strings.Join(loggerSegments, "/")

// ownReportingFloor is the level this package's reports are held at.
const ownReportingFloor = slog.LevelInfo

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

// SeiConfigManager validates the config through sei-config, runs the legacy handler, then installs
// sei.toml's resolved values. It never writes files or refuses a boot the legacy path allows.
type SeiConfigManager struct {
	// logger receives the reports; nil, the production value, means the package logger. Tests set it.
	logger *slog.Logger
}

// keepOwnReportingVisible lowers this package's logger, and only it, to ownReportingFloor. Call it after
// anything that sets the process-wide level.
func keepOwnReportingVisible() {
	// Leave a logger already at or below the floor alone; SetLevel assigns.
	if at, known := seilog.GetLevel(loggerName); known && at <= ownReportingFloor {
		return
	}

	// Zero means no logger matched. Written to stderr, since the logger may be too quiet to carry it.
	if seilog.SetLevel(loggerName, ownReportingFloor) == 0 {
		fmt.Fprintf(os.Stderr, "configmanager: reporting level for %q could not be held, so this "+
			"manager's reports may be silenced\n", loggerName)
	}
}

// log returns the logger to report through, and never returns nil.
func (m SeiConfigManager) log() *slog.Logger {
	if m.logger != nil {
		return m.logger
	}
	return logger
}

// Apply validates the operator's config, runs the legacy handler, reports the validation, and then
// installs sei.toml's resolved values. It returns only the legacy handler's error.
func (m SeiConfigManager) Apply(cmd *cobra.Command, customAppConfigTemplate string, customAppConfig any) error {
	// Before the handler, which marks flags changed from app.toml values.
	typed := TypedFlags(cmd)

	// Validate the files the operator wrote, before the handler generates any. Report after it, at the
	// log level it sets, and not deferred: see reportAdvisory.
	out := validateAdvisory(cmd)
	err := server.InterceptConfigsPreRunHandler(cmd, customAppConfigTemplate, customAppConfig)
	keepOwnReportingVisible()
	reportAdvisory(m.log(), out)
	if err != nil {
		return err
	}

	// After the handler, which builds the source the values go into.
	installResolved(cmd, typed, m.log())
	return nil
}

// reportAdvisory logs an advisory outcome, recovering a panic from the logging itself.
func reportAdvisory(lg *slog.Logger, out advisoryOutcome) {
	// The recover must stay in this closure: in reportAdvisory's own body, a deferred call would swallow
	// a panic from the legacy handler. TestApplyPropagatesALegacyHandlerPanic holds this.
	defer func() {
		if r := recover(); r != nil {
			// A second panic, from logging the first, must not escape.
			defer func() { _ = recover() }()
			lg.Error("config validation reporting panicked (advisory; recovered, node will boot)",
				"panic", r, "stack", string(debug.Stack()))
		}
	}()
	logAdvisory(lg, out)
}

// advisoryOutcome is what the validation pass saw, returned rather than logged so tests can observe it.
type advisoryOutcome struct {
	// Home is the directory the pass read, empty when it never got that far.
	Home string
	// Stage names where the pass stopped, and is stageNone when it completed.
	Stage stage
	// Skipped records that there was nothing to validate: no home resolved, or no
	// config on disk yet, which is the normal case on a fresh node.
	Skipped bool
	// Diagnostics are the rendered validation findings.
	Diagnostics []string
	// Err is a resolve or read failure, advisory like everything else here.
	Err error
	// Panic is a recovered panic value and Stack its origin.
	Panic any
	Stack []byte
}

// stage names how far the advisory pass got.
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

// validateAdvisory reads the on-disk config under the node's home and validates it with sei-config,
// capturing any failure or panic in the result. A node with no config yet is skipped.
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
	// An empty home would read ./config, which may be another node's.
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

// maxLoggedItems bounds the rendered list in one log line; the full count is logged beside it.
const maxLoggedItems = 10

// logAdvisory reports an outcome through seilog. Nothing here refuses boot.
func logAdvisory(lg *slog.Logger, out advisoryOutcome) {
	switch {
	case out.Panic != nil:
		lg.Error("config validation panicked (advisory; recovered, node will boot)",
			"panic", out.Panic, "stack", string(out.Stack))
	case out.Stage == stageResolve:
		lg.Warn("could not resolve home dir for config validation (advisory)", "error", out.Err)
	// Catches any stage without its own case.
	case out.Stage != stageNone:
		lg.Warn("config validation stopped early (advisory)",
			"stage", out.Stage, "error", out.Err)
	// A skip with a home is the ordinary fresh node, which stays quiet.
	case out.Skipped && out.Home == "":
		lg.Info("config validation skipped: no home dir resolved (advisory)")
	case !out.Skipped && out.Stage == stageNone && len(out.Diagnostics) == 0:
		lg.Info("config validation passed: no advisories (node will boot)", "home", out.Home)
	}

	if len(out.Diagnostics) == 0 {
		return
	}
	shown, omitted := capLoggedItems(out.Diagnostics)
	lg.Warn("advisory config validation diagnostics (not enforced; node will boot)",
		"home", out.Home, "count", len(out.Diagnostics), "diagnostics", shown, "omitted", omitted)
}

// capLoggedItems splits a list bound for one log line into the part to render and the number left out.
func capLoggedItems(items []string) (shown []string, omitted int) {
	if len(items) <= maxLoggedItems {
		return items, 0
	}
	return items[:maxLoggedItems], len(items) - maxLoggedItems
}

// resolveHomeDir resolves --home the same way the legacy handler in sei-cosmos/server/util.go does.
// TestResolveHomeDirAgreesWithTheLegacyHandler holds the two together.
//
// TODO: export the handler's resolution from sei-cosmos/server and call it here.
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

// Select maps SEI_CONFIG_MANAGER, matched exactly, to a manager: unset or "legacy" is Legacy, "v2" is
// Sei, and anything else is an error.
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
