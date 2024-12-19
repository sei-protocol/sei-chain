package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/sei-protocol/sei-chain/precompiles/confidentialtransfers"
	cttutils "github.com/sei-protocol/sei-chain/x/confidentialtransfers/client/cli"
	cttypes "github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw1155"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	TrueStr      = "true"
	FalseStr     = "false"
	auditorsFlag = "auditors"
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
	cmd.AddCommand(CmdQueryERC1155Payload())
	cmd.AddCommand(CmdQueryERC20())
	cmd.AddCommand(CmdQueryPayload())
	cmd.AddCommand(CmdQueryPointer())
	cmd.AddCommand(CmdQueryPointerVersion())
	cmd.AddCommand(CmdQueryPointee())
	cmd.AddCommand(GetCmdQueryCtTransferPayload())

	return cmd
}

func CmdQuerySeiAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sei-addr",
		Short: "gets sei address (sei...) by EVM address (0x...) if account has association set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

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
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
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
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
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

func CmdQueryPayload() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "payload [abi-filepath] [method] [arguments...]",
		Short: "get hex payload for the given inputs",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			dat, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}

			newAbi, err := abi.JSON(bytes.NewReader(dat))
			if err != nil {
				return err
			}
			bz, err := getMethodPayload(newAbi, args[1:])
			if err != nil {
				return err
			}

			return clientCtx.PrintString(hex.EncodeToString(bz))
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
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			abi, err := cw20.Cw20MetaData.GetAbi()
			if err != nil {
				return err
			}
			var bz []byte
			switch args[0] {
			case "transfer":
				if len(args) != 3 {
					return errors.New("expected usage: `seid tx evm erc20-payload transfer [to] [amount]`")
				}
				to := common.HexToAddress(args[1])
				amt, _ := sdk.NewIntFromString(args[2])
				bz, err = abi.Pack(args[0], to, amt.BigInt())
			case "approve":
				if len(args) != 3 {
					return errors.New("expected usage: `seid tx evm erc20-payload approve [spender] [amount]`")
				}
				spender := common.HexToAddress(args[1])
				amt, _ := sdk.NewIntFromString(args[2])
				bz, err = abi.Pack(args[0], spender, amt.BigInt())
			case "transferFrom":
				if len(args) != 4 {
					return errors.New("expected usage: `seid tx evm erc20-payload transferFrom [from] [to] [amount]`")
				}
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
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			abi, err := cw721.Cw721MetaData.GetAbi()
			if err != nil {
				return err
			}
			var bz []byte
			switch args[0] {
			case "approve":
				if len(args) != 3 {
					return errors.New("expected usage: `seid tx evm erc721-payload approve [spender] [tokenId]`")
				}
				spender := common.HexToAddress(args[1])
				id, _ := sdk.NewIntFromString(args[2])
				bz, err = abi.Pack(args[0], spender, id.BigInt())
			case "transferFrom":
				if len(args) != 4 {
					return errors.New("expected usage: `seid tx evm erc721-payload transferFrom [from] [to] [tokenId]`")
				}
				from := common.HexToAddress(args[1])
				to := common.HexToAddress(args[2])
				id, _ := sdk.NewIntFromString(args[3])
				bz, err = abi.Pack(args[0], from, to, id.BigInt())
			case "setApprovalForAll":
				if len(args) != 3 {
					return errors.New("expected usage: `seid tx evm erc721-payload setApprovalForAll [spender] [ture|false]`")
				}
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

func CmdQueryERC1155Payload() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "erc1155-payload [method] [arguments...]",
		Short: "get hex payload for the given inputs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			abi, err := cw1155.Cw1155MetaData.GetAbi()
			if err != nil {
				return err
			}
			var bz []byte
			switch args[0] {
			case "safeTransferFrom":
				if len(args) != 6 {
					return errors.New("expected usage: `seid tx evm erc1155-payload safeTransferFrom [from] [to] [tokenId] [amount] [data]`")
				}
				from := common.HexToAddress(args[1])
				to := common.HexToAddress(args[2])
				id, _ := sdk.NewIntFromString(args[3])
				amt, _ := sdk.NewIntFromString(args[4])
				bz, err = abi.Pack(args[0], from, to, id.BigInt(), amt.BigInt(), []byte(args[5]))
			case "safeBatchTransferFrom":
				if len(args) != 6 {
					return errors.New("expected usage: `seid tx evm erc1155-payload safeBatchTransferFrom [from] [to] [tokenIds] [amounts] [data]`")
				}
				from := common.HexToAddress(args[1])
				to := common.HexToAddress(args[2])
				idsRaw := strings.Split(strings.ReplaceAll(strings.ReplaceAll(args[3], "[", ""), "]", ""), ",")
				var ids []*big.Int
				for _, n := range idsRaw {
					id, ok := sdk.NewIntFromString(strings.Trim(n, " "))
					if !ok {
						return errors.New("cannot parse array of int from: " + args[3])
					}
					ids = append(ids, id.BigInt())
				}
				amtsRaw := strings.Split(strings.ReplaceAll(strings.ReplaceAll(args[4], "[", ""), "]", ""), ",")
				var amts []*big.Int
				for _, n := range amtsRaw {
					amt, ok := sdk.NewIntFromString(strings.Trim(n, " "))
					if !ok {
						return errors.New("cannot parse array of int from: " + args[4])
					}
					amts = append(amts, amt.BigInt())
				}
				bz, err = abi.Pack(args[0], from, to, ids, amts, []byte(args[5]))
			case "setApprovalForAll":
				if len(args) != 3 {
					return errors.New("expected usage: `seid tx evm erc1155-payload setApprovalForAll [spender] [ture|false]`")
				}
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

func CmdQueryPointer() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pointer [type] [pointee]",
		Short: "get pointer address of the specified type (one of [NATIVE, CW20, CW721, CW1155, ERC20, ERC721, ERC1155]) and pointee",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)
			ctx := cmd.Context()

			res, err := queryClient.Pointer(ctx, &types.QueryPointerRequest{
				PointerType: types.PointerType(types.PointerType_value[args[0]]), Pointee: args[1],
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func CmdQueryPointerVersion() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pointer-version [type]",
		Short: "Query for the current pointer version and stored code ID (if applicable)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			ctx := cmd.Context()

			req := types.QueryPointerVersionRequest{PointerType: types.PointerType(types.PointerType_value[args[0]])}
			res, err := queryClient.PointerVersion(ctx, &req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func CmdQueryPointee() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pointee [type] [pointer]",
		Short: "Get pointee address of the specified type (one of [NATIVE, CW20, CW721, ERC20, ERC721]) and pointer",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)
			ctx := cmd.Context()

			res, err := queryClient.Pointee(ctx, &types.QueryPointeeRequest{
				PointerType: types.PointerType(types.PointerType_value[args[0]]), Pointer: args[1],
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func GetCmdQueryCtTransferPayload() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ct-transfer-payload [abi-filepath] [from_evm_address] [to_evm_address] [amount] [flags]",
		Short: "get hex payload for the confidential transfer",
		Args:  cobra.ExactArgs(4),
		RunE:  queryCtTransferPayload,
	}

	flags.AddQueryFlagsToCmd(cmd)
	cmd.Flags().StringSlice(auditorsFlag, []string{}, "List of auditors")

	return cmd
}

func queryCtTransferPayload(cmd *cobra.Command, args []string) error {
	queryClientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}

	queryClient := types.NewQueryClient(queryClientCtx)
	ctQueryClient := cttypes.NewQueryClient(queryClientCtx)

	fromEvmAddress := common.HexToAddress(args[1])
	fromRes, err := queryClient.SeiAddressByEVMAddress(context.Background(), &types.QuerySeiAddressByEVMAddressRequest{EvmAddress: fromEvmAddress.String()})
	if err != nil {
		return err
	}
	if !fromRes.Associated {
		return errors.New("from address not associated with a Sei address")
	}

	_, name, _, err := client.GetFromFields(queryClientCtx, queryClientCtx.Keyring, fromRes.SeiAddress)
	if err != nil {
		return err
	}
	privKey, err := getPrivateKeyForName(cmd, name)
	if err != nil {
		return err
	}

	toEvmAddress := common.HexToAddress(args[2])
	toRes, err := queryClient.SeiAddressByEVMAddress(context.Background(), &types.QuerySeiAddressByEVMAddressRequest{EvmAddress: toEvmAddress.String()})
	if err != nil {
		return err
	}
	if !toRes.Associated {
		return errors.New("to address not associated with a Sei address")
	}

	coin, err := sdk.ParseCoinNormalized(args[3])
	if err != nil {
		return err
	}

	senderAccount, err := cttutils.GetAccount(ctQueryClient, fromRes.SeiAddress, coin.Denom)
	if err != nil {
		return err
	}

	recipientAccount, err := cttutils.GetAccount(ctQueryClient, toRes.SeiAddress, coin.Denom)
	if err != nil {
		return err
	}

	auditorAddrs, err := cmd.Flags().GetStringSlice(auditorsFlag)
	if err != nil {
		return err
	}

	var auditors []cttypes.AuditorInput
	transferMethod := confidentialtransfers.TransferMethod

	if auditorAddrs != nil && len(auditorAddrs) > 0 {
		auditors = make([]cttypes.AuditorInput, len(auditorAddrs))
		for i, auditorAddr := range auditorAddrs {
			auditorAccount, err := cttutils.GetAccount(ctQueryClient, auditorAddr, coin.Denom)
			if err != nil {
				return err
			}
			auditors[i] = cttypes.AuditorInput{
				Address: auditorAddr,
				Pubkey:  &auditorAccount.PublicKey,
			}
		}
		transferMethod = confidentialtransfers.TransferWithAuditorsMethod
	}

	transfer, err := cttypes.NewTransfer(
		privKey,
		fromRes.SeiAddress,
		toRes.SeiAddress,
		coin.Denom,
		senderAccount.DecryptableAvailableBalance,
		senderAccount.AvailableBalance,
		coin.Amount.Uint64(),
		&recipientAccount.PublicKey,
		auditors)

	if err != nil {
		return err
	}

	transferProto := cttypes.NewMsgTransferProto(transfer)
	fromAmountLo, _ := transferProto.FromAmountLo.Marshal()
	fromAmountHi, _ := transferProto.FromAmountHi.Marshal()
	toAmountLo, _ := transferProto.ToAmountLo.Marshal()
	toAmountHi, _ := transferProto.ToAmountHi.Marshal()
	remainingBalance, _ := transferProto.RemainingBalance.Marshal()
	proofs, _ := transferProto.Proofs.Marshal()
	dat, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}

	newAbi, err := abi.JSON(bytes.NewReader(dat))
	if err != nil {
		return err
	}

	inputArgs := []interface{}{
		fromEvmAddress,
		toEvmAddress,
		fromEvmAddress,
		toEvmAddress,
		coin.Denom,
		fromAmountLo,
		fromAmountHi,
		toAmountLo,
		toAmountHi,
		remainingBalance,
		transferProto.DecryptableBalance,
		proofs,
	}

	if auditors != nil && len(auditors) > 0 {
		var evmAuditors []cttypes.EvmAuditor
		auditorsProto := transferProto.Auditors
		for _, auditorProto := range auditorsProto {
			encryptedTransferAmountLo, _ := auditorProto.EncryptedTransferAmountLo.Marshal()
			encryptedTransferAmountHi, _ := auditorProto.EncryptedTransferAmountHi.Marshal()
			transferAmountLoValidityProof, _ := auditorProto.TransferAmountLoValidityProof.Marshal()
			transferAmountHiValidityProof, _ := auditorProto.TransferAmountHiValidityProof.Marshal()
			transferAmountLoEqualityProof, _ := auditorProto.TransferAmountLoEqualityProof.Marshal()
			transferAmountHiEqualityProof, _ := auditorProto.TransferAmountHiEqualityProof.Marshal()
			evmAddressRequest := &types.QueryEVMAddressBySeiAddressRequest{SeiAddress: auditorProto.AuditorAddress}
			evmAddress, err := queryClient.EVMAddressBySeiAddress(context.Background(), evmAddressRequest)
			if err != nil {
				return err
			}
			if !evmAddress.Associated {
				return errors.New("auditor address not associated with an EVM address")
			}
			evmAuditor := cttypes.EvmAuditor{
				AuditorAddress:                common.HexToAddress(evmAddress.EvmAddress),
				EncryptedTransferAmountLo:     encryptedTransferAmountLo,
				EncryptedTransferAmountHi:     encryptedTransferAmountHi,
				TransferAmountLoValidityProof: transferAmountLoValidityProof,
				TransferAmountHiValidityProof: transferAmountHiValidityProof,
				TransferAmountLoEqualityProof: transferAmountLoEqualityProof,
				TransferAmountHiEqualityProof: transferAmountHiEqualityProof,
			}
			evmAuditors = append(evmAuditors, evmAuditor)
		}
		inputArgs = append(inputArgs, evmAuditors)
	}

	bz, err := newAbi.Pack(
		transferMethod,
		inputArgs...)
	if err != nil {
		return err
	}
	return queryClientCtx.PrintString(hex.EncodeToString(bz))
}
