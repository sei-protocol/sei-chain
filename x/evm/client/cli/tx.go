package cli

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"

	"github.com/cosmos/cosmos-sdk/crypto/hd"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/precompiles"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/wsei"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

const (
	FlagGasFeeCap = "gas-fee-cap"
	FlagGas       = "gas-limit"
	FlagValue     = "value"
	FlagRPC       = "evm-rpc"
	FlagNonce     = "nonce"
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
	cmd.AddCommand(CmdDeployContract())
	cmd.AddCommand(CmdCallContract())
	cmd.AddCommand(CmdDeployWSEI())
	cmd.AddCommand(CmdERC20Send())
	cmd.AddCommand(CmdCallPrecompile())
	cmd.AddCommand(NativeSendTxCmd())
	cmd.AddCommand(RegisterCwPointerCmd())
	cmd.AddCommand(RegisterEvmPointerCmd())
	cmd.AddCommand(NewAddERCNativePointerProposalTxCmd())
	cmd.AddCommand(AssociateContractAddressCmd())
	cmd.AddCommand(NativeAssociateCmd())

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
				if localInfo.GetAlgo() != hd.Secp256k1Type {
					return errors.New("can only use addresses using secp256k1")
				}
				priv, err := legacy.PrivKeyFromBytes([]byte(localInfo.PrivKeyArmor))
				if err != nil {
					return err
				}
				privHex = hex.EncodeToString(priv.Bytes())
			}

			emptyHash := crypto.Keccak256Hash([]byte{})
			key, err := crypto.HexToECDSA(privHex)
			if err != nil {
				return err
			}
			sig, err := crypto.Sign(emptyHash[:], key)
			if err != nil {
				return err
			}
			R, S, _, err := ethtx.DecodeSignature(sig)
			if err != nil {
				return err
			}
			V := big.NewInt(int64(sig[64]))
			txData := evmrpc.AssociateRequest{V: hex.EncodeToString(V.Bytes()), R: hex.EncodeToString(R.Bytes()), S: hex.EncodeToString(S.Bytes())}
			bz, err := json.Marshal(txData)
			if err != nil {
				return err
			}
			body := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"sei_associate\",\"params\":[%s],\"id\":\"associate_addr\"}", string(bz))
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
		Use:   "send [to EVM address] [amount in wei] --from=<sender> --gas-fee-cap=<cap> --gas-limit=<limit> --evm-rpc=<url>",
		Short: "send usei to EVM address",
		Long:  "",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			key, err := getPrivateKey(cmd)
			if err != nil {
				return err
			}

			rpc, err := cmd.Flags().GetString(FlagRPC)
			if err != nil {
				return err
			}
			var nonce uint64
			if n, err := cmd.Flags().GetInt64(FlagNonce); err == nil && n >= 0 {
				nonce = uint64(n)
			} else {
				nonce, err = getNonce(rpc, key.PublicKey)
				if err != nil {
					return err
				}
			}

			to := common.HexToAddress(args[0])
			val, success := new(big.Int).SetString(args[1], 10)
			if !success {
				return fmt.Errorf("%s is an invalid amount to send", args[1])
			}
			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			txData.Nonce = nonce
			txData.Value = val
			txData.Data = []byte("")
			txData.To = &to
			resp, err := sendTx(txData, rpc, key)
			if err != nil {
				return err
			}

			fmt.Println("Transaction hash:", resp.Hex())

			return nil
		},
	}

	cmd.Flags().Uint64(FlagGasFeeCap, 1000000000000, "Gas fee cap for the transaction")
	cmd.Flags().Uint64(FlagGas, 21000, "Gas limit for the transaction")
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	cmd.Flags().Int64(FlagNonce, -1, "Nonce override for the transaction. Negative value means no override")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

type Response struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      string `json:"id"`
	Result  string `json:"result"`
}

