package configmanager

import (
	"log/slog"
	"os"
	"path/filepath"
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
	reportWhatTheFileDidNotReach(resolved, log)

	supplied := onlyWhatASourceSupplied(resolved)
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

// onlyWhatASourceSupplied narrows a resolution to the keys something other than the defaults answered.
//
// This is the whole difference between moving a setting and replacing a file. A resolution answers for
// every declared key, so installing all of it would write a default over whatever an operator's app.toml
// holds for every key their sei.toml does not mention: a hundred and fifty settings replaced because they
// moved one. Installing only what a source supplied means a key reaches the node exactly when somebody
// asked for it, and every other key reads as it always has.
//
// It also means a declared default never reaches a running node, which is what lets a default state what
// the provisioning command writes rather than having to state what each node already runs.
func onlyWhatASourceSupplied(resolved registry.Resolved) registry.Resolved {
	out := registry.Resolved{Values: make(map[string]any, len(resolved.Overrides))}
	for _, key := range resolved.Overrides {
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
	path := filepath.Join(home, "config", seiTomlName)

	file, err := seitoml.Load(path)
	if err != nil {
		log.Debug("no readable sei.toml; every key reads as it always has", "path", path, "err", err)
		return nil, false
	}
	return file, true
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

// flagValues renders a snapshot of typed flags as a configuration source.
func flagValues(typed map[string]string) map[string]any {
	if len(typed) == 0 {
		return nil
	}
	out := make(map[string]any, len(typed))
	for name, value := range typed {
		out[name] = value
	}
	return out
}
