package cli

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// RunWithArgs executes the given command with the specified command line args
// and environmental variables set. It returns any error returned from cmd.Execute()
//
// This is only used in testing.
func RunWithArgs(ctx context.Context, cmd *cobra.Command, args []string, env map[string]string) error {
	oargs := os.Args
	oenv := map[string]string{}
	// defer returns the environment back to normal
	defer func() {
		os.Args = oargs
		for k, v := range oenv {
			os.Setenv(k, v)
		}
	}()

	// set the args and env how we want them
	os.Args = args
	for k, v := range env {
		// backup old value if there, to restore at end
		oenv[k] = os.Getenv(k)
		err := os.Setenv(k, v)
		if err != nil {
			return err
		}
	}

	// and finally run the command
	return RunWithTrace(ctx, cmd)
}

func RunWithTrace(ctx context.Context, cmd *cobra.Command) error {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.ExecuteContext(ctx); err != nil {
		if viper.GetBool(TraceFlag) {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			fmt.Fprintf(os.Stderr, "ERROR: %v\n%s\n", err, buf)
		} else {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		}

		return err
	}
	return nil
}

// WriteConfigVals writes a toml file with the given values.
// It returns an error if writing was impossible.
func WriteConfigVals(dir string, vals map[string]string) error {
	data := ""
	for k, v := range vals {
		data += fmt.Sprintf("%s = \"%s\"\n", k, v)
	}
	cfile := filepath.Join(dir, "config.toml")
	return ioutil.WriteFile(cfile, []byte(data), 0600)
}

// NewCompletionCmd returns a cobra.Command that generates bash and zsh
// completion scripts for the given root command. If hidden is true, the
// command will not show up in the root command's list of available commands.
func NewCompletionCmd(rootCmd *cobra.Command, hidden bool) *cobra.Command {
	flagZsh := "zsh"
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate shell completion scripts",
		Long: fmt.Sprintf(`Generate Bash and Zsh completion scripts and print them to STDOUT.

Once saved to file, a completion script can be loaded in the shell's
current session as shown:

   $ . <(%s completion)

To configure your bash shell to load completions for each session add to
your $HOME/.bashrc or $HOME/.profile the following instruction:

   . <(%s completion)
`, rootCmd.Use, rootCmd.Use),
		RunE: func(cmd *cobra.Command, _ []string) error {
			zsh, err := cmd.Flags().GetBool(flagZsh)
			if err != nil {
				return err
			}
			if zsh {
				return rootCmd.GenZshCompletion(cmd.OutOrStdout())
			}
			return rootCmd.GenBashCompletion(cmd.OutOrStdout())
		},
		Hidden: hidden,
		Args:   cobra.NoArgs,
	}

	cmd.Flags().Bool(flagZsh, false, "Generate Zsh completion script")

	return cmd
}
