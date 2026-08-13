package configcli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/version"
)

// FileName is what the configuration is called inside a node's config directory.
const FileName = "sei.toml"

// flagMode names the node mode a verb resolves baselines for.
const flagMode = "mode"

// flagFromLegacy asks generate to carry an existing node's configuration over rather than write
// defaults.
//
// A flag on generate rather than a verb of its own. Both produce the file a node starts from and
// only the source of the values differs, and the whole command set is reachable only where the v2
// manager runs, so a flag cannot surprise an operator who has not opted in. A separate verb would
// add a name to the tree and answer no question this does not.
const flagFromLegacy = "from-legacy"

// Path is where a node's sei.toml lives, given its home directory.
//
// Beside the files a node already keeps, so an operator finds it where they look for app.toml and
// config.toml rather than somewhere this tool invented.
func Path(home string) string {
	return filepath.Join(home, "config", FileName)
}

// Verbs are the sei.toml commands that live under the existing config command.
//
// A slice rather than their own parent, so they join the command an operator already uses for
// configuration instead of standing beside it. Cobra resolves a subcommand before it treats
// an argument as positional, so config generate reaches the verb here while config chain-id still
// falls through to the client configuration it has always meant.
//
// None of these declares --home. The root command carries it as a persistent flag, and a local one
// of the same name shadows it, leaving every verb to read its own default while ignoring the --home
// the operator passed. defaultHome serves only as the fallback for a tree carrying no such flag.
func Verbs(defaultHome string) []*cobra.Command {
	return []*cobra.Command{
		generateCmd(defaultHome),
		showCmd(defaultHome),
		diffCmd(defaultHome),
		doctorCmd(defaultHome),
		setCmd(defaultHome),
		unsetCmd(defaultHome),
		upgradeCmd(defaultHome),
	}
}

// VerbNames lists the names Verbs occupies under the config command.
//
// Exported so a test can hold them against the keys the client configuration answers to. A verb
// sharing a name with one of those keys takes over a command operators already use.
func VerbNames(defaultHome string) []string {
	verbs := Verbs(defaultHome)
	out := make([]string, 0, len(verbs))
	for _, v := range verbs {
		out = append(out, v.Name())
	}
	return out
}

// generateCmd writes a fresh file for a mode.
func generateCmd(defaultHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Write a complete sei.toml for a node mode",
		Long: "Write every setting this binary knows, at the default for the given mode.\n\n" +
			"Every key written this way is a value this binary will not rewrite, so the node keeps " +
			"them across an upgrade even where a later release ships a different default. Run this " +
			"again to move onto current defaults.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, mode, err := target(cmd, defaultHome)
			if err != nil {
				return err
			}
			if mode == "" {
				return fmt.Errorf("generate needs --mode: every value it writes is the default for one "+
					"node mode, and a file written for the wrong one is complete, plausible and wrong "+
					"throughout. One of %v", registry.Modes())
			}
			if err := refuseUnlessForced(cmd, path); err != nil {
				return err
			}
			fromLegacy, err := cmd.Flags().GetBool(flagFromLegacy)
			if err != nil {
				return err
			}
			if fromLegacy {
				home, err := homeDir(cmd, defaultHome)
				if err != nil {
					return err
				}
				return adoptInto(cmd, path, home, mode)
			}
			return writeDefaults(cmd, path, mode)
		},
	}
	cmd.Flags().String(flagMode, "", modeUsage())
	cmd.Flags().Bool("force", false, "Replace an existing file, discarding every value in it")
	cmd.Flags().Bool(flagFromLegacy, false,
		"Build the file from this node's existing app.toml and config.toml instead of from defaults")
	return cmd
}

// refuseUnlessForced stops generate from replacing a file somebody already has.
//
// Generating over a file discards every value in it, including the ones an operator set, and those
// exist nowhere else. --force is how they say to do it anyway.
func refuseUnlessForced(cmd *cobra.Command, path string) error {
	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return err
	}
	if force {
		return nil
	}
	if _, err := seitoml.Load(path); err != nil {
		return nil // no readable file there, so there is nothing to lose
	}
	return fmt.Errorf("%s already exists. Generating would replace every value in it, including any "+
		"you set. Pass --force to do that, or use diff to see what it holds first", path)
}

// writeDefaults builds the file from this binary's defaults and reports where it landed.
func writeDefaults(cmd *cobra.Command, path string, mode registry.Mode) error {
	file, err := Generate(mode)
	if err != nil {
		return err
	}
	if err := file.Save(path); err != nil {
		return err
	}
	cmd.Printf("Wrote %s for %q mode.\n", path, mode)
	return nil
}

// showCmd prints the file as it stands.
func showCmd(defaultHome string) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print this node's sei.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := path(cmd, defaultHome)
			if err != nil {
				return err
			}
			file, err := seitoml.Load(path)
			if err != nil {
				return err
			}
			raw, err := file.Bytes()
			if err != nil {
				return err
			}
			cmd.Print(string(raw))
			return nil
		},
	}
}

// diffCmd compares the file against this binary's defaults.
func diffCmd(defaultHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare sei.toml against this binary's defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, mode, err := target(cmd, defaultHome)
			if err != nil {
				return err
			}
			file, err := seitoml.Load(path)
			if err != nil {
				return err
			}
			comparison, err := Diff(file, mode)
			if err != nil {
				return err
			}
			cmd.Print(comparison.Report())
			return nil
		},
	}
	cmd.Flags().String(flagMode, "", "Compare against another mode's defaults instead of the one "+
		"the file records; one of "+fmt.Sprint(registry.Modes()))
	return cmd
}

