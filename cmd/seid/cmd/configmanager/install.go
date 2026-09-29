package configmanager

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/sei-protocol/sei-chain/config/appopts"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"

	// Sections register on import; a section nothing imports is silently undeclared.
	_ "github.com/sei-protocol/sei-chain/config/cosmosbase"

	_ "github.com/sei-protocol/sei-chain/config/tendermintbase"
)

// seiTomlName is the file this manager reads.
const seiTomlName = "sei.toml"

// appliedNone is the log attribute marking an outcome that installed nothing.
var appliedNone = slog.String("applied", "none")

// installResolved puts the values sei.toml supplies into the source the boot has just built. Every way
// this can fail installs nothing and leaves each key reading as it always has.
func installResolved(cmd *cobra.Command, typed map[string]string, log *slog.Logger) {
	// A panic must not refuse the boot, including one from logging the first.
	defer func() {
		if r := recover(); r != nil {
			defer func() { _ = recover() }()
			log.Error("installing this node's configuration panicked", appliedNone,
				"panic", r, "stack", string(debug.Stack()))
		}
	}()

	ctx := server.GetServerContextFromCmd(cmd)
	if ctx == nil || ctx.Viper == nil {
		log.Warn("no configuration source to install into", appliedNone)
		return
	}

	file, ok := readSeiToml(cmd, log)
	if !ok {
		return
	}
	mode, err := file.Mode()
	if err != nil {
		log.Warn("sei.toml records no usable node mode", appliedNone, "err", err)
		return
	}
	written, err := file.Values()
	if err != nil {
		log.Warn("cannot read the values sei.toml writes", appliedNone, "err", err)
		return
	}

	resolved, err := registry.Resolve(registry.Mode(mode), everyChannelAnOperatorCanUse(written, typed))
	if err != nil {
		log.Warn("cannot resolve this node's configuration", appliedNone,
			"mode", mode, "err", err)
		return
	}
	// A refused registration makes its keys look unknown, so report it first.
	reportWhatThisBinaryCouldNotUse(resolved, log)

	reportWhatTheFileDidNotReach(resolved, log)

	if !theFileNamesTheKindThisNodeRuns(ctx, mode, log) {
		return
	}

	// Routine lines are debug on every subcommand but start; problems report everywhere.
	said := log.Info
	if !runsANode(cmd) {
		said = log.Debug
	}

	forADecode, ownedByADecode := registry.ResolvedAndOwnedByDecodedSections(resolved)
	forALookup := everyKeyALookupReads(resolved, ownedByADecode)

	// Drop, per key, values a reader would silently coerce. A dropped key still reads its SEID_ variable
	// through the boot's AutomaticEnv, as it did before this manager (PLT-1136).
	if bad := whatDecodesToSomethingElse(whatEachDeclaredKeyHolds(registry.Mode(mode)),
		forALookup.Values); len(bad) > 0 {
		for key := range bad {
			delete(forALookup.Values, key)
		}
		shown, omitted := capLoggedItems(problemsInOrder(bad))
		log.Error("these written values would reach their setting as something other than what they say, "+
			"so none of them is installed and each reads as it always has",
			"count", len(bad), "written", strings.Join(shown, "; "), "omitted", omitted)
	}

	// Before the decoded delivery: Install refuses all or nothing, so it must fail before any section is
	// published.
	report, err := appopts.Install(ctx.Viper, forALookup)
	if err != nil {
		log.Warn("cannot install this node's configuration", appliedNone, "err", err)
		return
	}

	deliverDecodedSections(ctx, forADecode, log, said)

	// After both deliveries, since the reports above say nothing was applied.
	applyTheLevelTheStructNowHolds(ctx, log)
	said("configuration installed", "mode", mode,
		"count", len(report.Installed),
		"read_here_first_count", len(report.Added))
}

// runsANode reports whether this command runs a node, which decides the level routine reports use.
func runsANode(cmd *cobra.Command) bool {
	return cmd != nil && cmd.Name() == "start"
}

// everyChannelAnOperatorCanUse returns the file, environment and typed-flag sources for a resolution.
// Omitting one would install a lower layer over it.
func everyChannelAnOperatorCanUse(written map[string]any, typed map[string]string) registry.Sources {
	return registry.Sources{
		File:      written,
		LookupEnv: os.LookupEnv,
		Flags:     flagValues(typed),
	}
}

// everyKeyALookupReads returns the resolved values minus the keys owned by decoded sections.
func everyKeyALookupReads(resolved registry.Resolved, ownedByADecode []string) registry.Resolved {
	owning := make(map[string]bool, len(ownedByADecode))
	for _, key := range ownedByADecode {
		owning[key] = true
	}

	out := registry.Resolved{Values: make(map[string]any, len(resolved.Values))}
	for key, value := range resolved.Values {
		if owning[key] {
			continue
		}
		out.Values[key] = value
	}
	return out
}

