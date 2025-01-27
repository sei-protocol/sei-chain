package cli

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gogo/protobuf/proto"
	pre "github.com/sei-protocol/sei-chain/precompiles/common"
	ctpre "github.com/sei-protocol/sei-chain/precompiles/confidentialtransfers"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	ethtxtypes "github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/spf13/cobra"
	tmtypes "github.com/tendermint/tendermint/abci/types"
)

const (
	decryptAvailableBalanceFlag = "decrypt-available-balance"
	decryptorFlag               = "decryptor"
	flagRPC                     = "evm-rpc"
)

// GetQueryCmd returns the cli query commands for the minting module.
func GetQueryCmd() *cobra.Command {
	confidentialTransfersQueryCmd := &cobra.Command{
		Use:                        types.ShortModuleName,
		Short:                      "Querying commands for the confidential transfer module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	confidentialTransfersQueryCmd.AddCommand(
		GetCmdQueryAccount(),
		GetCmdQueryAllAccount(),
		GetCmdQueryTx(),
	)

	return confidentialTransfersQueryCmd
}

// GetCmdQueryAccount implements a command to return an account asssociated with the address and denom
func GetCmdQueryAccount() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account [denom] [address] [flags]",
		Short: "Query the account state",
		Long: "Queries the account state associated with the address and denom." +
			"Pass the --decryptor flag to decrypt the account" +
			"Pass the --decrypt-available-balance flag to attempt to decrypt the available balance.",
		Args: cobra.ExactArgs(2),
		RunE: queryAccount,
	}

	flags.AddQueryFlagsToCmd(cmd)
	cmd.Flags().String(decryptorFlag, "", "Name or address of private key to decrypt the account")
	cmd.Flags().Bool(decryptAvailableBalanceFlag, false, "Set this to attempt to decrypt the available balance")
	return cmd
}

func queryAccount(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}
	queryClient := types.NewQueryClient(clientCtx)

	denom := args[0]

	// Validate denom
	err = sdk.ValidateDenom(denom)
	if err != nil {
		return fmt.Errorf("invalid denom: %v", err)
	}

	address := args[1]
	// Validate address
	_, err = sdk.AccAddressFromBech32(address)
	if err != nil {
		return fmt.Errorf("invalid address: %v", err)
	}

	decryptorAccount, err := cmd.Flags().GetString(decryptorFlag)
	if err != nil {
		return err
	}
	decryptorAddr, name, _, err := client.GetFromFields(clientCtx, clientCtx.Keyring, decryptorAccount)
	if err != nil {
		return err
	}

	res, err := queryClient.GetCtAccount(cmd.Context(), &types.GetCtAccountRequest{
		Address: address,
		Denom:   denom,
	})
	if err != nil {
		return err
	}

	// If the decryptor flag passed matches the queried address, attempt to decrypt the contents
	if decryptorAddr.String() == address {
		account, err := res.Account.FromProto()
		if err != nil {
			return err
		}
		privateKey, err := getPrivateKey(cmd, name)
		if err != nil {
			return err
		}

		aesKey, err := utils.GetAESKey(*privateKey, denom)
		if err != nil {
			return err
		}

		decryptor := elgamal.NewTwistedElgamal()
		keyPair, err := utils.GetElGamalKeyPair(*privateKey, denom)
		if err != nil {
			return err
		}

		decryptAvailableBalance, err := cmd.Flags().GetBool(decryptAvailableBalanceFlag)
		if err != nil {
			return err
		}

		if decryptAvailableBalance {
			err = clientCtx.PrintString(
				"--decrypt-available-balance set to true." +
					"This operation could take a long time and may not succeed even if the private key provided is valid\n")
			if err != nil {
				return err
			}
		}

		decryptedAccount, err := account.Decrypt(decryptor, keyPair, aesKey, decryptAvailableBalance)
		if err != nil {
			return err
		}

		return clientCtx.PrintProto(decryptedAccount)

	} else {
		return clientCtx.PrintProto(res.Account)
	}
}

