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
	// owning package's initialisation, so a section nothing imports is absent from what this installs, and
	// absent silently, since an undeclared key is left to whatever answered it before.
	_ "github.com/sei-protocol/sei-chain/config/cosmosbase"

	// The sections whose keys belong to the node's own configuration file. These are the ones read by a
	// decode, so they are deliberately left out of what this installs.
	_ "github.com/sei-protocol/sei-chain/config/tendermintbase"
)

// seiTomlName is the file this manager reads.
const seiTomlName = "sei.toml"

// appliedNone is the attribute every outcome that installs nothing carries. One field rather than a
// sentence on each message, so a fleet can match on it instead of on message text.
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
	// Before the report below, because a refused registration is what makes that one point at the wrong
	// file, and an operator reading in order should meet the cause first.
	reportWhatThisBinaryCouldNotUse(resolved, log)

	// A key nothing declares is the most common thing an operator gets wrong and the only signal they
	// have for it.
	reportWhatTheFileDidNotReach(resolved, log)

	reportWhatTheFileSaysTheNodeIs(ctx, mode, log)

	// Every subcommand installs, and only one of them runs a node, so the lines describing an ordinary
	// boot drop to debug everywhere else. On `seid keys list` nobody asked, and a line held above the
	// operator's own level buries the reports beside it that are actionable.
	//
	// Holding keys back joins them. It is a problem, but not one an operator can act on, and its trigger
	// is any sei.toml carrying a [p2p], [mempool] or root key, which is nearly every file somebody would
	// write. It keeps its own level on the boot, because that is the one place holding them back changes
	// what the node runs.
	//
	// What a refused registration says, and what a key nothing declares says, report everywhere. Both are
	// things to fix.
	said, warned := log.Info, log.Warn
	if !runsANode(cmd) {
		said, warned = log.Debug, log.Debug
	}

	// One read for both halves. A section arriving between two reads would be absent from what is
	// reported and present in what is dropped, which is undelivered and unreported at once.
	forADecode, ownedByADecode := registry.SuppliedAndOwnedByDecodedSections(resolved)

	// Only what the file itself wrote. The supplied set is filled by every channel, and a flag or a
	// variable answering one of these keys does reach the node, so reporting it as read-as-it-always-has
	// would be false as well as pointed at the wrong file.
	heldFromTheFile := whatTheFileWroteForADecode(forADecode, written)
	reportWhatThisInstallHoldsBack(heldFromTheFile, warned)

	// Every declared key a lookup reads, whether sei.toml mentioned it or not. There is no case where this
	// is empty for a reason an operator caused: the paths above already returned for a file that could not
	// be read or used, so reaching here means the resolution answered.
	report, err := appopts.Install(ctx.Viper, everyKeyALookupReads(resolved, ownedByADecode))
	if err != nil {
		log.Warn("cannot install this node's configuration", appliedNone, "err", err)
		return
	}
	// Counted rather than named. Both lists arrive sorted and a rendered one is capped, so naming them
	// prints the same alphabetically-first handful on every node on every boot and never a value. The
	// counts do carry: read_here_first_count is the set the source did not already have, which is the
	// part most likely to change what the node runs.
	said("configuration installed", "mode", mode,
		"count", len(report.Installed),
		"read_here_first_count", len(report.Added))
}

// runsANode reports whether this command is the one that goes on to run a node. Every subcommand installs;
// this decides only which of them reports it at a level an operator sees.
func runsANode(cmd *cobra.Command) bool {
	// By name, because nothing else on the command distinguishes them. One added later reports at the
	// quieter level until it is named here, which is the safe direction to be wrong in.
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
		// A key both deliveries carried would be installed into the source as well as decoded, and the
		// install refuses a key its own contract does not cover, which would take the whole install down
		// and with it every key of every other section.
		if owning[key] {
			continue
		}
		out.Values[key] = value
	}
	return out
}

// whatTheFileWroteForADecode narrows the decoded sections' supplied values to the keys the file itself
// wrote, sorted. A key a flag or the environment answered reaches the node through that channel, so naming
// the file for it would report a value as held back when it arrived.
func whatTheFileWroteForADecode(bySection map[string]map[string]any, written map[string]any) []string {
	inTheFile := make(map[string]bool, len(written))
	for key := range written {
		inTheFile[strings.ToLower(key)] = true
	}
	var keys []string
	for _, values := range bySection {
		for key := range values {
			if inTheFile[strings.ToLower(key)] {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// reportWhatThisInstallHoldsBack names the supplied keys this install cannot deliver, because their reader
// decoded its file before this ran.
//
// Unreported they are invisible: absent from what was installed, and declared, so absent from the
// undeclared keys too. No source is named, because the resolution does not record which one answered, and
// naming the file for a value a variable supplied is a misattribution.
func reportWhatThisInstallHoldsBack(keys []string, say func(string, ...any)) {
	if len(keys) == 0 {
		return
	}
	shown, omitted := capLoggedItems(keys)
	say("sei.toml writes keys whose reader decodes its file whole; this install cannot deliver them "+
		"and they read as they always have",
		"count", len(keys), "keys", strings.Join(shown, ","), "omitted", omitted)
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
// this warning on every boot and bury the typo it exists to surface.
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

// reportWhatTheFileSaysTheNodeIs names a disagreement about what kind of node this is. sei.toml records it
// at the top and every resolved value is the answer for that kind; the node's own file states it again in a
// key of its own, and that one is what the node runs as.
//
// Reported rather than corrected, because what kind of node this is gets decided when it is provisioned.
// Nothing here can change it either way: this manager deliberately does not declare the node's own mode key,
// since two keys for one fact can be written to disagree, which is the state this reports.
func reportWhatTheFileSaysTheNodeIs(ctx *server.Context, mode string, log *slog.Logger) {
	if ctx == nil || ctx.Config == nil || ctx.Config.Mode == "" {
		return
	}
	running := ctx.Config.Mode
	if !modesDisagree(mode, running) {
		return
	}
	log.Error("sei.toml says this is one kind of node and the node's own configuration file says another; "+
		"every value resolved here is the answer for the first and the node runs as the second",
		"sei.toml", mode, "running", running)
}

// modesDisagree reports whether the kind of node sei.toml records and the kind the node runs as are
// different kinds.
//
// One pairing is not a disagreement. The kind that keeps every version of history has no name of its own in
// the node's own configuration file, so the command that writes that file writes the query-serving name
// instead, and the difference between them lives in settings the node's own file does not carry.
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
	// An empty home resolves the read to ./config, relative to whatever directory the process started in,
	// so it would install some unrelated node's file into this one. Declining is the only safe answer:
	// there is no directory to read, and reading the wrong one is worse than reading none.
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

// TypedFlags records which flags this invocation carried. It answers for the state of the flags at the
// moment it runs, so a caller has to run it before anything that calls Set on one.
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
	// Through the environment spelling, where a dot, a hyphen and an underscore are the same character.
	// A node's flags separate words with an underscore where the tag they decode through uses a hyphen, so
	// comparing the two directly leaves an operator's typed flag looking like a name nothing declares, and
	// the file then wins over the command line. The registry refuses to let two declared keys share this
	// spelling, so a flag matches at most one key.
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
