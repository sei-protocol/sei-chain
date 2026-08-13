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

	// Every section this binary declares, so the values installed into a booting node cover the whole
	// key space. Without this the set is whatever the import graph produced, and a section left out
	// resolves through the machinery that answered it before with nothing reporting the difference.
	_ "github.com/sei-protocol/sei-chain/config/sections"
)

// installResolved puts the values sei.toml resolves into the source the boot just built.
//
// Nothing here can stop a node starting. A node with no sei.toml, an unreadable one, or one recording
// a mode this binary does not know installs nothing and reads exactly as it always has, so selecting
// this manager changes nothing until an operator generates a file. Refusing instead would turn a
// mistyped file into an outage on the next restart, and the file is hand-editable by design.
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
	mode, ok := usableMode(file, log)
	if !ok {
		return
	}
	warnOnModeConflict(ctx, mode, log)

	written, err := file.Values()
	if err != nil {
		log.Warn("cannot read the values sei.toml writes; every key reads as it always has", "err", err)
		return
	}
	// Every channel an operator can use, in the order Precedence declares: the file beats the baseline,
	// the environment beats the file, and a flag they typed beats both. Omitting any of them installs a
	// lower layer over the top of what the operator chose, which is a value silently ignored rather
	// than a value overridden. The flag layer matters most, because installing writes into the source's
	// override layer, above a bound flag: without it, a declared key that a flag also delivers would
	// resolve without ever seeing the command line and then bury it.
	//
	// The environment and flag layers are both driven by the declared set. An environment cannot be
	// enumerated for a prefix, and enumerating a command's flags would report every unrelated one as a
	// key nothing declares. That is only possible for a declared key, which is the same reason an
	// undeclared key is left to the source that already answers it.
	resolved, err := registry.Resolve(registry.Mode(mode),
		registry.FileLayer(written),
		registry.EnvLayer(os.LookupEnv),
		registry.FlagLayer(lookIn(typed)))
	if err != nil {
		log.Warn("cannot resolve this node's configuration; every key reads as it always has",
			"mode", mode, "err", err)
		return
	}
	for _, key := range resolved.Unknown {
		log.Warn("sei.toml writes a key no section declares; it has no effect", "key", key)
	}
	report, err := appopts.Install(ctx.Viper, resolved)
	if err != nil {
		log.Warn("cannot install resolved values; every key reads as it always has", "err", err)
		return
	}
	log.Info("resolved configuration installed", "mode", mode, "summary", report.Summary())
}

// readSeiToml loads the node's sei.toml, reporting the ordinary absence quietly.
//
// A node that has not generated one is the expected state during the migration, so that is not a
// warning. A file that exists and will not parse is, because somebody wrote it and it is not doing
// what they think.
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

// usableMode reads the node mode the file records.
//
// Every value a node reads through the registry is the resolution for one mode, so a file that does not
// say which cannot be used at all. Reported rather than guessed: guessing picks one mode's defaults for
// a node configured as another.
func usableMode(file *seitoml.File, log *slog.Logger) (string, bool) {
	mode, err := file.Mode()
	if err != nil {
		log.Warn("sei.toml records no usable node mode; every key reads as it always has", "err", err)
		return "", false
	}
	for _, known := range registry.Modes() {
		if string(known) == mode {
			return mode, true
		}
	}
	log.Warn("sei.toml records a node mode this binary does not know; every key reads as it always has",
		"mode", mode, "known", registry.Modes())
	return "", false
}

// warnOnModeConflict says so when the node's two configuration files disagree about what it is.
//
// A warning rather than a refusal. The combination is wrong, and an operator needs to know, but a node
// that has been running should not stop starting because of a hand edit. The doctor verb is where this
// halts, so a deploy can gate on it before a restart does.
func warnOnModeConflict(ctx *server.Context, mode string, log *slog.Logger) {
	if ctx.Config == nil || ctx.Config.Mode == "" {
		return
	}
	if err := appopts.ReconcileMode(mode, ctx.Config.Mode); err != nil {
		log.Warn("this node's configuration files disagree about what kind of node it is", "err", err)
	}
}

// TypedFlags records which flags this invocation actually carried, and has to run before anything else
// touches them.
//
// A flag reports Changed when something called Set on it, and the legacy handler calls Set on every flag
// whose name its configuration knows a value for, so that a file can supply a flag's default. After that
// has run, Changed no longer distinguishes a flag the operator typed from a key their app.toml holds. A
// flag layer built from it would put app.toml above sei.toml, which is a worse inversion than the one the
// layer exists to prevent.
//
// So the snapshot is taken at the one point before that happens, which is the entry to Apply. Taking it
// there rather than inside the install is the difference between an invariant and a convention: there is
// no later point at which the truth is still available.
func TypedFlags(cmd *cobra.Command) map[string]string {
	out := map[string]string{}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			out[strings.ToLower(f.Name)] = f.Value.String()
		}
	})
	return out
}

// lookIn answers from a snapshot of the flags the operator typed.
func lookIn(typed map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := typed[key]
		return v, ok
	}
}
