// Reading a node's legacy files without the boot's handler, which would create missing ones and copy
// their values into flags.

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

// theHomeThisCommandRuns resolves this command's home directory, refusing an empty one, which would read
// ./config under the working directory.
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

// theVariableThatSetsTheHome names the environment variable the home resolves from, derived as
// resolveHomeDir derives it.
func theVariableThatSetsTheHome() string {
	exe, err := os.Executable()
	if err != nil {
		return "the home variable for this binary"
	}
	return strings.ToUpper(path.Base(exe)) + "_HOME"
}

// theNodesOwnConfiguration decodes config.toml over the defaults, as a boot does. A missing file yields
// the defaults; any other failure is an error.
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

// theSourceThisNodeWouldBuild returns the lookup source a boot builds: app.toml read over the start
// command's flag defaults. A missing app.toml leaves the flag defaults answering.
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

// theStartCommandsFlags returns the flags of the root's start command. Missing is refused: a key answered
// only by a flag's default, such as pruning, would otherwise read as unanswered.
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
