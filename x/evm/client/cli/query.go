package cli

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
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
	cmd.AddCommand(CmdQueryERC20())

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

func CmdQueryERC20() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "erc20 [addr] [method] [arguments...]",
		Short: "get hex payload for the given inputs",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			queryClient := types.NewQueryClient(clientCtx)
			abi, err := native.NativeMetaData.GetAbi()
			if err != nil {
				return err
			}
			var bz []byte
			switch args[1] {
			case "name", "symbol", "decimals", "totalSupply":
				bz, err = abi.Pack(args[1])
			case "balanceOf":
				acc := common.HexToAddress(args[2])
				bz, err = abi.Pack(args[1], acc)
			case "allowance":
				owner := common.HexToAddress(args[2])
				spender := common.HexToAddress(args[3])
				bz, err = abi.Pack(args[1], owner, spender)
			default:
				return errors.New("unknown method")
			}
			if err != nil {
				return err
			}
			res, err := queryClient.StaticCall(context.Background(), &types.QueryStaticCallRequest{
				To:   args[0],
				Data: bz,
			})
			if err != nil {
				return err
			}
			fields, err := abi.Unpack(args[1], res.Data)
			if err != nil {
				return err
			}
			var output string
			switch args[1] {
			case "name", "symbol":
				output = fields[0].(string)
			case "decimals":
				output = fmt.Sprintf("%d", fields[0].(uint8))
			case "totalSupply", "balanceOf", "allowance":
				output = fields[0].(*big.Int).String()
			}

			return clientCtx.PrintString(output)
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
