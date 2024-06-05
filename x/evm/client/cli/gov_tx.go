package cli

import (
	"strconv"
	"strings"

	"github.com/sei-protocol/sei-chain/x/evm/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/spf13/cobra"
)

func NewAddERCNativePointerProposalTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-erc-native-pointer title description token name symbol decimals deposit",
		Args:  cobra.ExactArgs(7),
		Short: "Submit an add ERC-native pointer proposal",
		Long: strings.TrimSpace(`
			Submit a proposal to register an ERC pointer contract for a native token with
			provided metadata.
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			decimals, err := strconv.ParseUint(args[5], 10, 8)
			if err != nil {
				return err
			}
			deposit, err := sdk.ParseCoinsNormalized(args[6])
			if err != nil {
				return err
			}

			// Convert proposal to RegisterPairsProposal Type
			from := clientCtx.GetFromAddress()

			content := types.AddERCNativePointerProposalV2{
				Title:       args[0],
				Description: args[1],
				Token:       args[2],
				Name:        args[3],
				Symbol:      args[4],
				Decimals:    uint32(decimals),
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
