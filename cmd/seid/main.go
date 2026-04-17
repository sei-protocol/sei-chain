package main

import (
	"os"

	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/cmd/seid/cmd"

	"github.com/sei-protocol/sei-chain/app"
	svrcmd "github.com/sei-protocol/sei-chain/sei-cosmos/server/cmd"
)

func main() {
	params.SetAddressPrefixes()
	rootCmd, _ := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
