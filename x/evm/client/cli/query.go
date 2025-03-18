package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"math/big"
	"os"
	"strings"

	"github.com/sei-protocol/sei-chain/precompiles/confidentialtransfers"
	ctcliutils "github.com/sei-protocol/sei-chain/x/confidentialtransfers/client/cli"
	cttypes "github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/lib/ethapi"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/sei-protocol/sei-chain/evmrpc"
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
	cmd.AddCommand(GetCmdQueryCtInitAccountPayload())
	cmd.AddCommand(GetCmdQueryCtApplyPendingBalancePayload())
	cmd.AddCommand(GetCmdQueryCtWithdrawPayload())
	cmd.AddCommand(GetCmdQueryCtCloseAccountPayload())
	cmd.AddCommand(CmdQueryTxByHash())

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
		Use:   "ct-transfer-payload [abi-filepath] [from_address] [to_address] [amount] [flags]",
		Short: "get hex payload for the confidential transfer",
		Args:  cobra.ExactArgs(4),
		RunE:  queryCtTransferPayload,
	}

	flags.AddQueryFlagsToCmd(cmd)
	cmd.Flags().StringSlice(auditorsFlag, []string{}, "List of auditor addresses")

	return cmd
}

func queryCtTransferPayload(cmd *cobra.Command, args []string) error {
	queryClientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}

	queryClient := types.NewQueryClient(queryClientCtx)
	ctQueryClient := cttypes.NewQueryClient(queryClientCtx)

	fromAddress := args[1]
	if fromAddress == "" {
		return errors.New("from address cannot be empty")
	}

	fromSeiAddress, err := getSeiAddress(queryClient, fromAddress)
	if err != nil {
		return err
	}

	_, name, _, err := client.GetFromFields(queryClientCtx, queryClientCtx.Keyring, fromSeiAddress)
	if err != nil {
		return err
	}
	privKey, err := getPrivateKeyForName(cmd, name)
	if err != nil {
		return err
	}

	toAddress := args[2]
	if toAddress == "" {
		return errors.New("to address cannot be empty")
	}

	toSeiAddress, err := getSeiAddress(queryClient, toAddress)
	if err != nil {
		return err
	}

	coin, err := sdk.ParseCoinNormalized(args[3])
	if err != nil {
		return err
	}

	senderAccount, err := ctcliutils.GetAccount(ctQueryClient, fromSeiAddress, coin.Denom)
	if err != nil {
		return err
	}

	recipientAccount, err := ctcliutils.GetAccount(ctQueryClient, toSeiAddress, coin.Denom)
	if err != nil {
		return err
	}

	auditorAddrs, err := cmd.Flags().GetStringSlice(auditorsFlag)
	if err != nil {
		return err
	}

	var auditors []cttypes.AuditorInput
	transferMethod := confidentialtransfers.TransferMethod

	if len(auditorAddrs) > 0 {
		auditors = make([]cttypes.AuditorInput, len(auditorAddrs))
		for i, auditorAddr := range auditorAddrs {
			auditorSeiAddr, err := getSeiAddress(queryClient, auditorAddr)
			if err != nil {
				return err
			}
			auditorAccount, err := ctcliutils.GetAccount(ctQueryClient, auditorSeiAddr, coin.Denom)
			if err != nil {
				return err
			}
			auditors[i] = cttypes.AuditorInput{
				Address: auditorSeiAddr,
				Pubkey:  &auditorAccount.PublicKey,
			}
		}
		transferMethod = confidentialtransfers.TransferWithAuditorsMethod
	}

	transfer, err := cttypes.NewTransfer(
		privKey,
		fromSeiAddress,
		toSeiAddress,
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

	if err = transferProto.ValidateBasic(); err != nil {
		return err
	}

	fromAmountLo, err := transferProto.FromAmountLo.Marshal()
	if err != nil {
		return err
	}

	fromAmountHi, err := transferProto.FromAmountHi.Marshal()
	if err != nil {
		return err
	}

	toAmountLo, err := transferProto.ToAmountLo.Marshal()
	if err != nil {
		return err
	}

	toAmountHi, err := transferProto.ToAmountHi.Marshal()
	if err != nil {
		return err
	}

	remainingBalance, err := transferProto.RemainingBalance.Marshal()
	if err != nil {
		return err
	}

	proofs, err := transferProto.Proofs.Marshal()
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

	inputArgs := []interface{}{
		toSeiAddress,
		coin.Denom,
		fromAmountLo,
		fromAmountHi,
		toAmountLo,
		toAmountHi,
		remainingBalance,
		transferProto.DecryptableBalance,
		proofs,
	}

	if len(auditors) > 0 {
		var ctAuditors []cttypes.CtAuditor
		auditorsProto := transferProto.Auditors
		for _, auditorProto := range auditorsProto {
			encryptedTransferAmountLo, err := auditorProto.EncryptedTransferAmountLo.Marshal()
			if err != nil {
				return err
			}

			encryptedTransferAmountHi, err := auditorProto.EncryptedTransferAmountHi.Marshal()
			if err != nil {
				return err
			}

			transferAmountLoValidityProof, err := auditorProto.TransferAmountLoValidityProof.Marshal()
			if err != nil {
				return err
			}

			transferAmountHiValidityProof, err := auditorProto.TransferAmountHiValidityProof.Marshal()
			if err != nil {
				return err
			}

			transferAmountLoEqualityProof, err := auditorProto.TransferAmountLoEqualityProof.Marshal()
			if err != nil {
				return err
			}

			transferAmountHiEqualityProof, err := auditorProto.TransferAmountHiEqualityProof.Marshal()
			if err != nil {
				return err
			}

			evmAuditor := cttypes.CtAuditor{
				AuditorAddress:                auditorProto.AuditorAddress,
				EncryptedTransferAmountLo:     encryptedTransferAmountLo,
				EncryptedTransferAmountHi:     encryptedTransferAmountHi,
				TransferAmountLoValidityProof: transferAmountLoValidityProof,
				TransferAmountHiValidityProof: transferAmountHiValidityProof,
				TransferAmountLoEqualityProof: transferAmountLoEqualityProof,
				TransferAmountHiEqualityProof: transferAmountHiEqualityProof,
			}
			ctAuditors = append(ctAuditors, evmAuditor)

		}
		inputArgs = append(inputArgs, ctAuditors)
	}

	bz, err := newAbi.Pack(
		transferMethod,
		inputArgs...)
	if err != nil {
		return err
	}
	return queryClientCtx.PrintString(hex.EncodeToString(bz))
}

