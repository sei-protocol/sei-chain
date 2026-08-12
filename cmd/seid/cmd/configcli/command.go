package configcli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/config/registry"
	"github.com/sei-protocol/sei-chain/config/seitoml"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
)

// FileName is what the configuration is called inside a node's config directory.
const FileName = "sei.toml"

// flagMode names the node mode a verb resolves baselines for.
const flagMode = "mode"

// Path is where a node's sei.toml lives, given its home directory.
//
// Beside the files a node already keeps, so an operator finds it where they look for app.toml and
// config.toml rather than somewhere this tool invented.
func Path(home string) string {
	return filepath.Join(home, "config", FileName)
}

// Command is the seid config tree.
//
// Every verb resolves its own path from --home, because this tree does not run the boot's
// pre-run hook and therefore cannot rely on anything it would have set up.
func Command(defaultHome string) *cobra.Command {
	cmd := &cobra.Command{
		// Not "config": that name is taken by the client configuration command, which manages
		// client.toml. Two unrelated files under one verb would leave an operator unable to tell
		// which one `seid config set` wrote.
		Use:   "node-config",
		Short: "Inspect and edit sei.toml",
		Long: "Read and edit this node's sei.toml.\n\n" +
			"A key written in the file is your decision and this binary never rewrites it. A key " +
			"absent from the file follows the default for the node's mode, which may change between " +
			"releases.",
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.PersistentFlags().String(flags.FlagHome, defaultHome, "The application home directory")

	cmd.AddCommand(
		generateCmd(),
		showCmd(),
		diffCmd(),
		doctorCmd(),
		setCmd(),
		unsetCmd(),
		upgradeCmd(),
	)
	return cmd
}

// generateCmd writes a fresh file for a mode.
func generateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Write a complete sei.toml for a node mode",
		Long: "Write every setting this binary knows, at the default for the given mode.\n\n" +
			"Every key written this way is a value this binary will not rewrite, so the node keeps " +
			"them across an upgrade even where a later release ships a different default. Run this " +
			"again to move onto current defaults.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, mode, err := target(cmd)
			if err != nil {
				return err
			}
			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return err
			}
			if !force {
				if _, err := seitoml.Load(path); err == nil {
					return fmt.Errorf("%s already exists. Generating would replace every value in it, "+
						"including any you set. Pass --force to do that, or use diff to see what it "+
						"holds first", path)
				}
			}

			file, err := Generate(mode)
			if err != nil {
				return err
			}
			if err := file.Save(path); err != nil {
				return err
			}
			cmd.Printf("Wrote %s for %q mode.\n", path, mode)
			return nil
		},
	}
	cmd.Flags().String(flagMode, string(registry.ModeFull), modeUsage())
	cmd.Flags().Bool("force", false, "Replace an existing file, discarding every value in it")
	return cmd
}

// showCmd prints the file as it stands.
func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print this node's sei.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := path(cmd)
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
func diffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare sei.toml against this binary's defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, mode, err := target(cmd)
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
	cmd.Flags().String(flagMode, string(registry.ModeFull), modeUsage())
	return cmd
}

// doctorCmd checks what the file writes against what this binary declares.
func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check sei.toml for settings this binary does not recognize",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := path(cmd)
			if err != nil {
				return err
			}
			file, err := seitoml.Load(path)
			if err != nil {
				return err
			}
			diagnosis, err := Doctor(file)
			if err != nil {
				return err
			}
			cmd.Print(diagnosis.Report())
			if !diagnosis.Healthy() {
				// A non-zero exit is what lets an operator gate a deploy on this, and the report above
				// already names every key, so this adds no second copy of the list.
				return fmt.Errorf("%d written setting(s) are not recognized by this binary",
					len(diagnosis.Unrecognized))
			}
			return nil
		},
	}
}

// setCmd writes one value.
func setCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write one setting",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := path(cmd)
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
func unsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove one setting so it follows this binary's default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := path(cmd)
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
func upgradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Move sei.toml onto this binary's schema",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := path(cmd)
			if err != nil {
				return err
			}
			dryRun, err := cmd.Flags().GetBool("dry-run")
			if err != nil {
				return err
			}

			steps, err := seitoml.Upgrade(path, seitoml.Migrations(), dryRun)
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
func path(cmd *cobra.Command) (string, error) {
	home, err := cmd.Flags().GetString(flags.FlagHome)
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", fmt.Errorf("no home directory; pass --home")
	}
	return Path(home), nil
}

// target resolves both the file and the mode, for the verbs that need baselines.
//
// The mode is passed through unchecked. Every function that turns a mode into baselines refuses one
// no node runs, so checking here as well would put the same guard in two places and let the one
// that matters be removed without a test noticing.
func target(cmd *cobra.Command) (string, registry.Mode, error) {
	p, err := path(cmd)
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
	return fmt.Sprintf("The node mode to resolve defaults for; one of %v", registry.Modes())
}
