package main

import (
	"os"

	svrcmd "github.com/sei-protocol/sei-chain/cosmos-sdk/server/cmd"
	"github.com/sei-protocol/sei-chain/interchain-accounts/app"
	"github.com/sei-protocol/sei-chain/interchain-accounts/cmd/icad/cmd"
)

func main() {
	rootCmd, _ := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
