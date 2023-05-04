package tx

import (
	"strings"

	"github.com/sei-protocol/sei-chain/x/dex/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	cutils "github.com/sei-protocol/sei-chain/x/dex/client/utils"

	"github.com/spf13/cobra"
)

// NewAddAssetProposalTxCmd returns a CLI command handler for creating
// a add asset proposal governance transaction.
func NewAddAssetProposalTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-asset-proposal [proposal-file]",
		Args:  cobra.ExactArgs(1),
		Short: "Submit an add asset proposal",
		Long: strings.TrimSpace(`
			Submit a proposal to add a list of assets and corresponding metadata to dex assets.
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			proposal, err := cutils.ParseAddAssetMetadataProposalJSON(clientCtx.LegacyAmino, args[0])
			if err != nil {
				return err
			}

			// Convert proposal to RegisterPairsProposal Type
			from := clientCtx.GetFromAddress()

			content := types.AddAssetMetadataProposal{Title: proposal.Title, Description: proposal.Description, AssetList: proposal.AssetList}

			deposit, err := sdk.ParseCoinsNormalized(proposal.Deposit)
			if err != nil {
				return err
			}

			msg, err := govtypes.NewMsgSubmitProposal(&content, deposit, from)
			if err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