func GetCmdQueryCtInitAccountPayload() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ct-init-account-payload [abi-filepath] [from_address] [denom]",
		Short: "get hex payload for the confidential transfers account initialization",
		Args:  cobra.ExactArgs(3),
		RunE:  queryCtInitAccountPayload,
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func queryCtInitAccountPayload(cmd *cobra.Command, args []string) error {
	queryClientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}
	queryClient := types.NewQueryClient(queryClientCtx)

	dat, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}

	newAbi, err := abi.JSON(bytes.NewReader(dat))
	if err != nil {
		return err
	}

	fromAddress := args[1]
	if fromAddress == "" {
		return errors.New("from address cannot be empty")
	}

	seiAddress, err := getSeiAddress(queryClient, fromAddress)
	if err != nil {
		return err
	}

	denom := args[2]
	if denom == "" {
		return errors.New("denom cannot be empty")
	}

	_, name, _, err := client.GetFromFields(queryClientCtx, queryClientCtx.Keyring, seiAddress)
	if err != nil {
		return err
	}

	privKey, err := getPrivateKeyForName(cmd, name)
	if err != nil {
		return err
	}

	initAccount, err := cttypes.NewInitializeAccount(fromAddress, denom, *privKey)
	if err != nil {
		return err
	}

	initAccountProto := cttypes.NewMsgInitializeAccountProto(initAccount)

	if err = initAccountProto.ValidateBasic(); err != nil {
		return err
	}

	pendingBalanceLo, err := initAccountProto.PendingBalanceLo.Marshal()
	if err != nil {
		return err
	}
	pendingBalanceHi, err := initAccountProto.PendingBalanceHi.Marshal()
	if err != nil {
		return err
	}
	availableBalance, err := initAccountProto.AvailableBalance.Marshal()
	if err != nil {
		return err
	}
	proofs, err := initAccountProto.Proofs.Marshal()
	if err != nil {
		return err
	}

	bz, err := newAbi.Pack(
		confidentialtransfers.InitializeAccountMethod,
		initAccountProto.FromAddress,
		initAccountProto.Denom,
		initAccountProto.PublicKey,
		initAccountProto.DecryptableBalance,
		pendingBalanceLo,
		pendingBalanceHi,
		availableBalance,
		proofs)

	if err != nil {
		return err
	}
	return queryClientCtx.PrintString(hex.EncodeToString(bz))
}

