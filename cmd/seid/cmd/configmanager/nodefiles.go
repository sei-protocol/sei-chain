// Reading the files a node already has, without the boot's handler.
//
// The handler that builds the boot's source generates config.toml and app.toml when they are absent, and
// copies configuration values into flags. A command that answers a question about those files must do
// neither, so it reads them here instead.

package configmanager

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// theHomeThisCommandRuns resolves the home directory this command was given, and refuses an empty one.
//
// Every path a command reads joins the home. An empty one leaves those paths relative, so a read lands in
// ./config under whatever directory the command was run from, which is some other node's files. Answering
// for the wrong node is worse than not answering, so the two are resolved together and no caller can hold
// a home without having checked it.
func theHomeThisCommandRuns(cmd *cobra.Command) (string, error) {
	home, err := resolveHomeDir(cmd)
	if err != nil {
		return "", fmt.Errorf("resolve the home directory: %w", err)
	}
	if home == "" {
		return "", fmt.Errorf("no home directory is set, so the files read here would be whichever ones "+
			"the working directory holds. Pass --home, or set %s", theVariableThatSetsTheHome())
	}
	return home, nil
}

// theVariableThatSetsTheHome names the environment variable the home resolves from.
//
// Derived from the running binary the same way the resolver derives it, so a message naming it cannot
// drift from the name that actually works.
func theVariableThatSetsTheHome() string {
	exe, err := os.Executable()
	if err != nil {
		return "the home variable for this binary"
	}
	return strings.ToUpper(path.Base(exe)) + "_HOME"
}

// theNodesOwnConfiguration reads the node's own configuration file into the struct a boot decodes it into.
//
// Decoded rather than read key by key, so every caller sees the same shape a boot sees. A boot unmarshals
// this file over the same defaults, so a key stated with nothing after it arrives empty and an absent key
// keeps the default, which is what a boot runs with.
//
// A file that is not there is the only absence. Every other failure is a file somebody wrote that a boot
// does not start on, so answering with defaults would describe a node that cannot boot.
func theNodesOwnConfiguration(home string) (*tmcfg.Config, error) {
	cfg := tmcfg.DefaultConfig()
	v := viper.New()
	v.SetConfigFile(filepath.Join(home, "config", "config.toml"))
	switch err := v.ReadInConfig(); {
	case errors.Is(err, fs.ErrNotExist):
		return cfg, nil
	case err != nil:
		return nil, err
	}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// startCommandName is the subcommand whose flags answer a key a file leaves out.
const startCommandName = "start"

// theSourceThisNodeWouldBuild returns what this node answers for a key looked up by name, without booting.
//
// A boot binds its start command's flags into the source it builds and then reads app.toml over them, so a
// key with a flag of its own is answered by that flag's default whether the file mentions it or not. The
// same two, in the same order, so a written value outranks a flag's default here as it does there.
//
// A file that is not there leaves the flag defaults answering, which is what a node in that state runs
// until a boot generates one. Every other failure is a file a boot does not start on.
func theSourceThisNodeWouldBuild(cmd *cobra.Command, home string) (*viper.Viper, error) {
	set, err := theStartCommandsFlags(cmd)
	if err != nil {
		return nil, err
	}
	v := viper.New()
	if err := v.BindPFlags(set); err != nil {
		return nil, err
	}
	v.SetConfigFile(filepath.Join(home, "config", "app.toml"))
	switch err := v.ReadInConfig(); {
	case errors.Is(err, fs.ErrNotExist):
		return v, nil
	case err != nil:
		return nil, err
	}
	return v, nil
}

// theStartCommandsFlags returns the flags this binary's start command carries.
//
// Found on the root by name rather than built here, so they are the flags this binary ships and a flag
// added to the start command is carried without anything else changing.
//
// Not found is refused. Without these a key whose only answer is a flag's default reads as unanswered, and
// a caller writing a file from that would leave the key out and move it to its declared value. Pruning is
// the one to picture: the flag prunes and the declaration keeps everything, so the file would silently
// stop a node pruning.
func theStartCommandsFlags(cmd *cobra.Command) (*pflag.FlagSet, error) {
	for _, sub := range cmd.Root().Commands() {
		if sub.Name() != startCommandName {
			continue
		}
		set := pflag.NewFlagSet(startCommandName, pflag.ContinueOnError)
		set.AddFlagSet(sub.Flags())
		set.AddFlagSet(sub.PersistentFlags())
		return set, nil
	}
	return nil, fmt.Errorf("this binary has no %q command, so the defaults its flags carry cannot be "+
		"read and a key answered only by one would read as answered by nothing", startCommandName)
}
