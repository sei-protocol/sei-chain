package query

import (
	"fmt"
	// "strings"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	// "github.com/cosmos/cosmos-sdk/client/flags"
	// sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// GetQueryCmd returns the cli query commands for this module
func GetQueryCmd() *cobra.Command {
	// Group dex queries under a subcommand
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdQueryParams())
	cmd.AddCommand(CmdListLongBook())
	cmd.AddCommand(CmdShowLongBook())
	cmd.AddCommand(CmdListShortBook())
	cmd.AddCommand(CmdShowShortBook())
	cmd.AddCommand(CmdGetPrice())
	cmd.AddCommand(CmdGetLatestPrice())
	cmd.AddCommand(CmdGetPrices())
	cmd.AddCommand(CmdGetTwaps())
	cmd.AddCommand(CmdGetAssetList())
	cmd.AddCommand(CmdGetAssetMetadata())
	cmd.AddCommand(CmdGetRegisteredPairs())
	cmd.AddCommand(CmdGetRegisteredContract())
	cmd.AddCommand(CmdGetOrders())
	cmd.AddCommand(CmdGetOrdersByID())
	cmd.AddCommand(CmdGetMatchResult())
	cmd.AddCommand(CmdGetOrderCount())
	cmd.AddCommand(CmdListAllContractInfo())

	// this line is used by starport scaffolding # 1

	return cmd
}