func GetCmdQueryCtApplyPendingBalancePayload() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ct-apply-pending-balance-payload [abi-filepath] [from_address] [denom]",
		Short: "get hex payload for the confidential transfers apply pending balance method",
		Args:  cobra.ExactArgs(3),
		RunE:  queryCtApplyPendingBalancePayload,
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func queryCtApplyPendingBalancePayload(cmd *cobra.Command, args []string) error {
	queryClientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}
	queryClient := types.NewQueryClient(queryClientCtx)

	dat, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}

	newAbi, err := abi.JSON(bytes.NewReader(dat))
	if err != nil {
		return err
	}

	fromAddress := args[1]
	if fromAddress == "" {
		return errors.New("from address cannot be empty")
	}

	seiAddress, err := getSeiAddress(queryClient, fromAddress)
	if err != nil {
		return err
	}

	denom := args[2]
	if denom == "" {
		return errors.New("denom cannot be empty")
	}

	_, name, _, err := client.GetFromFields(queryClientCtx, queryClientCtx.Keyring, seiAddress)
	if err != nil {
		return err
	}

	privKey, err := getPrivateKeyForName(cmd, name)
	if err != nil {
		return err
	}

	ctQueryClient := cttypes.NewQueryClient(queryClientCtx)
	fromAccount, err := ctcliutils.GetAccount(ctQueryClient, seiAddress, denom)
	if err != nil {
		return err
	}

	applyPendingBalance, err := cttypes.NewApplyPendingBalance(
		*privKey,
		seiAddress,
		denom,
		fromAccount.DecryptableAvailableBalance,
		fromAccount.PendingBalanceCreditCounter,
		fromAccount.AvailableBalance,
		fromAccount.PendingBalanceLo,
		fromAccount.PendingBalanceHi)
	if err != nil {
		return err
	}

	applyPendingBalanceProto := cttypes.NewMsgApplyPendingBalanceProto(applyPendingBalance)

	if err = applyPendingBalanceProto.ValidateBasic(); err != nil {
		return err
	}

	availableBalance, err := applyPendingBalanceProto.CurrentAvailableBalance.Marshal()
	if err != nil {
		return err
	}

	bz, err := newAbi.Pack(
		confidentialtransfers.ApplyPendingBalanceMethod,
		applyPendingBalanceProto.Denom,
		applyPendingBalanceProto.NewDecryptableAvailableBalance,
		applyPendingBalanceProto.CurrentPendingBalanceCounter,
		availableBalance)

	if err != nil {
		return err
	}
	return queryClientCtx.PrintString(hex.EncodeToString(bz))
}

func GetCmdQueryCtWithdrawPayload() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ct-withdraw-payload [abi-filepath] [from_address] [amount]",
		Short: "get hex payload for the confidential transfers withdraw method",
		Args:  cobra.ExactArgs(3),
		RunE:  queryCtWithdrawPayload,
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func queryCtWithdrawPayload(cmd *cobra.Command, args []string) error {
	queryClientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}
	queryClient := types.NewQueryClient(queryClientCtx)

	dat, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}

	newAbi, err := abi.JSON(bytes.NewReader(dat))
	if err != nil {
		return err
	}

	fromAddress := args[1]
	if fromAddress == "" {
		return errors.New("from address cannot be empty")
	}

	seiAddress, err := getSeiAddress(queryClient, fromAddress)
	if err != nil {
		return err
	}

	coin, err := sdk.ParseCoinNormalized(args[2])
	if err != nil {
		return err
	}

	_, name, _, err := client.GetFromFields(queryClientCtx, queryClientCtx.Keyring, seiAddress)
	if err != nil {
		return err
	}

	privKey, err := getPrivateKeyForName(cmd, name)
	if err != nil {
		return err
	}

	ctQueryClient := cttypes.NewQueryClient(queryClientCtx)
	fromAccount, err := ctcliutils.GetAccount(ctQueryClient, seiAddress, coin.Denom)
	if err != nil {
		return err
	}

	withdraw, err := cttypes.NewWithdraw(
		*privKey,
		fromAccount.AvailableBalance,
		coin.Denom,
		seiAddress,
		fromAccount.DecryptableAvailableBalance,
		coin.Amount.BigInt())

	if err != nil {
		return err
	}

	withdrawProto := cttypes.NewMsgWithdrawProto(withdraw)

	if err = withdrawProto.ValidateBasic(); err != nil {
		return err
	}

	remainingBalanceCommitment, err := withdrawProto.RemainingBalanceCommitment.Marshal()
	if err != nil {
		return err
	}

	proofs, err := withdrawProto.Proofs.Marshal()
	if err != nil {
		return err
	}

	bz, err := newAbi.Pack(
		confidentialtransfers.WithdrawMethod,
		withdrawProto.Denom,
		coin.Amount.BigInt(),
		withdrawProto.DecryptableBalance,
		remainingBalanceCommitment,
		proofs)

	if err != nil {
		return err
	}
	return queryClientCtx.PrintString(hex.EncodeToString(bz))
}

