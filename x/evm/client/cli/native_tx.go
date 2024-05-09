package cli

import (
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
		Short: `Register a CosmWasm pointer for an ERC20/721 contract. Pointer type is either ERC20 or ERC721`,
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

func RegisterErcPointerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register-erc-pointer [pointer type] [cw-address] --gas-fee-cap=<cap> --gas-limit=<limit> --evm-rpc=<url>",
		Short: `Register an ERC pointer for a CosmWasm contract. Pointer type is either CW20, CW721, or NATIVE`,
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
