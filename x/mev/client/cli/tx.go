package cli

import (
    "fmt"

    "github.com/spf13/cobra"

    "github.com/cosmos/cosmos-sdk/client"
    "github.com/cosmos/cosmos-sdk/client/flags"
    "github.com/cosmos/cosmos-sdk/client/tx"
    "github.com/sei-protocol/sei-chain/x/mev/types"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:                        types.ModuleName,
        Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
        DisableFlagParsing:         true,
        SuggestionsMinimumDistance: 2,
        RunE:                       client.ValidateCmd,
    }

    // Add tx commands here when needed
    
    return cmd
}
