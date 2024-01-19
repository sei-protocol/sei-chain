package cli

import (
	"context"
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// GetQueryCmd returns the cli query commands for this module
func GetQueryCmd(_ string) *cobra.Command {
	// Group epoch queries under a subcommand
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdQuerySeiAddress())
	cmd.AddCommand(CmdQueryEVMAddress())
	cmd.AddCommand(CmdQueryERC20Payload())
	cmd.AddCommand(CmdQueryERC721Payload())

	return cmd
}

func CmdQuerySeiAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sei-addr",
		Short: "gets sei address (sei...) by EVM address (0x...) if account has association set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.SeiAddressByEVMAddress(context.Background(), &types.QuerySeiAddressByEVMAddressRequest{EvmAddress: args[0]})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func CmdQueryEVMAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evm-addr",
		Short: "gets evm address (0x...) by Sei address (sei...) if account has association set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.EVMAddressBySeiAddress(context.Background(), &types.QueryEVMAddressBySeiAddressRequest{SeiAddress: args[0]})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func CmdQueryERC20Payload() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "erc20-payload [method] [arguments...]",
		Short: "get hex payload for the given inputs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			abi, err := cw20.Cw20MetaData.GetAbi()
			if err != nil {
				return err
			}
			var bz []byte
			switch args[0] {
			case "transfer":
				to := common.HexToAddress(args[1])
				amt, _ := sdk.NewIntFromString(args[2])
				bz, err = abi.Pack(args[0], to, amt.BigInt())
			case "approve":
				spender := common.HexToAddress(args[1])
				amt, _ := sdk.NewIntFromString(args[2])
				bz, err = abi.Pack(args[0], spender, amt.BigInt())
			case "transferFrom":
				from := common.HexToAddress(args[1])
				to := common.HexToAddress(args[2])
				amt, _ := sdk.NewIntFromString(args[3])
				bz, err = abi.Pack(args[0], from, to, amt.BigInt())
			}
			if err != nil {
				return err
			}

			return clientCtx.PrintString(hex.EncodeToString(bz))
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func CmdQueryERC721Payload() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "erc721-payload [method] [arguments...]",
		Short: "get hex payload for the given inputs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)

			abi, err := cw721.Cw721MetaData.GetAbi()
			if err != nil {
				return err
			}
			var bz []byte
			switch args[0] {
			case "approve":
				spender := common.HexToAddress(args[1])
				id, _ := sdk.NewIntFromString(args[2])
				bz, err = abi.Pack(args[0], spender, id.BigInt())
			case "transferFrom":
				from := common.HexToAddress(args[1])
				to := common.HexToAddress(args[2])
				id, _ := sdk.NewIntFromString(args[3])
				bz, err = abi.Pack(args[0], from, to, id.BigInt())
			case "setApprovalForAll":
				op := common.HexToAddress(args[1])
				approved := args[2] == "true"
				bz, err = abi.Pack(args[0], op, approved)
			}
			if err != nil {
				return err
			}

			return clientCtx.PrintString(hex.EncodeToString(bz))
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
