package cli

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

const (
	FlagGasFeeCap  = "gas-fee-cap"
	FlagGas        = "gas-limit"
	FlagEVMChainID = "evm-chain-id"
	FlagRPC        = "evm-rpc"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdAssociateAddress())
	cmd.AddCommand(CmdSend())
	cmd.AddCommand(CmdDeployErc20())

	return cmd
}

func CmdAssociateAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "associate-address [optional priv key hex] --rpc=<url> --from=<sender>",
		Short: "associate EVM and Sei address for the sender",
		Long:  "",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			var privHex string
			if len(args) == 1 {
				privHex = args[0]
			} else {
				txf := tx.NewFactoryCLI(clientCtx, cmd.Flags())
				kb := txf.Keybase()
				info, err := kb.Key(clientCtx.GetFromName())
				if err != nil {
					return err
				}
				localInfo, ok := info.(keyring.LocalInfo)
				if !ok {
					return errors.New("can only associate address for local keys")
				}
				priv, err := legacy.PrivKeyFromBytes([]byte(localInfo.PrivKeyArmor))
				if err != nil {
					return err
				}
				privHex = hex.EncodeToString(priv.Bytes())
			}

			emptyHash := common.Hash{}
			key, _ := crypto.HexToECDSA(privHex)
			sig, err := crypto.Sign(emptyHash[:], key)
			if err != nil {
				return err
			}
			R, S, _, err := ethtx.DecodeSignature(sig)
			if err != nil {
				return err
			}
			V := big.NewInt(int64(sig[64]))
			txData := ethtx.AssociateTx{V: V.Bytes(), R: R.Bytes(), S: S.Bytes()}
			bz, err := txData.Marshal()
			if err != nil {
				return err
			}
			payload := "0x" + hex.EncodeToString(bz)
			body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_sendRawTransaction\",\"params\":[\"%s\"],\"id\":\"associate_addr\"}", payload)
			rpc, err := cmd.Flags().GetString(FlagRPC)
			if err != nil {
				return err
			}
			req, err := http.NewRequest(http.MethodGet, rpc, strings.NewReader(body))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer res.Body.Close()
			resBody, err := io.ReadAll(res.Body)
			if err != nil {
				return err
			}
			fmt.Printf("Response: %s\n", string(resBody))

			return nil
		},
	}

	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func CmdSend() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send [to EVM address] [amount in wei] --from=<sender> --gas-fee-cap=<cap> --gas-limit=<limit> --evm-chain-id=<chain-id> --evm-rpc=<url>",
		Short: "send usei to EVM address",
		Long:  "",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			txf := tx.NewFactoryCLI(clientCtx, cmd.Flags())
			kb := txf.Keybase()
			info, err := kb.Key(clientCtx.GetFromName())
			if err != nil {
				return err
			}
			localInfo, ok := info.(keyring.LocalInfo)
			if !ok {
				return errors.New("can only associate address for local keys")
			}
			priv, err := legacy.PrivKeyFromBytes([]byte(localInfo.PrivKeyArmor))
			if err != nil {
				return err
			}
			privHex := hex.EncodeToString(priv.Bytes())
			key, _ := crypto.HexToECDSA(privHex)

			rpc, err := cmd.Flags().GetString(FlagRPC)
			if err != nil {
				return err
			}
			nonceQuery := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"%s\",\"latest\"],\"id\":\"send-cli\"}", crypto.PubkeyToAddress(key.PublicKey).Hex())
			req, err := http.NewRequest(http.MethodGet, rpc, strings.NewReader(nonceQuery))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer res.Body.Close()
			resBody, err := io.ReadAll(res.Body)
			if err != nil {
				return err
			}
			resObj := map[string]interface{}{}
			if err := json.Unmarshal(resBody, &resObj); err != nil {
				return err
			}
			nonce := new(hexutil.Uint64)
			if err := nonce.UnmarshalText([]byte(resObj["result"].(string))); err != nil {
				return err
			}

			to := common.HexToAddress(args[0])
			val, success := new(big.Int).SetString(args[1], 10)
			if !success {
				return fmt.Errorf("%s is an invalid amount to send", args[1])
			}
			gasFeeCap, err := cmd.Flags().GetUint64(FlagGasFeeCap)
			if err != nil {
				return err
			}
			gasLimit, err := cmd.Flags().GetUint64(FlagGas)
			if err != nil {
				return err
			}
			chainID, err := cmd.Flags().GetUint64(FlagEVMChainID)
			if err != nil {
				return err
			}
			txData := ethtypes.DynamicFeeTx{
				Nonce:     uint64(*nonce),
				GasFeeCap: new(big.Int).SetUint64(gasFeeCap),
				Gas:       gasLimit,
				To:        &to,
				Value:     val,
				Data:      []byte(""),
				ChainID:   new(big.Int).SetUint64(chainID),
			}
			ethCfg := types.DefaultChainConfig().EthereumConfig(txData.ChainID)
			signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), 1)
			tx := ethtypes.NewTx(&txData)
			tx, err = ethtypes.SignTx(tx, signer, key)
			if err != nil {
				return err
			}
			bz, err := tx.MarshalBinary()
			if err != nil {
				return err
			}
			payload := "0x" + hex.EncodeToString(bz)

			body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_sendRawTransaction\",\"params\":[\"%s\"],\"id\":\"send\"}", payload)
			req, err = http.NewRequest(http.MethodGet, rpc, strings.NewReader(body))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/json")
			res, err = http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer res.Body.Close()
			resBody, err = io.ReadAll(res.Body)
			if err != nil {
				return err
			}
			fmt.Printf("Response: %s\n", string(resBody))

			return nil
		},
	}

	cmd.Flags().Uint64(FlagGasFeeCap, 1000000000000, "Gas fee cap for the transaction")
	cmd.Flags().Uint64(FlagGas, 21000, "Gas limit for the transaction")
	cmd.Flags().Uint64(FlagEVMChainID, 713715, "EVM chain ID")
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