// GetCmdQueryAccount implements a command to return an account asssociated with the address and denom
func GetCmdQueryAllAccount() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accounts [address]",
		Short: "Query all the confidential token accounts associated with the address",
		Args:  cobra.ExactArgs(1),
		RunE:  queryAllAccounts,
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func queryAllAccounts(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}
	queryClient := types.NewQueryClient(clientCtx)

	address := args[0]
	// Validate address
	_, err = sdk.AccAddressFromBech32(address)
	if err != nil {
		return fmt.Errorf("invalid address: %v", err)
	}

	res, err := queryClient.GetAllCtAccounts(cmd.Context(), &types.GetAllCtAccountsRequest{
		Address: args[0],
	})

	if err != nil {
		return err
	}

	for i := range res.Accounts {
		err = clientCtx.PrintString("\n")
		if err != nil {
			return err
		}

		err = clientCtx.PrintProto(&res.Accounts[i])
		if err != nil {
			return err
		}
	}

	return nil
}

// GetCmdQueryTx implements a command to query a tx by it's transaction hash and return it's decrypted state by decrypting with the senders private key.
func GetCmdQueryTx() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tx [hash] --decryptor [decryptor] [flags]",
		Short: "Query the confidential transaction and decrypts it",
		Long: "Query the confidential transaction by it's tx hash and decrypts it using the private key of the account in --decryptor. For decryption to work, the decryptor should be of a sender, receiver or auditor." +
			"Pass the --decrypt-available-balance flag to attempt to decrypt the available balance. (This is an expensive operation and may not succeed even if the private key provided is valid)",
		Args: cobra.ExactArgs(1),
		RunE: queryDecryptedTx,
	}

	flags.AddQueryFlagsToCmd(cmd)
	cmd.Flags().String(decryptorFlag, "", "Name or address of private key to decrypt the account")
	cmd.Flags().Bool(decryptAvailableBalanceFlag, false, "Set this to attempt to decrypt the available balance")
	cmd.Flags().String(flagRPC, "", "EVM RPC endpoint e.g. http://localhost:8545")
	return cmd
}

func queryDecryptedTx(cmd *cobra.Command, args []string) error {
	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return err
	}

	// Get the transaction hash
	txHashHex := args[0]

	evmRpc, err := cmd.Flags().GetString(flagRPC)
	if err != nil {
		return err
	}

	if evmRpc != "" {
		txHashHex, err = getTxHashByEvmHash(evmRpc, txHashHex)
		if err != nil {
			return err
		}
	}

	decryptorAccount, err := cmd.Flags().GetString(decryptorFlag)
	if err != nil {
		return err
	}

	if decryptorAccount == "" {
		return fmt.Errorf("--decryptor flag must be set since we need the private key to decrypt the transaction")
	}

	fromAddr, name, _, err := client.GetFromFields(clientCtx, clientCtx.Keyring, decryptorAccount)
	if err != nil {
		return err
	}
	clientCtx = clientCtx.WithFrom(decryptorAccount).WithFromAddress(fromAddr).WithFromName(name)

	decryptAvailableBalance, err := cmd.Flags().GetBool(decryptAvailableBalanceFlag)
	if err != nil {
		return err
	}
	if decryptAvailableBalance {
		err = clientCtx.PrintString("--decrypt-available-balance set to true. This operation could take a long time and may not succeed even if the private key provided is valid\n")
		if err != nil {
			return err
		}
	}

	txResponse, err := authtx.QueryTx(clientCtx, txHashHex)
	if err != nil {
		return err
	}
	if txResponse.Tx == nil {
		return fmt.Errorf("transaction not found")
	}

	// Decode the transaction
	var rawTx tx.Tx
	if err := clientCtx.Codec.Unmarshal(txResponse.Tx.Value, &rawTx); err != nil {
		return fmt.Errorf("failed to unmarshal transaction: %w", err)
	}

	decryptor := elgamal.NewTwistedElgamal()
	privateKey, err := getPrivateKey(cmd, name)

	if err != nil {
		return err
	}
	msgPrinted := false
	for _, msg := range rawTx.Body.Messages {
		result, foundMsg, err := handleDecryptableMessage(clientCtx.Codec, msg, txResponse.Events, decryptor, privateKey, decryptAvailableBalance, fromAddr.String(), evmRpc)
		if !foundMsg {
			continue
		} else {
			if err != nil {
				return err
			}
			err = clientCtx.PrintProto(result)
			msgPrinted = true
			if err != nil {
				return err
			}
		}
	}

	if !msgPrinted {
		return fmt.Errorf("no decryptable message found in the transaction")
	}

	return nil
}

