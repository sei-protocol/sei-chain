package cli

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/spf13/cobra"
)

func NativeSendTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "native-send [from_key_or_address] [to_evm_address] [amount]",
		Short: `Send funds from one account to an EVM address (e.g. 0x....).
		Note, the '--from' flag is ignored as it is implied from [from_key_or_address].
		When using '--dry-run' a key name cannot be used, only a bech32 address.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Flags().Set(flags.FlagFrom, args[0])
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			coins, err := sdk.ParseCoinsNormalized(args[2])
			if err != nil {
				return err
			}

			msg := &types.MsgSend{
				FromAddress: clientCtx.GetFromAddress().String(),
				ToAddress:   args[1],
				Amount:      coins,
			}
			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func AssociateContractAddressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "associate-contract-address [cw-address]",
		Short: `Set address association for a CosmWasm contract.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			addr, err := sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return err
			}
			msg := types.NewMsgAssociateContractAddress(clientCtx.GetFromAddress(), addr)
			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func NativeAssociateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "native-associate [custom msg]",
		Short: `Set address association for the sender.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.NewMsgAssociate(clientCtx.GetFromAddress(), args[0])
			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func PrintClaimTxPayloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "print-claim [claimer] --from=<sender>",
		Short: `Print hex-encoded claim message payload for Sei Solo migration.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.NewMsgClaim(clientCtx.GetFromAddress(), common.HexToAddress(args[0]))
			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			clientCtx.PrintSignedOnly = true
			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func PrintClaimTxBySenderPayloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "print-claim-by-sender [claimer] [sender] --from=<sender>",
		Short: `Print hex-encoded claim message payload for Sei Solo migration.`,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := types.NewMsgClaim(sdk.MustAccAddressFromBech32(args[1]), common.HexToAddress(args[0]))
			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			clientCtx.PrintSignedOnly = true
			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func PrintClaimSpecificTxPayloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "print-claim-specific [claimer] [[CW20|CW721] [contract addr]]... --from=<sender>",
		Short: `Print hex-encoded claim specific message payload for Sei Solo migration.`,
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			if len(args)%2 != 1 {
				return errors.New("print-claim-specific takes odd number of arguments (the first argument is the claimer address, and the rest are asset type/address pairs)")
			}
			assets := []*types.Asset{}
			for i := 1; i < len(args); i += 2 {
				var assetType types.AssetType
				switch args[i] {
				case "CW20":
					assetType = types.AssetType_TYPECW20
				case "CW721":
					assetType = types.AssetType_TYPECW721
				default:
					return fmt.Errorf("accepted asset types are CW20 and CW721. Received %s", args[i])
				}
				assets = append(assets, &types.Asset{
					AssetType:       assetType,
					ContractAddress: args[i+1],
				})
			}

			msg := types.NewMsgClaimSpecific(clientCtx.GetFromAddress(), common.HexToAddress(args[0]), assets...)
			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			clientCtx.PrintSignedOnly = true
			return tx.GenerateOrBroadcastTxCLI(cmd.Context(), clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
