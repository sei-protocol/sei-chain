package server

// DONTCOVER

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
)

// LatestVersionCmd returns the latest version of the application DB
func LatestVersionCmd(defaultNodeHome string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "latest_version",
		Short: "Prints the latest version of the app DB",
		RunE: func(cmd *cobra.Command, args []string) error {
			serverCtx := GetServerContextFromCmd(cmd)
			config := serverCtx.Config

			homeDir, _ := cmd.Flags().GetString(flags.FlagHome)
			config.SetRoot(homeDir)

			if _, err := os.Stat(config.GenesisFile()); os.IsNotExist(err) {
				return err
			}

			db, err := openDB(config.RootDir)
			if err != nil {
				return err
			}
			fmt.Println(rootmulti.GetLatestVersion(db))

			return nil
		},
	}

	cmd.Flags().String(flags.FlagHome, defaultNodeHome, "The application home directory")

	return cmd
}