// Helper function to unmarshal a message and run its Decrypt() method
func handleDecryptableMessage(
	cdc codec.Codec,
	msgAny *cdctypes.Any,
	events []tmtypes.Event,
	decryptor *elgamal.TwistedElGamal,
	privKey *ecdsa.PrivateKey,
	decryptAvailableBalance bool,
	address string,
	evmRpc string) (msg proto.Message, foundDecryptableMsg bool, error error) {
	// Try to unmarshal the message as one of the known types
	var sdkmsg sdk.Msg
	err := cdc.UnpackAny(msgAny, &sdkmsg)
	if err != nil {
		return nil, false, nil
	}

	// If the message is of MsgEVMTransaction type, convert it to a corresponding confidential transfer message
	// e.g. MsgTransfer
	if isEvmMsg(sdkmsg) {
		sdkmsg, err = convertEvmMsgToCtMsg(sdkmsg, events, evmRpc)
		if err != nil {
			return nil, false, err
		}
	}

	var result proto.Message
	switch message := sdkmsg.(type) {
	case *types.MsgInitializeAccount:
		result, err = message.Decrypt(decryptor, *privKey, decryptAvailableBalance)
	case *types.MsgWithdraw:
		result, err = message.Decrypt(decryptor, *privKey, decryptAvailableBalance)
	case *types.MsgApplyPendingBalance:
		result, err = message.Decrypt(decryptor, *privKey, decryptAvailableBalance)
	case *types.MsgTransfer:
		result, err = message.Decrypt(decryptor, *privKey, decryptAvailableBalance, address)
	case *types.MsgDeposit:
		result = message
	case *types.MsgCloseAccount:
		result = message
	default:
		return nil, false, nil
	}

	return result, true, err
}

type RpcResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Result  string `json:"result"`
}

func getTxHashByEvmHash(evmRpc string, ethHash string) (string, error) {
	body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"sei_getCosmosTx\",\"params\":[\"%s\"],\"id\":\"cosmos_tx\"}", ethHash)
	return executeRpcCall(evmRpc, body)
}

func getSeiAddress(evmRpc string, evmAddress string) (string, error) {
	body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"sei_getSeiAddress\",\"params\":[\"%s\"],\"id\":\"1\"}", evmAddress)
	return executeRpcCall(evmRpc, body)
}

func executeRpcCall(evmRpc string, requestBody string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, evmRpc, strings.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	var response RpcResponse
	err = json.Unmarshal(resBody, &response)
	if err != nil {
		return "", err
	}
	return response.Result, nil
}

func isEvmMsg(msg sdk.Msg) bool {
	return reflect.TypeOf(msg) == reflect.TypeOf(&evmtypes.MsgEVMTransaction{})
}

