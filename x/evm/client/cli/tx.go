package cli

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
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
	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw20"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

const (
	FlagGasFeeCap = "gas-fee-cap"
	FlagGas       = "gas-limit"
	FlagRPC       = "evm-rpc"
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
	cmd.AddCommand(CmdDeployErcCw20())
	cmd.AddCommand(CmdCallContract())
	cmd.AddCommand(CmdDeployErcCw721())
	cmd.AddCommand(CmdERC20Send())

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
		Use:   "send [to EVM address] [amount in wei] --from=<sender> --gas-fee-cap=<cap> --gas-limit=<limit> --evm-chain-id=<chain-id> --evm-rpc=<url>",
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
			nonce, err := getNonce(rpc, key.PublicKey)
			if err != nil {
				return err
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
			txData.Nonce = uint64(*nonce)
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
		Use:   "deploy-erc20 [denom] [name] [symbol] [decimal] --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-chain-id=<chain-id> --evm-rpc=<url>",
		Short: "Deploy ERC20 contract for a native Sei token",
		Long:  "",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			err = sdk.ValidateDenom(args[0])
			if err != nil {
				return err
			}
			denom := args[0]
			name := args[1]
			symbol := args[2]
			decimal, err := strconv.ParseUint(args[3], 10, 64)
			if err != nil {
				return err
			}
			if decimal > math.MaxUint8 {
				return fmt.Errorf("decimal cannot be larger than %d", math.MaxUint8)
			}

			bytecode := native.GetBin()
			abi := native.GetABI()
			parsedABI, err := ethabi.JSON(strings.NewReader(string(abi)))
			if err != nil {
				fmt.Println("failed at parsing abi")
				return err
			}
			constructorArguments := []interface{}{
				denom, name, symbol, uint8(decimal),
			}

			packedArgs, err := parsedABI.Pack("", constructorArguments...)
			if err != nil {
				return err
			}
			contractData := append(bytecode, packedArgs...)

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

			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			txData.Nonce = uint64(*nonce)
			txData.Value = big.NewInt(0)
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
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func CmdDeployErcCw20() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-erccw20 [cw20addr] [name] [symbol] --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-chain-id=<chain-id> --evm-rpc=<url>",
		Short: "Deploy ERC20 contract for a CW20 token",
		Long:  "",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			_, err = sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return err
			}

			bytecode := cw20.GetBin()
			abi := cw20.GetABI()
			parsedABI, err := ethabi.JSON(strings.NewReader(string(abi)))
			if err != nil {
				fmt.Println("failed at parsing abi")
				return err
			}
			constructorArguments := []interface{}{
				args[0], args[1], args[2],
			}

			packedArgs, err := parsedABI.Pack("", constructorArguments...)
			if err != nil {
				return err
			}
			contractData := append(bytecode, packedArgs...)

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

			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			txData.Nonce = uint64(*nonce)
			txData.Value = big.NewInt(0)
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
	cmd.Flags().Uint64(FlagGas, 7000000, "Gas limit for the transaction")
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func CmdDeployErcCw721() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-erccw721 [cw721addr] [name] [symbol] --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-chain-id=<chain-id> --evm-rpc=<url>",
		Short: "Deploy ERC721 contract for a CW20 token",
		Long:  "",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			_, err = sdk.AccAddressFromBech32(args[0])
			if err != nil {
				return err
			}

			bytecode := cw721.GetBin()
			abi := cw721.GetABI()
			parsedABI, err := ethabi.JSON(strings.NewReader(string(abi)))
			if err != nil {
				fmt.Println("failed at parsing abi")
				return err
			}
			constructorArguments := []interface{}{
				args[0], args[1], args[2],
			}

			packedArgs, err := parsedABI.Pack("", constructorArguments...)
			if err != nil {
				return err
			}
			contractData := append(bytecode, packedArgs...)

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

			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			txData.Nonce = uint64(*nonce)
			txData.Value = big.NewInt(0)
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
	cmd.Flags().Uint64(FlagGas, 7000000, "Gas limit for the transaction")
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func CmdCallContract() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "call-contract [addr] [payload hex] --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-chain-id=<chain-id> --evm-rpc=<url>",
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
			nonce, err := getNonce(rpc, key.PublicKey)
			if err != nil {
				return err
			}

			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			txData.Nonce = uint64(*nonce)
			txData.Value = big.NewInt(0)
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
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func CmdERC20Send() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "erc20-send [addr] [recipient] [amount] --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-chain-id=<chain-id> --evm-rpc=<url>",
		Short: "send recipient <amount> (in smallest unit) ERC20 tokens",
		Long:  "",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			contract := common.HexToAddress(args[0])
			recipient := common.HexToAddress(args[1])
			amt, err := strconv.ParseUint(args[2], 10, 64)
			if err != nil {
				return err
			}
			abi, err := native.NativeMetaData.GetAbi()
			if err != nil {
				return err
			}
			payload, err := abi.Pack("transfer", recipient, new(big.Int).SetUint64(amt))
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

			txData, err := getTxData(cmd)
			if err != nil {
				return err
			}
			txData.Nonce = uint64(*nonce)
			txData.Value = big.NewInt(0)
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
	priv, err := legacy.PrivKeyFromBytes([]byte(localInfo.PrivKeyArmor))
	if err != nil {
		return nil, err
	}
	privHex := hex.EncodeToString(priv.Bytes())
	key, _ := crypto.HexToECDSA(privHex)
	return key, nil
}

func getNonce(rpc string, key ecdsa.PublicKey) (*hexutil.Uint64, error) {
	nonceQuery := fmt.Sprintf("{\"jsonrpc\": \"2.0\",\"method\": \"eth_getTransactionCount\",\"params\":[\"%s\",\"pending\"],\"id\":\"send-cli\"}", crypto.PubkeyToAddress(key).Hex())
	req, err := http.NewRequest(http.MethodGet, rpc, strings.NewReader(nonceQuery))
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
	nonce := new(hexutil.Uint64)
	if err := nonce.UnmarshalText([]byte(resObj["result"].(string))); err != nil {
		return nil, err
	}
	return nonce, nil
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
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), 1)
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