func CmdDeployContract() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy [path to binary] --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-rpc=<url>",
		Short: "Deploy an EVM contract for binary at specified path",
		Long:  "",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			code, err := os.ReadFile(args[0])
			if err != nil {
				panic("failed to read contract binary")
			}
			bz, err := hex.DecodeString(string(code))
			if err != nil {
				panic("failed to decode contract binary")
			}

			key, err := getPrivateKey(cmd)
			if err != nil {
				return err
			}

			rpc, err := cmd.Flags().GetString(FlagRPC)
			if err != nil {
				return err
			}
			var nonce uint64
			if n, err := cmd.Flags().GetInt64(FlagNonce); err == nil && n >= 0 {
				nonce = uint64(n)
			} else {
				nonce, err = getNonce(rpc, key.PublicKey)
				if err != nil {
					return err
				}
			}

			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			txData.Nonce = nonce
			txData.Value = utils.Big0
			txData.Data = bz

			resp, err := sendTx(txData, rpc, key)
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
			fmt.Println("Transaction hash:", resp.Hex())
			return nil
		},
	}

	cmd.Flags().Uint64(FlagGasFeeCap, 1000000000000, "Gas fee cap for the transaction")
	cmd.Flags().Uint64(FlagGas, 5000000, "Gas limit for the transaction")
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	cmd.Flags().Int64(FlagNonce, -1, "Nonce override for the transaction. Negative value means no override")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func CmdCallContract() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "call-contract [addr] [payload hex] --value=<payment> --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-rpc=<url>",
		Short: "Call EVM contract with a bytes payload in hex",
		Long:  "",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			contract := common.HexToAddress(args[0])
			payload, err := hex.DecodeString(args[1])
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
			var nonce uint64
			if n, err := cmd.Flags().GetInt64(FlagNonce); err == nil && n >= 0 {
				nonce = uint64(n)
			} else {
				nonce, err = getNonce(rpc, key.PublicKey)
				if err != nil {
					return err
				}
			}

			value, err := cmd.Flags().GetString(FlagValue)
			if err != nil {
				return err
			}
			valueBig, success := new(big.Int).SetString(value, 10)
			if !success || valueBig.Cmp(utils.Big0) < 0 {
				return fmt.Errorf("%s is not a valid value. Must be a decimal nonnegative integer", value)
			}

			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			txData.Nonce = nonce
			txData.Value = valueBig
			txData.Data = payload
			txData.To = &contract

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
	cmd.Flags().String(FlagValue, "0", "Value for the transaction")
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	cmd.Flags().Int64(FlagNonce, -1, "Nonce override for the transaction. Negative value means no override")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func CmdERC20Send() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "erc20-send [addr] [recipient] [amount] --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-rpc=<url>",
		Short: "send recipient <amount> (in smallest unit) ERC20 tokens",
		Long:  "",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			contract := common.HexToAddress(args[0])
			recipient := common.HexToAddress(args[1])
			amt, ok := new(big.Int).SetString(args[2], 10)
			if !ok {
				return fmt.Errorf("unable to parse amount: %s", args[2])
			}
			abi, err := native.NativeMetaData.GetAbi()
			if err != nil {
				return err
			}
			payload, err := abi.Pack("transfer", recipient, amt)
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
			var nonce uint64
			if n, err := cmd.Flags().GetInt64(FlagNonce); err == nil && n >= 0 {
				nonce = uint64(n)
			} else {
				nonce, err = getNonce(rpc, key.PublicKey)
				if err != nil {
					return err
				}
			}

			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			txData.Nonce = nonce
			txData.Value = utils.Big0
			txData.Data = payload
			txData.To = &contract

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
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	cmd.Flags().Int64(FlagNonce, -1, "Nonce override for the transaction. Negative value means no override")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func CmdCallPrecompile() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "call-precompile [precompile name] [method] [args...] --value=<payment> --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-rpc=<url>",
		Short: "call method on precompile",
		Long:  "",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			pInfo := precompiles.GetPrecompileInfo(args[0])
			payload, err := getMethodPayload(pInfo.ABI, args[1:])
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
			var nonce uint64
			if n, err := cmd.Flags().GetInt64(FlagNonce); err == nil && n >= 0 {
				nonce = uint64(n)
			} else {
				nonce, err = getNonce(rpc, key.PublicKey)
				if err != nil {
					return err
				}
			}

			value, err := cmd.Flags().GetString(FlagValue)
			if err != nil {
				return err
			}

			valueBig := big.NewInt(0)
			if value != "" {
				valueBig, success := new(big.Int).SetString(value, 10)
				if !success || valueBig.Cmp(utils.Big0) < 0 {
					return fmt.Errorf("%s is not a valid value. Must be a decimal nonnegative integer", value)
				}
			}

			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			txData.Nonce = nonce
			txData.Value = valueBig
			txData.Data = payload
			to := pInfo.Address
			txData.To = &to

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
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	cmd.Flags().Int64(FlagNonce, -1, "Nonce override for the transaction. Negative value means no override")
	cmd.Flags().String(FlagValue, "", "Value for the transaction")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func CmdDeployWSEI() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-wsei --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-rpc=<url>",
		Short: "Deploy ERC20 contract for a native Sei token",
		Long:  "",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			contractData := wsei.GetBin()

			key, err := getPrivateKey(cmd)
			if err != nil {
				return err
			}

			rpc, err := cmd.Flags().GetString(FlagRPC)
			if err != nil {
				return err
			}
			var nonce uint64
			if n, err := cmd.Flags().GetInt64(FlagNonce); err == nil && n >= 0 {
				nonce = uint64(n)
			} else {
				nonce, err = getNonce(rpc, key.PublicKey)
				if err != nil {
					return err
				}
			}

			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			txData.Nonce = nonce
			txData.Value = utils.Big0
			txData.Data = contractData

			resp, err := sendTx(txData, rpc, key)
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
			fmt.Println("Transaction hash:", resp.Hex())
			return nil
		},
	}

	cmd.Flags().Uint64(FlagGasFeeCap, 1000000000000, "Gas fee cap for the transaction")
	cmd.Flags().Uint64(FlagGas, 5000000, "Gas limit for the transaction")
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	cmd.Flags().Int64(FlagNonce, -1, "Nonce override for the transaction. Negative value means no override")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func getPrivateKey(cmd *cobra.Command) (*ecdsa.PrivateKey, error) {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return nil, err
	}
	txf := tx.NewFactoryCLI(clientCtx, cmd.Flags())
	kb := txf.Keybase()
	info, err := kb.Key(clientCtx.GetFromName())
	if err != nil {
		return nil, err
	}
	localInfo, ok := info.(keyring.LocalInfo)
	if !ok {
		return nil, errors.New("can only associate address for local keys")
	}
	if localInfo.GetAlgo() != hd.Secp256k1Type {
		return nil, errors.New("can only use addresses using secp256k1")
	}
	priv, err := legacy.PrivKeyFromBytes([]byte(localInfo.PrivKeyArmor))
	if err != nil {
		return nil, err
	}
	privHex := hex.EncodeToString(priv.Bytes())
	key, _ := crypto.HexToECDSA(privHex)
	return key, nil
}

