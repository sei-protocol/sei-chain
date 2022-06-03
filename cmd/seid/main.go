package main

import (
	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/cmd/seid/cmd"
	"os"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/sei-protocol/sei-chain/app"
)

func main() {
	params.SetAddressPrefixes()
	rootCmd, _ := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