// doctorCmd checks what the file writes against what this binary declares.
func doctorCmd(defaultHome string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check sei.toml for settings this binary does not recognize",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := path(cmd, defaultHome)
			if err != nil {
				return err
			}
			file, err := seitoml.Load(path)
			if err != nil {
				return err
			}
			home, err := homeDir(cmd, defaultHome)
			if err != nil {
				return err
			}
			// An unreadable config.toml leaves the comparison out rather than failing the check. The
			// node's own mode is still worth reporting, and a missing second file is not evidence of
			// anything about the first.
			tendermintMode, _ := tendermintMode(home)

			diagnosis, err := Doctor(file, tendermintMode)
			if err != nil {
				return err
			}
			cmd.Print(diagnosis.Report())
			if !diagnosis.Healthy() {
				// A non-zero exit is what lets an operator gate a deploy on this, and the report above
				// already names every key, so this adds no second copy of the list.
				if diagnosis.ModeProblem != "" {
					return fmt.Errorf("sei.toml does not record a usable node mode")
				}
				return fmt.Errorf("%d written setting(s) are not recognized by this binary",
					len(diagnosis.Unrecognized))
			}
			return nil
		},
	}
}

// setCmd writes one value.
func setCmd(defaultHome string) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write one setting",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := path(cmd, defaultHome)
			if err != nil {
				return err
			}
			change, err := Set(path, args[0], args[1])
			if err != nil {
				return err
			}
			if change.Had {
				cmd.Printf("%s: %v -> %v\n", change.Key, change.From, change.To)
				return nil
			}
			cmd.Printf("%s = %v\n", change.Key, change.To)
			return nil
		},
	}
}

// unsetCmd removes one value so it follows the default again.
func unsetCmd(defaultHome string) *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove one setting so it follows this binary's default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := path(cmd, defaultHome)
			if err != nil {
				return err
			}
			change, err := Unset(path, args[0])
			if err != nil {
				return err
			}
			if !change.Removed {
				cmd.Printf("%s was not written, so nothing changed.\n", change.Key)
				return nil
			}
			cmd.Printf("Removed %s, which held %v. It now follows this binary's default.\n",
				change.Key, change.From)
			return nil
		},
	}
}

// upgradeCmd moves the file forward through the migration chain.
func upgradeCmd(defaultHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Move sei.toml onto this binary's schema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := path(cmd, defaultHome)
			if err != nil {
				return err
			}
			dryRun, err := cmd.Flags().GetBool("dry-run")
			if err != nil {
				return err
			}

			steps, err := seitoml.Upgrade(path, seitoml.Migrations(), dryRun, version.Version)
			for _, step := range steps {
				cmd.Printf("version %d: %s\n", step.To, step.Summary)
				for _, key := range step.Changed {
					cmd.Printf("    %s\n", key)
				}
			}
			if err != nil {
				return err
			}
			switch {
			case len(steps) == 0:
				cmd.Printf("%s is already on schema version %d.\n", path, seitoml.CurrentVersion())
			case dryRun:
				cmd.Printf("Nothing was written. Run without --dry-run to apply these %d step(s).\n",
					len(steps))
			default:
				cmd.Printf("%s is now on schema version %d.\n", path, seitoml.CurrentVersion())
			}
			return nil
		},
	}
	cmd.Flags().Bool("dry-run", false, "Report what would change and write nothing")
	return cmd
}

// path resolves the file this command acts on.
func path(cmd *cobra.Command, defaultHome string) (string, error) {
	home, err := homeDir(cmd, defaultHome)
	if err != nil {
		return "", err
	}
	return Path(home), nil
}

// homeDir resolves the node's home directory, which is where the existing configuration lives.
func homeDir(cmd *cobra.Command, defaultHome string) (string, error) {
	home := defaultHome
	if cmd.Flags().Lookup(flags.FlagHome) != nil {
		fromFlag, err := cmd.Flags().GetString(flags.FlagHome)
		if err != nil {
			return "", err
		}
		if fromFlag != "" {
			home = fromFlag
		}
	}
	if home == "" {
		return "", fmt.Errorf("no home directory; pass --home")
	}
	return home, nil
}

// adoptInto builds the file from a node's existing configuration and reports what it did.
func adoptInto(cmd *cobra.Command, path, home string, mode registry.Mode) error {
	existing, err := LegacySource(home)
	if err != nil {
		return err
	}
	adoption, err := Adopt(existing, os.LookupEnv, mode)
	if err != nil {
		return err
	}
	if err := adoption.File.Save(path); err != nil {
		return err
	}
	cmd.Printf("Wrote %s for %q mode, from this node's existing configuration.\n", path, mode)
	cmd.Print(adoption.Report())
	return nil
}

// target resolves both the file and the mode, for the verbs that need baselines.
//
// The mode is passed through unchecked. Every function that turns a mode into baselines refuses one
// no node runs, so checking here as well would put the same guard in two places and let the one
// that matters be removed without a test noticing.
func target(cmd *cobra.Command, defaultHome string) (string, registry.Mode, error) {
	p, err := path(cmd, defaultHome)
	if err != nil {
		return "", "", err
	}
	raw, err := cmd.Flags().GetString(flagMode)
	if err != nil {
		return "", "", err
	}
	return p, registry.Mode(raw), nil
}

// modeUsage lists the modes, so the help text cannot name one the registry does not have.
func modeUsage() string {
	return fmt.Sprintf("The node mode to resolve defaults for (required); one of %v", registry.Modes())
}

// tendermintMode reads the mode config.toml declares.
//
// Read straight from the file rather than from a running node's context, because the doctor runs
// without one. An absent or unreadable file yields no mode, which the caller treats as a comparison it
// cannot make rather than as a finding.
func tendermintMode(home string) (string, error) {
	path := filepath.Join(home, "config", "config.toml")
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return "", err
	}
	return v.GetString("mode"), nil
}
