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

	// The sections whose keys belong to the upstream server. A section reaches the registry through its
	// owning package's initialisation. A section nothing imports is therefore absent from what this
	// installs, and absent silently: an undeclared key keeps whatever answered it before.
	_ "github.com/sei-protocol/sei-chain/config/cosmosbase"

	// The sections whose keys belong to the node's own configuration file. These are the ones read by a
	// decode, so they are left out of what this installs.
	_ "github.com/sei-protocol/sei-chain/config/tendermintbase"
)

// seiTomlName is the file this manager reads.
const seiTomlName = "sei.toml"

// appliedNone marks an outcome that installed nothing. One field rather than the same sentence repeated on
// each message, so a fleet can match on it instead of on message text.
var appliedNone = slog.String("applied", "none")

// installResolved puts the values sei.toml supplies into the source the boot has just built. Every way
// this can fail installs nothing and leaves each key reading as it always has.
func installResolved(cmd *cobra.Command, typed map[string]string, log *slog.Logger) {
	// A panic here would refuse the boot, which this path exists to never do. What follows walks the
	// node's own configuration types by reflection and decodes through two libraries, so a panic is a
	// shape nobody predicted rather than a value an operator wrote. The nested recover keeps a panic from
	// the logging itself inside, the way the advisory reporter does.
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
		// Load refuses a file with no usable mode, so this answers for a File that did not come from it.
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
	// Before the reports it explains. A refused registration makes the next report name the wrong file, so
	// an operator reading in order meets the cause first.
	reportWhatThisBinaryCouldNotUse(resolved, log)

	// After the level, so a file that raises it can report its own mistakes. A key nothing declares is
	// the most common thing an operator gets wrong and the only signal they have for it.
	reportWhatTheFileDidNotReach(resolved, log)

	// Before anything is delivered. A key that arrives here is the answer for the kind sei.toml names, and
	// on a disagreement that is not the kind this node is.
	if !theFileNamesTheKindThisNodeRuns(ctx, mode, log) {
		return
	}

	// Every subcommand installs, and only one of them runs a node, so the line describing an ordinary
	// boot drops to debug everywhere else. On `seid keys list` nobody asked, and a line held above the
	// operator's own level buries the reports beside it that are actionable.
	//
	// What a refused registration says, and what a key nothing declares says, report everywhere. Both are
	// things to fix.
	said := log.Info
	if !runsANode(cmd) {
		said = log.Debug
	}

	// One read, and both halves are used: the values a decode has to deliver, and every key those
	// sections own so the install below leaves them out. Two reads would let a section arrive between
	// them, absent from the delivery and present in what the install drops.
	forADecode, ownedByADecode := registry.ResolvedAndOwnedByDecodedSections(resolved)

	// Every declared key a lookup reads, whether sei.toml mentioned it or not. There is no case where this
	// is empty for a reason an operator caused: the paths above already returned for a file that could not
	// be read or used, so reaching here means the resolution answered.
	forALookup := everyKeyALookupReads(resolved, ownedByADecode)

	// Values a reader turns into something else rather than refusing. Nothing downstream objects: the
	// source hands the value out as it was written, and a reader asking for a number gets a zero from a
	// word, so a setting an operator meant to change ends up off with nothing naming it. Dropped one key
	// at a time, because a lookup delivers each key on its own and one wrong value need not cost the rest.
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

	// Before the decode below. This refuses its whole set on one bad key. A decode refuses one section at
	// a time. In the other order, the refusal arrives after sections are published. The node then reads
	// sei.toml for some settings and its own files for the rest, under a line saying nothing was applied.
	report, err := appopts.Install(ctx.Viper, forALookup)
	if err != nil {
		log.Warn("cannot install this node's configuration", appliedNone, "err", err)
		return
	}

	// The second delivery. Their file is read into a struct before this runs and nothing consults the
	// source for them afterwards, so the values are decoded into that struct instead.
	deliverDecodedSections(ctx, forADecode, log)

	// After both deliveries, because this changes the level the process runs at and every path above
	// reports that nothing was applied. Run first, those reports would be false for this one setting.
	// They stay visible either way: this package holds its own logger at a floor, and each of them is a
	// warning or an error.
	applyTheLevelTheStructNowHolds(ctx, log)
	// Counted, not named. Both lists arrive sorted and the rendered one is capped, so naming them prints
	// the same first ten names on every boot and never a value. read_here_first_count is the set the
	// source did not already hold, which is the part most likely to change what the node runs.
	said("configuration installed", "mode", mode,
		"count", len(report.Installed),
		"read_here_first_count", len(report.Added))
}

// runsANode reports whether this command is the one that goes on to run a node. Every subcommand installs;
// this decides only which of them reports it at a level an operator sees.
func runsANode(cmd *cobra.Command) bool {
	// By name, because nothing else on the command distinguishes them. A command added later reports at
	// the quieter level until it is named here.
	return cmd != nil && cmd.Name() == "start"
}

// everyChannelAnOperatorCanUse names the sources a resolution for this node reads.
//
// Omitting one installs a lower layer over the top of what an operator chose, which is a value silently
// ignored rather than a value overridden. The flag channel matters most: an installed value sits above a
// bound flag, so a declared key a flag also delivers would resolve without ever seeing the command line
// and then bury it.
func everyChannelAnOperatorCanUse(written map[string]any, typed map[string]string) registry.Sources {
	return registry.Sources{
		File:      written,
		LookupEnv: os.LookupEnv,
		Flags:     flagValues(typed),
	}
}

