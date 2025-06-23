package cli

import (
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/precompiles"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/spf13/cobra"
)

const (
	FlagCwAddress = "cw-address"
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

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func RegisterCwPointerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-cw-pointer [pointer type] [erc address]",
		Short: `Register a CosmWasm pointer for an ERC20/721/1155 contract. Pointer type is either ERC20, ERC721, or ERC1155.`,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgRegisterPointer{
				Sender:      clientCtx.GetFromAddress().String(),
				PointerType: types.PointerType(types.PointerType_value[args[0]]),
				ErcAddress:  args[1],
			}
			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func RegisterEvmPointerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-evm-pointer [pointer type] [cw-address] --gas-fee-cap=<cap> --gas-limit=<limit> --evm-rpc=<url>",
		Short: `Register an EVM pointer for a CosmWasm contract. Pointer type is either CW20, CW721, CW1155, or NATIVE.`,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pInfo := precompiles.GetPrecompileInfo(pointer.PrecompileName)
			var payload []byte
			var err error
			switch args[0] {
			case "CW20":
				payload, err = getMethodPayload(pInfo.ABI, []string{pointer.AddCW20Pointer, args[1]})
			case "CW721":
				payload, err = getMethodPayload(pInfo.ABI, []string{pointer.AddCW721Pointer, args[1]})
			case "CW1155":
				payload, err = getMethodPayload(pInfo.ABI, []string{pointer.AddCW1155Pointer, args[1]})
			case "NATIVE":
				payload, err = getMethodPayload(pInfo.ABI, []string{pointer.AddNativePointer, args[1]})
			default:
				return fmt.Errorf("invalid pointer type: %s", args[0])
			}
			if err != nil {
				return err
			}
			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			key, err := getPrivateKey(cmd)
			if err != nil {
				return err
			}

			rpc, err := cmd.Flags().GetString(FlagRPC)
			if err != nil {
				return err
			}
			nonce, err := getNonce(rpc, key.PublicKey)
			if err != nil {
				return err
			}

			txData.Nonce = nonce
			txData.Data = payload
			addr := common.HexToAddress(pointer.PointerAddress)
			txData.To = &addr

			resp, err := sendTx(txData, rpc, key)
			if err != nil {
				return err
			}

			fmt.Println("Transaction hash:", resp.Hex())
			return nil
		},
	}

	cmd.Flags().Uint64(FlagGasFeeCap, 1000000000000, "Gas fee cap for the transaction")
	cmd.Flags().Uint64(FlagGas, 7000000, "Gas limit for the transaction")
	cmd.Flags().String(FlagCwAddress, "", "CosmWasm contract address")
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
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

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
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

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
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
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
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
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