func getNonce(rpc string, key ecdsa.PublicKey) (uint64, error) {
	nonceQuery := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"%s\",\"pending\"],\"id\":\"send-cli\"}", crypto.PubkeyToAddress(key).Hex())
	req, err := http.NewRequest(http.MethodGet, rpc, strings.NewReader(nonceQuery))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, err
	}
	resObj := map[string]interface{}{}
	if err := json.Unmarshal(resBody, &resObj); err != nil {
		return 0, err
	}
	nonce := new(hexutil.Uint64)
	if err := nonce.UnmarshalText([]byte(resObj["result"].(string))); err != nil {
		return 0, err
	}
	return uint64(*nonce), nil
}

func getChainId(rpc string) (*big.Int, error) {
	q := "{\"jsonrpc\": \"2.0\",\"method\": \"eth_chainId\",\"params\":[],\"id\":\"send-cli\"}"
	req, err := http.NewRequest(http.MethodGet, rpc, strings.NewReader(q))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	resObj := map[string]interface{}{}
	if err := json.Unmarshal(resBody, &resObj); err != nil {
		return nil, err
	}
	chainId := new(hexutil.Big)
	if err := chainId.UnmarshalText([]byte(resObj["result"].(string))); err != nil {
		return nil, err
	}
	return chainId.ToInt(), nil
}

func getTxData(cmd *cobra.Command) (*ethtypes.DynamicFeeTx, error) {
	gasFeeCap, err := cmd.Flags().GetUint64(FlagGasFeeCap)
	if err != nil {
		return nil, err
	}
	gasLimit, err := cmd.Flags().GetUint64(FlagGas)
	if err != nil {
		return nil, err
	}
	rpc, err := cmd.Flags().GetString(FlagRPC)
	if err != nil {
		return nil, err
	}
	chainID, err := getChainId(rpc)
	if err != nil {
		return nil, err
	}
	return &ethtypes.DynamicFeeTx{
		GasFeeCap: new(big.Int).SetUint64(gasFeeCap),
		GasTipCap: new(big.Int).SetUint64(gasFeeCap),
		Gas:       gasLimit,
		ChainID:   chainID,
	}, nil
}

func sendTx(txData *ethtypes.DynamicFeeTx, rpcUrl string, key *ecdsa.PrivateKey) (common.Hash, error) {
	ethCfg := types.DefaultChainConfig().EthereumConfig(txData.ChainID)
	signer := ethtypes.MakeSigner(ethCfg, utils.Big1, 1)
	signedTx, err := ethtypes.SignTx(ethtypes.NewTx(txData), signer, key)
	if err != nil {
		return common.Hash{}, err
	}

	ethClient, err := ethclient.Dial(rpcUrl)
	if err != nil {
		return common.Hash{}, err
	}

	if err := ethClient.SendTransaction(context.Background(), signedTx); err != nil {
		return common.Hash{}, err
	}

	return signedTx.Hash(), nil
}
