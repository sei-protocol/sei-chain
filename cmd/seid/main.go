package main

import (
	"fmt"
	"os"

	"github.com/CosmWasm/wasmd/check"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/cmd/seid/cmd"
)

func main() {

	if trimmed, ok := check.RuntimePathTrimmed(); ok && trimmed {
		_, _ = fmt.Fprintln(os.Stderr, "WARNING: paths are likely trimmed. This can result in inconsistent error stacktrace.")
	}

	params.SetAddressPrefixes()
	rootCmd, _ := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, app.DefaultNodeHome); err != nil {
		os.Exit(1)
	}
}