func convertEvmMsgToCtMsg(sdkmsg sdk.Msg, events []tmtypes.Event, evmRpc string) (sdk.Msg, error) {
	message := sdkmsg.(*evmtypes.MsgEVMTransaction)
	var dyanmicFeeTx ethtxtypes.DynamicFeeTx
	data := message.GetData()
	err := proto.Unmarshal(data.Value, &dyanmicFeeTx)
	if err != nil {
		return nil, err
	}

	methodID, err := pre.ExtractMethodID(dyanmicFeeTx.Data)
	if err != nil {
		return nil, err
	}
	ctPrecompile, _ := ctpre.NewPrecompile(nil, nil, nil)
	method, err := ctPrecompile.ABI.MethodById(methodID)
	if err != nil {
		return nil, err
	}
	// In Ethereum transactions, the first 4 bytes of the Data field typically represent the method ID
	argsBz := dyanmicFeeTx.Data[4:]
	args, err := method.Inputs.Unpack(argsBz)
	if err != nil {
		return nil, err
	}

	getAddressFromEvent := func(eventType, attrKey string) (string, error) {
		for _, event := range events {
			if event.Type == eventType {
				for _, attr := range event.Attributes {
					if string(attr.Key) == attrKey {
						return string(attr.Value), nil
					}
				}
			}
		}
		return "", fmt.Errorf("address not found for event type %s", eventType)
	}

	switch method.Name {
	case ctpre.ApplyPendingBalanceMethod:
		address, err := getAddressFromEvent(types.EventTypeApplyPendingBalance, types.AttributeAddress)
		if err != nil {
			return nil, err
		}
		msg, err := ctpre.BuildApplyPendingBalanceMsgFromArgs(address, args)
		if err != nil {
			return nil, err
		}
		return msg, nil
	case ctpre.DepositMethod:
		address, err := getAddressFromEvent(types.EventTypeDeposit, types.AttributeAddress)
		if err != nil {
			return nil, err
		}
		msg, err := ctpre.BuildDepositMsgFromArgs(address, args)
		if err != nil {
			return nil, err
		}
		return msg, nil
	case ctpre.InitializeAccountMethod:
		address, err := getAddressFromEvent(types.EventTypeInitializeAccount, types.AttributeAddress)
		if err != nil {
			return nil, err
		}
		msg, err := ctpre.BuildInitializeAccountMsgFromArgs(address, args)
		if err != nil {
			return nil, err
		}
		return msg, nil
	case ctpre.TransferMethod:
		fromAddress, err := getAddressFromEvent(types.TypeMsgTransfer, types.AttributeSender)
		if err != nil {
			return nil, err
		}
		toAddress, err := getAddressFromEvent(types.TypeMsgTransfer, types.AttributeRecipient)
		if err != nil {
			return nil, err
		}
		msg, err := ctpre.BuildTransferMsgFromArgs(fromAddress, toAddress, args)
		if err != nil {
			return nil, err
		}
		return msg, nil
	case ctpre.TransferWithAuditorsMethod:
		fromAddress, err := getAddressFromEvent(types.EventTypeTransfer, types.AttributeSender)
		if err != nil {
			return nil, err
		}
		toAddress, err := getAddressFromEvent(types.EventTypeTransfer, types.AttributeRecipient)
		if err != nil {
			return nil, err
		}
		msg, err := ctpre.BuildTransferMsgFromArgs(fromAddress, toAddress, args)
		if err != nil {
			return nil, err
		}

		auditors, err := getAuditorsFromArg(evmRpc, args[9])
		if err != nil {
			return nil, err
		}
		msg.Auditors = auditors
		return msg, nil
	case ctpre.WithdrawMethod:
		address, err := getAddressFromEvent(types.EventTypeWithdraw, types.AttributeAddress)
		if err != nil {
			return nil, err
		}
		msg, err := ctpre.BuildWithdrawMsgFromArgs(address, args)
		if err != nil {
			return nil, err
		}
		return msg, nil
	case ctpre.CloseAccountMethod:
		address, err := getAddressFromEvent(types.EventTypeCloseAccount, types.AttributeAddress)
		if err != nil {
			return nil, err
		}
		msg, err := ctpre.BuildCloseAccountMsgFromArgs(address, args)
		if err != nil {
			return nil, err
		}
		return msg, nil
	default:
		return nil, fmt.Errorf("unknown method %s", method.Name)
	}
}

func getAuditorsFromArg(evmRpc string, arg interface{}) ([]*types.Auditor, error) {
	ctAuditors, err := ctpre.GetCtAuditors(arg)
	if err != nil {
		return nil, err
	}

	if len(ctAuditors) == 0 {
		return nil, errors.New("auditors array cannot be empty")
	}

	auditors := make([]*types.Auditor, 0)
	for _, auditor := range ctAuditors {
		auditorAddr, err := getValidSeiAddressFromString(evmRpc, auditor.AuditorAddress)
		if err != nil {
			return nil, err
		}

		a, err := ctpre.GetAuditorFromCtAuditor(auditorAddr, auditor)
		if err != nil {
			return nil, err
		}
		auditors = append(auditors, a)
	}
	return auditors, nil
}

func getValidSeiAddressFromString(evmRpc string, addr string) (string, error) {
	if common.IsHexAddress(addr) {
		evmAddr := common.HexToAddress(addr)
		res, err := getSeiAddress(evmRpc, evmAddr.String())
		if err != nil {
			return "", err
		}
		return res, nil
	}
	if seiAddress, err := sdk.AccAddressFromBech32(addr); err != nil {
		return "", fmt.Errorf("invalid address %s: %w", addr, err)
	} else {
		return seiAddress.String(), nil
	}
}
