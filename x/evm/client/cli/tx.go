package cli

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

const (
	FlagGasFeeCap  = "gas-fee-cap"
	FlagGas        = "gas-limit"
	FlagEVMChainID = "evm-chain-id"
	FlagRPC        = "rpc"
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

	return cmd
}

func CmdAssociateAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "associate-address --rpc=<url> --from=<sender>",
		Short: "associate EVM and Sei address for the sender",
		Long:  "",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			txf := tx.NewFactoryCLI(clientCtx, cmd.Flags())
			kb := txf.Keybase()
			emptyHash := common.Hash{}
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
		Use:   "send [to EVM address] [amount in usei] [nonce] --from=<sender> --gas-fee-cap=<cap> --gas-limt=<limit> --evm-chain-id=<chain-id> --rpc=<url>",
		Short: "send usei to EVM address",
		Long:  "",
		Args:  cobra.ExactArgs(3),
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

			to := common.HexToAddress(args[0])
			val, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return err
			}
			nonce, err := strconv.ParseUint(args[2], 10, 64)
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
			txData := ethtypes.DynamicFeeTx{
				Nonce:     nonce,
				GasFeeCap: new(big.Int).SetUint64(gasFeeCap),
				Gas:       gasLimit,
				To:        &to,
				Value:     new(big.Int).Mul(new(big.Int).SetUint64(val), state.UseiToSweiMultiplier),
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

	cmd.Flags().Uint64(FlagGasFeeCap, 1000000000000, "Gas fee cap for the transaction")
	cmd.Flags().Uint64(FlagGas, 21000, "Gas limit for the transaction")
	cmd.Flags().Uint64(FlagEVMChainID, 713715, "EVM chain ID")
	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
