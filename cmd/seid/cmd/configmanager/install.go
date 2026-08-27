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

// installResolved puts the values sei.toml supplies into the source the boot has just built.
//
// Nothing here refuses a boot. A node with no sei.toml, an unreadable one, one recording a mode this
// binary does not know, or a value the install cannot use installs nothing and reads exactly as it always
// has, so selecting this manager is a switch rather than a configuration change. Refusing instead would
// turn a mistyped line in a hand-editable file into an outage on the next restart.
//
// The recover below is not what carries that on its own, because a cost is not a panic: a file whose
// reading outgrows the memory the process has is killed rather than recovered, and no recover runs. The
// file is read within a bound stated where it is read, and that bound is the other half of this promise.
func installResolved(cmd *cobra.Command, typed map[string]string, log *slog.Logger) {
	// A panic here would refuse the boot, which this path exists to never do. What follows walks the
	// node's own configuration types by reflection and decodes through two libraries, so a panic is a
	// shape nobody predicted rather than a value an operator wrote. The nested recover keeps a panic from
	// the logging itself inside, the way the advisory reporter does.
	defer func() {
		if r := recover(); r != nil {
			defer func() { _ = recover() }()
			log.Error("installing this node's configuration panicked; every key reads as it always has",
				"panic", r, "stack", string(debug.Stack()))
		}
	}()

	ctx := server.GetServerContextFromCmd(cmd)
	if ctx == nil || ctx.Viper == nil {
		log.Warn("no configuration source to install into; every key reads as it always has")
		return
	}

	file, ok := readSeiToml(cmd, log)
	if !ok {
		return
	}
	mode, ok := recordedMode(file, log)
	if !ok {
		return
	}
	written, err := file.Values()
	if err != nil {
		log.Warn("cannot read the values sei.toml writes; every key reads as it always has", "err", err)
		return
	}

	resolved, err := registry.Resolve(registry.Mode(mode), everyChannelAnOperatorCanUse(written, typed))
	if err != nil {
		log.Warn("cannot resolve this node's configuration; every key reads as it always has",
			"mode", mode, "err", err)
		return
	}
	// First, because every report below is a log line and a refusal is reported at a level an operator
	// may have raised the threshold above. Doing this after would mean the one setting somebody changes
	// in order to see a refusal is the setting a refusal suppresses.
	applyResolvedLogLevel(resolved, typed, log)

	// Before the reports it explains, because a refused registration is what makes the next one point at
	// the wrong file, and an operator reading in order should meet the cause first.
	reportWhatThisBinaryCouldNotUse(resolved, log)

	// After the level, so a file that raises it can report its own mistakes. A key nothing declares is
	// the most common thing an operator gets wrong and the only signal they have for it.
	reportWhatTheFileDidNotReach(resolved, log)

	reportWhatTheFileSaysTheNodeIs(ctx, mode, log)

	// Every subcommand installs, and only one of them runs a node, so the lines describing an ordinary
	// boot drop to debug everywhere else. On `seid keys list` nobody asked, and a line held above the
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

	// The second delivery. Their file is read into a struct before this runs and nothing consults the
	// source for them afterwards, so the values are decoded into that struct instead.
	deliverDecodedSections(ctx, forADecode, log)

	// Every declared key a lookup reads, whether sei.toml mentioned it or not. There is no case where this
	// is empty for a reason an operator caused: the paths above already returned for a file that could not
	// be read or used, so reaching here means the resolution answered.
	report, err := appopts.Install(ctx.Viper, everyKeyALookupReads(resolved, ownedByADecode))
	if err != nil {
		log.Warn("cannot install this node's configuration; every key reads as it always has", "err", err)
		return
	}
	// Added names the keys the source did not already carry, so a node reads them from the registry for
	// the first time. That is the set most likely to change what it runs, and it was computed and thrown
	// away.
	installed, omittedInstalled := capLoggedItems(report.Installed)
	added, omittedAdded := capLoggedItems(report.Added)
	said("configuration installed", "mode", mode,
		"count", len(report.Installed), "installed", strings.Join(installed, ","),
		"omitted", omittedInstalled,
		"read_here_first_count", len(report.Added), "read_here_first", strings.Join(added, ","),
		"read_here_first_omitted", omittedAdded)
}