func GetCmdQueryCtCloseAccountPayload() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ct-close-account-payload [abi-filepath] [from_address] [denom]",
		Short: "get hex payload for the confidential transfers close account method",
		Args:  cobra.ExactArgs(3),
		RunE:  queryCtCloseAccountPayload,
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func queryCtCloseAccountPayload(cmd *cobra.Command, args []string) error {
	queryClientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}
	queryClient := types.NewQueryClient(queryClientCtx)

	dat, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}

	newAbi, err := abi.JSON(bytes.NewReader(dat))
	if err != nil {
		return err
	}

	fromAddress := args[1]
	if fromAddress == "" {
		return errors.New("from address cannot be empty")
	}

	seiAddress, err := getSeiAddress(queryClient, fromAddress)
	if err != nil {
		return err
	}

	denom := args[2]
	if denom == "" {
		return errors.New("denom cannot be empty")
	}

	_, name, _, err := client.GetFromFields(queryClientCtx, queryClientCtx.Keyring, seiAddress)
	if err != nil {
		return err
	}

	privKey, err := getPrivateKeyForName(cmd, name)
	if err != nil {
		return err
	}

	ctQueryClient := cttypes.NewQueryClient(queryClientCtx)
	fromAccount, err := ctcliutils.GetAccount(ctQueryClient, seiAddress, denom)
	if err != nil {
		return err
	}

	closeAccount, err := cttypes.NewCloseAccount(
		*privKey,
		seiAddress,
		denom,
		fromAccount.PendingBalanceLo,
		fromAccount.PendingBalanceHi,
		fromAccount.AvailableBalance)
	if err != nil {
		return err
	}

	closeAccountProto := cttypes.NewMsgCloseAccountProto(closeAccount)

	if err = closeAccountProto.ValidateBasic(); err != nil {
		return err
	}

	proofs, err := closeAccountProto.Proofs.Marshal()
	if err != nil {
		return err
	}

	bz, err := newAbi.Pack(
		confidentialtransfers.CloseAccountMethod,
		closeAccountProto.Denom,
		proofs)

	if err != nil {
		return err
	}
	return queryClientCtx.PrintString(hex.EncodeToString(bz))
}

func getSeiAddress(queryClient types.QueryClient, address string) (string, error) {
	if common.IsHexAddress(address) {
		evmAddr := common.HexToAddress(address)
		res, err := queryClient.SeiAddressByEVMAddress(context.Background(), &types.QuerySeiAddressByEVMAddressRequest{EvmAddress: evmAddr.String()})
		if err != nil {
			return "", err
		}
		if res.Associated {
			return res.SeiAddress, nil
		} else {
			return "", fmt.Errorf("address %s is not associated", evmAddr)
		}
	}
	if seiAddress, err := sdk.AccAddressFromBech32(address); err != nil {
		return "", fmt.Errorf("invalid address %s: %w", address, err)
	} else {
		return seiAddress.String(), nil
	}
}

func CmdQueryTxByHash() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tx [hash]",
		Short: "Query for a transaction by tx hash, same as eth_getTransactionByHash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hash := common.HexToHash(args[0])
			rpc, err := cmd.Flags().GetString(FlagRPC)
			if err != nil {
				return err
			}
			ethClient, err := ethclient.Dial(rpc)
			if err != nil {
				return err
			}
			var response *ethapi.RPCTransaction
			err = ethClient.Client().CallContext(context.Background(), &response, "eth_getTransactionByHash", hash)
			if err != nil {
				return err
			} else if response == nil {
				return ethereum.NotFound
			}
			result, err := json.MarshalIndent(response, "", "  ")
			fmt.Printf("%s\n", result)
			return err
		},
	}

	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")

	return cmd
}