type Response struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      string `json:"id"`
	Result  string `json:"result"`
}

func CmdDeployErc20() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-erc20 [denom] [nonce] --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-chain-id=<chain-id> --evm-rpc=<url>",
		Short: "Deploy ERC20 contract for a native Sei token",
		Long:  "",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			err = sdk.ValidateDenom(args[0])
			if err != nil {
				return err
			}
			denom := args[0]

			nonce, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return err
			}
			gasFeeCap, err := cmd.Flags().GetUint64(FlagGasFeeCap)
			if err != nil {
				return err
			}
			gasLimit, err := cmd.Flags().GetUint64(FlagGas)
			if err != nil {
				return err
			}
			chainID, err := cmd.Flags().GetUint64(FlagEVMChainID)
			if err != nil {
				return err
			}
			bytecode := artifacts.GetNativeSeiTokensERC20Bin()
			abi := artifacts.GetNativeSeiTokensERC20ABI()
			parsedABI, err := ethabi.JSON(strings.NewReader(string(abi)))
			if err != nil {
				fmt.Println("failed at parsing abi")
				return err
			}
			constructorArguments := []interface{}{
				denom,
			}

			packedArgs, err := parsedABI.Pack("", constructorArguments...)
			if err != nil {
				return err
			}
			contractData := append(bytecode, packedArgs...)

			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			txf := tx.NewFactoryCLI(clientCtx, cmd.Flags())
			kb := txf.Keybase()
			info, err := kb.Key(clientCtx.GetFromName())
			if err != nil {
				return err
			}
			localInfo, ok := info.(keyring.LocalInfo)
			if !ok {
				return errors.New("can only associate address for local keys")
			}
			priv, err := legacy.PrivKeyFromBytes([]byte(localInfo.PrivKeyArmor))
			if err != nil {
				return err
			}
			privHex := hex.EncodeToString(priv.Bytes())
			key, _ := crypto.HexToECDSA(privHex)

			txData := ethtypes.DynamicFeeTx{
				Nonce:     nonce,
				GasFeeCap: new(big.Int).SetUint64(gasFeeCap),
				Gas:       gasLimit,
				Value:     big.NewInt(0),
				Data:      contractData,
				ChainID:   new(big.Int).SetUint64(chainID),
			}
			ethCfg := types.DefaultChainConfig().EthereumConfig(txData.ChainID)
			signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), 1)
			tx := ethtypes.NewTx(&txData)
			tx, err = ethtypes.SignTx(tx, signer, key)
			if err != nil {
				return err
			}
			bz, err := tx.MarshalBinary()
			if err != nil {
				return err
			}
			payload := "0x" + hex.EncodeToString(bz)

			body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_sendRawTransaction\",\"params\":[\"%s\"],\"id\":\"deploy-erc20\"}", payload)
			rpc, err := cmd.Flags().GetString(FlagRPC)
			if err != nil {
				return err
			}
			req, err := http.NewRequest(http.MethodGet, rpc, strings.NewReader(body))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer res.Body.Close()

			resBody, err := io.ReadAll(res.Body)
			if err != nil {
				return err
			}
			var resp Response
			err = json.Unmarshal(resBody, &resp)
			if err != nil {
				return err
			}

			senderAddr := crypto.PubkeyToAddress(key.PublicKey)
			data, err := rlp.EncodeToBytes([]interface{}{senderAddr, nonce})
			if err != nil {
				return err
			}
			hash := crypto.Keccak256Hash(data)
			contractAddress := hash.Bytes()[12:]
			contractAddressHex := hex.EncodeToString(contractAddress)

			fmt.Println("Deployer:", senderAddr)
			fmt.Println("Deployed to:", fmt.Sprintf("0x%s", contractAddressHex))
			fmt.Println("Transaction hash:", resp.Result)
			return nil
		},
	}

	cmd.Flags().Uint64(FlagGasFeeCap, 1000000000000, "Gas fee cap for the transaction")
	cmd.Flags().Uint64(FlagGas, 21000, "Gas limit for the transaction")
	cmd.Flags().Uint64(FlagEVMChainID, 713715, "EVM chain ID")
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