// runsANode reports whether this command is the one that goes on to run a node.
//
// Every subcommand passes through the same pre-run, so every one of them installs, and that is deliberate:
// a command answering a question about configuration has to read the values the node would run. What
// differs is whether an operator wants telling about it.
//
// Matched by name because nothing else on the command distinguishes them. A command added later that runs a
// node reports at the quieter level until it is named here, which is the safe direction to be wrong in.
func runsANode(cmd *cobra.Command) bool {
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
// decoding one.
//
// Every one of them, not only the ones a source wrote. This manager is where a node's configuration comes
// from, so a key sei.toml does not mention takes the value this binary declares for the kind of node it is,
// and app.toml is not consulted for a declared key at all. One file answers, and what it leaves out is
// answered by a declaration rather than by whatever happened to be on disk.
//
// The cost is worth stating plainly, because it is large: on a node whose app.toml was hand-tuned, every
// declared key absent from sei.toml moves to its declared value. A path that renders sei.toml from a node's
// existing files is what makes that safe, and it has to exist before anybody turns this on.
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

// reportWhatTheFileDidNotReach says what an operator asked for that had no effect.
//
// Two things, and neither is visible anywhere else. A key this file writes that no section declares reads
// as a setting and changes nothing. And a variable set for a key the environment cannot carry is skipped on
// purpose, so whatever the operator wrote elsewhere is what applies. Which file that was, if any, is not
// something the resolution records: an operator who reached for the variable because they had written the
// key nowhere else gets no value at all for it, and naming a file would be wrong twice.
//
// Only the file's own keys are named. The same resolution reports flag names that match no declared key,
// and those are not a mistake: a command carries flags that name no setting at all, so reporting them would
// put this warning on every boot and bury the typo it exists to surface.
//
// The key list is capped, because a file that is broken in one way is usually broken in many, and the count
// is what an operator alerts on.
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

// reportWhatTheFileSaysTheNodeIs names a disagreement about what kind of node this is.
//
// Two files state that, under different names. sei.toml records it at the top, and every value resolved
// through this manager is the answer for that kind of node. The node's own configuration file states it
// again in a key of its own, and that one is what the node runs as.
//
// This manager does not declare the second, on purpose: two keys for one fact can be written to disagree,
// and then a resolution answers for one while the node is the other. Not declaring it means nothing here
// can change it, which leaves the disagreement possible and unreported. A node whose file says validator
// while it runs as a full node resolves a validator's values and serves queries, and every report about it
// reads correctly.
//
// So it is compared and reported. Reported rather than corrected, because what kind of node this is gets
// decided when it is provisioned, and a configuration manager is not the thing that should change it.
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
		log.Warn("cannot resolve the home directory; every key reads as it always has", "err", err)
		return nil, false
	}
	file, err := readSeiTomlAt(home)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Not a mistake. A node without this file resolves nothing and reads as it always has.
		log.Debug("no sei.toml; every key reads as it always has", "home", home)
		return nil, false
	case err != nil:
		// Somebody wrote this file and it is not doing what they think. Reported at a level an operator
		// sees, because the alternative is a file that is silently ignored for the reason it was written.
		log.Warn("this node's sei.toml cannot be read; every key reads as it always has",
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

// recordedMode reads the node mode the file records.
//
// Every value a node reads through the registry is the resolution for one mode, so a file that does not
// say which cannot be used at all. Reported rather than guessed: guessing picks one mode's answers for a
// node configured as another.
//
// Whether the mode is one this binary knows is not checked here. The resolution refuses a mode no section
// declares defaults for, and it names the modes there are, so a check here would be the same guard a
// second time and a worse message.
func recordedMode(file *seitoml.File, log *slog.Logger) (string, bool) {
	mode, err := file.Mode()
	if err != nil {
		log.Warn("sei.toml records no usable node mode; every key reads as it always has", "err", err)
		return "", false
	}
	return mode, true
}

// TypedFlags records which flags this invocation carried, and has to run before anything else touches
// them.
//
// A flag reports itself changed when something called Set on it, and the handler this manager re-enters
// calls Set on every flag whose name its configuration knows a value for, so that a file can supply a
// flag's default. After that has run, a flag an operator typed and a key their app.toml holds are
// indistinguishable, and a flag channel built from that state would put app.toml above sei.toml. That is
// a worse inversion than the one the channel exists to prevent: the file an operator is being migrated
// onto would lose to the file they are being migrated off.
//
// So the snapshot is taken at the one point before that happens, which is the entry to Apply. Taking it
// there rather than inside the install is the difference between an invariant and a convention, because
// there is no later point at which the truth is still available.
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
// declare.
//
// A flag's name and the key it carries are not always spelled the same. The node's own flags separate words
// with an underscore where the tag they decode through uses a hyphen, so a flag named for a declared key is
// not equal to it, and comparing the two by string leaves an operator's typed flag looking like a name
// nothing declares. It is then dropped, and the file wins over the command line: the one channel somebody
// reaches for during an incident is the one that loses.
//
// Matched through the environment spelling, where a dot and a hyphen and an underscore are all the same
// character. That is an equivalence the registry already refuses to let two declared keys share, so a flag
// matches at most one key and no ambiguity is possible here.
//
// A flag matching no declared key is left under its own name. Most of the flags a node starts with were
// never configuration keys, and the resolution reports the unmatched ones from the file alone.
func flagValues(typed map[string]string) map[string]any {
	if len(typed) == 0 {
		return nil
	}
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
