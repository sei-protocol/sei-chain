package configmanager

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/sei-protocol/sei-chain/config/appopts"
	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"

	// The sections whose keys belong to the upstream server, which nothing else imports. A section
	// reaches the registry through its owning package's initialisation, so a section nothing imports is
	// absent from what this installs and absent silently, since an undeclared key is left to whatever
	// answered it before.
	_ "github.com/sei-protocol/sei-chain/config/cosmosbase"

	// The sections whose keys belong to the node's own configuration file, which nothing else imports
	// either. These are the sections the delivery beside this one decodes rather than installs.
	_ "github.com/sei-protocol/sei-chain/config/tendermintbase"
)

// seiTomlName is the file this manager reads.
const seiTomlName = "sei.toml"

// installResolved puts the values sei.toml supplies into the source the boot has just built.
//
// Nothing here can stop a node starting. A node with no sei.toml, an unreadable one, or one recording a
// mode this binary does not know installs nothing and reads exactly as it always has, so selecting this
// manager is a switch rather than a configuration change. Refusing instead would turn a mistyped line in
// a hand-editable file into an outage on the next restart.
func installResolved(cmd *cobra.Command, typed map[string]string, log *slog.Logger) {
	// The claim above is only true with this. What follows walks the node's own configuration types by
	// reflection and decodes through two libraries, so a panic here is a shape nobody predicted rather
	// than a value an operator wrote, and letting it escape would refuse the boot for the one reason this
	// path promises never to refuse it. The nested recover keeps a panic from the logging itself inside,
	// the way the advisory reporter does.
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

	// Every channel an operator can use. Omitting one installs a lower layer over the top of what they
	// chose, which is a value silently ignored rather than a value overridden. The flag channel matters
	// most: an installed value sits above a bound flag, so a declared key a flag also delivers would
	// resolve without ever seeing the command line and then bury it.
	resolved, err := registry.Resolve(registry.Mode(mode), registry.Sources{
		File:      written,
		LookupEnv: os.LookupEnv,
		Flags:     flagValues(typed),
	})
	if err != nil {
		log.Warn("cannot resolve this node's configuration; every key reads as it always has",
			"mode", mode, "err", err)
		return
	}
	// A key nothing declares is the most common thing an operator gets wrong and the only signal they
	// have for it.
	reportWhatTheFileDidNotReach(resolved, log)

	reportWhatTheFileSaysTheNodeIs(ctx, mode, log)

	supplied := onlyWhatALookupSourceSupplied(resolved)
	if len(supplied.Values) == 0 {
		log.Info("sei.toml supplies no declared value; every key reads as it always has", "mode", mode)
		return
	}
	report, err := appopts.Install(ctx.Viper, supplied)
	if err != nil {
		log.Warn("cannot install the values sei.toml supplies; every key reads as it always has",
			"err", err)
		return
	}
	log.Info("configuration installed", "mode", mode,
		"installed", strings.Join(report.Installed, ","))
}

// onlyWhatALookupSourceSupplied narrows a resolution to the keys something other than the defaults
// answered, for the sections whose readers look a key up rather than decoding one.
//
// This is the whole difference between moving a setting and replacing a file. A resolution answers for
// every declared key, so installing all of it would write a default over whatever an operator's app.toml
// holds for every key their sei.toml does not mention: a hundred and fifty settings replaced because they
// moved one. Installing only what a source supplied means a key reaches the node exactly when somebody
// asked for it, and every other key reads as it always has.
//
// It also means a declared default never reaches a running node, which is what lets a default state what
// the provisioning command writes rather than having to state what each node already runs.
func onlyWhatALookupSourceSupplied(resolved registry.Resolved) registry.Resolved {
	decoded := registry.DecodedSections()
	owning := map[string]bool{}
	for _, section := range registry.Sections() {
		if _, ok := decoded[section.Name]; !ok {
			continue
		}
		for _, key := range section.Keys {
			owning[key] = true
		}
	}

	out := registry.Resolved{Values: make(map[string]any, len(resolved.Overrides))}
	for _, key := range resolved.Overrides {
		// A key both deliveries carried would be installed into the source as well as decoded, and the
		// install refuses a key its own contract does not cover, which would take the whole install down
		// and with it every key of every other section.
		if owning[key] {
			continue
		}
		out.Values[key] = resolved.Values[key]
	}
	return out
}

// reportWhatTheFileDidNotReach says what an operator asked for that had no effect.
//
// Two things, and neither is visible anywhere else. A key no section declares is one this file cannot
// deliver, so it reads as a setting and changes nothing. And a variable set for a key no environment
// variable can carry is ignored on purpose, with the reason recorded where the key is declared.
//
// Reported once each rather than per key, because a node resolves over a hundred declared keys and a line
// each would bury the two or three that matter in the noise it creates.
func reportWhatTheFileDidNotReach(resolved registry.Resolved, log *slog.Logger) {
	if len(resolved.Unknown) > 0 {
		log.Warn("sei.toml writes keys no section declares; they have no effect",
			"count", len(resolved.Unknown), "keys", strings.Join(resolved.Unknown, ","))
	}
	if len(resolved.Ignored) == 0 {
		return
	}
	cannot := registry.EnvCannotDeliver()
	for _, key := range resolved.Ignored {
		log.Warn("an environment variable is set for a key the environment cannot supply; it has no "+
			"effect and the file's value applies", "key", key, "variable", registry.EnvName(key),
			"why", cannot[key])
	}
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
// A node that has not generated one is the expected state while sections are still moving, so that is not
// a warning. A file that exists and will not parse is, because somebody wrote it and it is not doing what
// they think.
func readSeiToml(cmd *cobra.Command, log *slog.Logger) (*seitoml.File, bool) {
	home, err := resolveHomeDir(cmd)
	if err != nil {
		log.Warn("cannot resolve the home directory; every key reads as it always has", "err", err)
		return nil, false
	}
	file, err := readSeiTomlAt(home)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// The common case, and not a mistake: a node with no sei.toml is every node today.
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
