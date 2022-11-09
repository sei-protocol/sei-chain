package cli

import (
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	cutils "github.com/sei-protocol/sei-chain/x/tokenfactory/client/utils"

	"github.com/spf13/cobra"
)

// NewAddCreatorsToDenomFeeWhitelistProposalTxCmd returns a CLI command handler for creating
// a add creators to whitelist governance transaction
func NewAddCreatorsToDenomFeeWhitelistProposalTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-creators-to-denom-fee-whitelist [proposal-file]",
		Args:  cobra.ExactArgs(1),
		Short: "Submit an add creators to denom fee whitelist proposal",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			proposal, err := cutils.ParseAddCreatorsToDenomFeeWhitelistProposalJSON(clientCtx.LegacyAmino, args[0])
			if err != nil {
				return err
			}

			// Convert proposal to AddCreatorsToDenomFeeWhitelistProposal Type
			from := clientCtx.GetFromAddress()

			content := types.AddCreatorsToDenomFeeWhitelistProposal{Title: proposal.Title, Description: proposal.Description, CreatorList: proposal.CreatorList}

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