// reportWhatThisBinaryCouldNotUse logs the registry's defects, which are bugs in this binary.
func reportWhatThisBinaryCouldNotUse(resolved registry.Resolved, log *slog.Logger) {
	if len(resolved.Refused) == 0 {
		return
	}
	said := make([]string, 0, len(resolved.Refused))
	for _, d := range resolved.Refused {
		said = append(said, fmt.Sprintf("%s: %v", d.Section, d.Err))
	}
	sort.Strings(said)
	shown, omitted := capLoggedItems(said)
	log.Error("this binary's own configuration registration carries a defect, so some declared keys do "+
		"not reach the node as declared; each line below says which and how",
		"count", len(said), "refused", strings.Join(shown, "; "), "omitted", omitted)
}

// reportWhatTheFileDidNotReach logs keys sei.toml writes that no section declares, and environment
// variables set for keys the environment cannot carry. Unknown flags are not reported.
func reportWhatTheFileDidNotReach(resolved registry.Resolved, log *slog.Logger) {
	if len(resolved.UnknownInFile) > 0 {
		shown, omitted := capLoggedItems(resolved.UnknownInFile)
		log.Warn("sei.toml writes keys no section declares; they have no effect",
			"count", len(resolved.UnknownInFile), "keys", strings.Join(shown, ","), "omitted", omitted)
	}
	for _, key := range sortedKeys(resolved.Ignored) {
		log.Warn("an environment variable is set for a key the environment cannot supply; it has no "+
			"effect and whatever was written elsewhere applies", "key", key,
			"variable", registry.EnvName(key), "why", resolved.Ignored[key])
	}
}

// sortedKeys returns a map's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// theFileNamesTheKindThisNodeRuns reports whether sei.toml's mode agrees with the mode the node runs as,
// logging the disagreement. On disagreement nothing is delivered, since every resolved value answers for
// the wrong kind of node.
func theFileNamesTheKindThisNodeRuns(ctx *server.Context, mode string, log *slog.Logger) bool {
	if ctx == nil || ctx.Config == nil {
		return true
	}
	running := ctx.Config.Mode
	if !modesDisagree(mode, running) {
		return true
	}
	log.Error("sei.toml says this is one kind of node and the node's own configuration file says another, "+
		"so nothing is delivered and every key reads as it always has. Every value resolved here is the "+
		"answer for the first and the node runs as the second",
		"sei.toml", mode, "running", running)
	return false
}

// modesDisagree reports whether the recorded and running modes differ. An archive node runs as "full",
// since config.toml has no archive mode; an empty running mode disagrees with every recorded one.
func modesDisagree(recorded, running string) bool {
	if recorded == running {
		return false
	}
	return recorded != string(registry.ModeArchive) || running != string(registry.ModeFull)
}

// OwnReportingEnabledForTest reports whether this package's logger would emit at the level its reports use.
func OwnReportingEnabledForTest() bool {
	return logger.Enabled(context.Background(), ownReportingFloor)
}

// readSeiToml loads the node's sei.toml. Its absence is logged at debug; any other failure warns.
func readSeiToml(cmd *cobra.Command, log *slog.Logger) (*seitoml.File, bool) {
	home, err := resolveHomeDir(cmd)
	if err != nil {
		log.Warn("cannot resolve the home directory", appliedNone, "err", err)
		return nil, false
	}
	// An empty home would read ./config, which may be another node's.
	if home == "" {
		log.Warn("no home directory is set, so there is nowhere to read sei.toml from", appliedNone)
		return nil, false
	}
	file, err := readSeiTomlAt(home)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		log.Debug("no sei.toml", appliedNone, "home", home)
		return nil, false
	case err != nil:
		log.Warn("this node's sei.toml cannot be read", appliedNone,
			"home", home, "err", err)
		return nil, false
	}
	return file, true
}

// readSeiTomlAt loads the sei.toml under a home directory.
func readSeiTomlAt(home string) (*seitoml.File, error) {
	file, err := seitoml.Load(filepath.Join(home, "config", seiTomlName))
	if err != nil {
		return nil, err
	}
	return file, nil
}

// TypedFlags records which flags this invocation carried. It records them as they are when it runs, so a
// caller has to run it before anything calls Set on one.
func TypedFlags(cmd *cobra.Command) map[string]string {
	out := map[string]string{}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			out[strings.ToLower(f.Name)] = f.Value.String()
		}
	})
	return out
}

// flagValues renders a snapshot of typed flags as a configuration source, under the keys the sections
// declare. A flag matching no declared key is left under its own name.
func flagValues(typed map[string]string) map[string]any {
	if len(typed) == 0 {
		return nil
	}
	// Matched by environment spelling, which equates dots, hyphens and underscores; flags use underscores
	// where keys use hyphens. The registry keeps these spellings unique.
	byEnvName := map[string]string{}
	for _, key := range registry.Keys() {
		byEnvName[registry.EnvName(key)] = key
	}

	out := make(map[string]any, len(typed))
	for name, value := range typed {
		key := name
		if declared, ok := byEnvName[registry.EnvName(name)]; ok {
			key = declared
		}
		out[key] = value
	}
	return out
}
