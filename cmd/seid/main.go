package main

import (
	"context"
	"os"

	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/cmd/seid/cmd"

	"github.com/sei-protocol/sei-chain/app"

	tmcli "github.com/tendermint/tendermint/libs/cli"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
)

func main() {
	params.SetAddressPrefixes()
	// Create and set a client.Context on the command's Context. During the pre-run
	// of the root command, a default initialized client.Context is provided to
	// seed child command execution with values such as AccountRetriver, Keyring,
	// and a Tendermint RPC. This requires the use of a pointer reference when
	// getting and setting the client.Context. Ideally, we utilize
	// https://github.com/spf13/cobra/pull/1118.
	srvCtx := server.NewDefaultContext()
	ctx := context.Background()
	ctx = context.WithValue(ctx, client.ClientContextKey, &client.Context{})
	ctx = context.WithValue(ctx, server.ServerContextKey, srvCtx)

	// Config.toml not yet initialized so we can't use the config file to set
	rootCmd, _ := cmd.NewRootCmd()
	rootCmd.PersistentFlags().String(flags.FlagLogLevel, "", "The logging level (trace|debug|info|warn|error|fatal|panic)")
	rootCmd.PersistentFlags().String(flags.FlagLogFormat, "", "The logging format (json|plain)")

	executor := tmcli.PrepareBaseCmd(rootCmd, "", app.DefaultNodeHome)
	if err := executor.ExecuteContext(ctx); err!=nil {
		os.Exit(1)
	}
	
}
