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
		Use:   "add-erc-native-pointer title description token version deposit [pointer address]",
		Args:  cobra.RangeArgs(5, 6),
		Short: "Submit an add ERC-native pointer proposal",
		Long: strings.TrimSpace(`
			Submit a proposal to register an ERC pointer contract address for a native token.
			Not specifying the pointer address means a proposal that deletes the existing pointer.
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			version, err := strconv.ParseUint(args[3], 10, 16)
			if err != nil {
				return err
			}
			deposit, err := sdk.ParseCoinsNormalized(args[4])
			if err != nil {
				return err
			}
			var pointer string
			if len(args) == 6 {
				pointer = args[5]
			}

			// Convert proposal to RegisterPairsProposal Type
			from := clientCtx.GetFromAddress()

			content := types.AddERCNativePointerProposal{
				Title:       args[0],
				Description: args[1],
				Token:       args[2],
				Version:     uint32(version),
				Pointer:     pointer,
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

func NewAddERCCW20PointerProposalTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-erc-cw20-pointer title description cw20address version deposit [pointer address]",
		Args:  cobra.RangeArgs(5, 6),
		Short: "Submit an add ERC-CW20 pointer proposal",
		Long: strings.TrimSpace(`
			Submit a proposal to register an ERC pointer contract address for a CW20 token.
			Not specifying the pointer address means a proposal that deletes the existing pointer.
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			version, err := strconv.ParseUint(args[3], 10, 16)
			if err != nil {
				return err
			}
			deposit, err := sdk.ParseCoinsNormalized(args[4])
			if err != nil {
				return err
			}
			var pointer string
			if len(args) == 6 {
				pointer = args[5]
			}

			// Convert proposal to RegisterPairsProposal Type
			from := clientCtx.GetFromAddress()

			content := types.AddERCCW20PointerProposal{
				Title:       args[0],
				Description: args[1],
				Pointee:     args[2],
				Version:     uint32(version),
				Pointer:     pointer,
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

func NewAddERCCW721PointerProposalTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-erc-cw721-pointer title description cw721address version deposit [pointer address]",
		Args:  cobra.RangeArgs(5, 6),
		Short: "Submit an add ERC-CW721 pointer proposal",
		Long: strings.TrimSpace(`
			Submit a proposal to register an ERC pointer contract address for a CW721 token.
			Not specifying the pointer address means a proposal that deletes the existing pointer.
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			version, err := strconv.ParseUint(args[3], 10, 16)
			if err != nil {
				return err
			}
			deposit, err := sdk.ParseCoinsNormalized(args[4])
			if err != nil {
				return err
			}
			var pointer string
			if len(args) == 6 {
				pointer = args[5]
			}

			// Convert proposal to RegisterPairsProposal Type
			from := clientCtx.GetFromAddress()

			content := types.AddERCCW721PointerProposal{
				Title:       args[0],
				Description: args[1],
				Pointee:     args[2],
				Version:     uint32(version),
				Pointer:     pointer,
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
