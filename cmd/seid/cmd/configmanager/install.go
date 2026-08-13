package configmanager

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

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
func installResolved(cmd *cobra.Command, log *slog.Logger) {
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
	// Both channels an operator can use, in the order Precedence declares: the file beats the baseline,
	// and the environment beats the file. Omitting either installs a lower layer over the top of what
	// the operator chose, which is a value silently ignored rather than a value overridden.
	//
	// The environment layer is driven by the declared set, since an environment cannot be enumerated for
	// a prefix. That is only possible for a declared key, which is the same reason an undeclared key is
	// left to the source that already answers it.
	resolved, err := registry.Resolve(registry.Mode(mode),
		registry.FileLayer(written), registry.EnvLayer(os.LookupEnv))
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