// everyKeyALookupReads narrows a resolution to the declared keys whose readers look a key up rather than
// decoding one. It keeps every one of them, including the keys that took a declared value rather than a
// written one.
func everyKeyALookupReads(resolved registry.Resolved, ownedByADecode []string) registry.Resolved {
	owning := make(map[string]bool, len(ownedByADecode))
	for _, key := range ownedByADecode {
		owning[key] = true
	}

	out := registry.Resolved{Values: make(map[string]any, len(resolved.Values))}
	for key, value := range resolved.Values {
		// A key both deliveries carried would be installed as well as decoded. The install refuses a key
		// its own contract does not cover, and that refusal covers every key of every section.
		if owning[key] {
			continue
		}
		out.Values[key] = value
	}
	return out
}

// reportWhatThisBinaryCouldNotUse names a registration this binary's own source got wrong.
//
// Not the operator's mistake and nothing they can fix. It reaches them anyway: a refused registration
// leaves its keys out of the declared set, so a valid value they wrote for one of those keys is reported as
// a key nothing declares, which points at their file for a defect in this binary.
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

// reportWhatTheFileDidNotReach says what an operator asked for that had no effect: a key this file writes
// that no section declares, and a variable set for a key the environment cannot carry.
//
// Only the file's own keys are named. The same resolution reports flag names matching no declared key, and
// those are not a mistake: a command carries flags that name no setting at all, so reporting them would put
// this warning on every boot and hide the typo it exists to report.
func reportWhatTheFileDidNotReach(resolved registry.Resolved, log *slog.Logger) {
	if len(resolved.UnknownInFile) > 0 {
		shown, omitted := capLoggedItems(resolved.UnknownInFile)
		log.Warn("sei.toml writes keys no section declares; they have no effect",
			"count", len(resolved.UnknownInFile), "keys", strings.Join(shown, ","), "omitted", omitted)
	}
	// Sorted, so a log line does not vary between runs for a configuration that did not change.
	for _, key := range sortedKeys(resolved.Ignored) {
		log.Warn("an environment variable is set for a key the environment cannot supply; it has no "+
			"effect and whatever was written elsewhere applies", "key", key,
			"variable", registry.EnvName(key), "why", resolved.Ignored[key])
	}
}

// sortedKeys returns a map's keys in a fixed order, so a report does not vary between runs for a
// configuration that did not change.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// theFileNamesTheKindThisNodeRuns reports whether sei.toml and the node's own file agree on what kind of
// node this is, and names the disagreement when they do not.
//
// Nothing is delivered when they disagree. Every resolved value is the answer for one kind of node, so a
// disagreement is not one setting that did not arrive: it is the whole configuration answering for a node
// this is not. A validator's declared values put the query and peer listeners on loopback and turn the
// query interfaces off, so a node that serves queries would go on running while serving none of them.
//
// An empty running kind counts as a disagreement. The node's own file can state the key with nothing after
// it, and a node running with no kind at all is not the kind sei.toml names either.
//
// Delivering nothing leaves the node reading its own files, exactly as it does with this manager switched
// off, which is the outcome an operator can still act on. The kind itself is not corrected here, because
// what kind of node this is gets decided when it is provisioned.
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

// modesDisagree reports whether the kind of node sei.toml records and the kind the node runs as are
// different kinds.
//
// One pairing is not a disagreement. The kind that keeps every version of history has no name in the node's
// own configuration file. The command that writes that file writes the query-serving name instead, and what
// separates the two lives in settings that file does not carry.
//
// An empty running mode is a real value and disagrees with every recorded kind. A node's own file can state
// the key with nothing after it, a boot unmarshals that over its default, and the node then runs with no
// kind at all. Answering with a default for that case would agree with whatever sei.toml records and hide
// it.
func modesDisagree(recorded, running string) bool {
	if recorded == running {
		return false
	}
	return recorded != string(registry.ModeArchive) || running != string(registry.ModeFull)
}

// OwnReportingEnabledForTest reports whether this package's logger would emit at the level its reports use.
//
// Exported for the test that holds the floor, because the thing under test is a level and not a message.
func OwnReportingEnabledForTest() bool {
	return logger.Enabled(context.Background(), ownReportingFloor)
}

// readSeiToml loads the node's sei.toml, reporting the ordinary absence quietly.
//
// A node with no sei.toml reads every key the way it always has, which is a state this manager supports
// rather than a mistake, so its absence is not a warning. A file that exists and will not parse is one
// somebody wrote that is not doing what they think, so that is.
func readSeiToml(cmd *cobra.Command, log *slog.Logger) (*seitoml.File, bool) {
	home, err := resolveHomeDir(cmd)
	if err != nil {
		log.Warn("cannot resolve the home directory", appliedNone, "err", err)
		return nil, false
	}
	// An empty home leaves the path relative, so the read lands in ./config under whatever directory the
	// process started in. Any sei.toml there belongs to another node, and installing it would configure
	// this one from it.
	if home == "" {
		log.Warn("no home directory is set, so there is nowhere to read sei.toml from", appliedNone)
		return nil, false
	}
	file, err := readSeiTomlAt(home)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Not a mistake. A node without this file resolves nothing and reads as it always has.
		log.Debug("no sei.toml", appliedNone, "home", home)
		return nil, false
	case err != nil:
		// Somebody wrote this file and it is not doing what they think. Reported at a level an operator
		// sees, because the alternative is a file that is silently ignored for the reason it was written.
		log.Warn("this node's sei.toml cannot be read", appliedNone,
			"home", home, "err", err)
		return nil, false
	}
	return file, true
}

// readSeiTomlAt loads the sei.toml under a home directory.
//
// Separate from the reporting, so the check command can ask the same question without a logger and get the
// same answer for the same file.
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
	// Matched by environment spelling, where a dot, a hyphen and an underscore are the same character.
	// Flag names use underscores and declared keys use hyphens, so a direct comparison finds no match and
	// the flag is dropped. The registry refuses to let two declared keys share this spelling, so a flag
	// matches at most one key.
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
