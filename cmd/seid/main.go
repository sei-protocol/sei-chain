package main

import (
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/cmd/seid/cmd"
	"os"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/sei-protocol/sei-chain/app"
)

func main() {
	params.SetAddressPrefixes()
	rootCmd, _ := cmd.NewRootCmd()
	config := server.GetServerContextFromCmd(rootCmd).Config
	params.SetTendermintConfigs(config)
	if err := svrcmd.Execute(rootCmd, app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
