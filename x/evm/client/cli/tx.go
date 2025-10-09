package cli

import (
	"bytes"
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

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"  // ✅ correct alias position
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/precompiles"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/wsei"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

// -----------------------------------------------------------------------------
// constants and simple types
// -----------------------------------------------------------------------------

const (
	FlagGasFeeCap = "gas-fee-cap"
	FlagGas       = "gas-limit"
	FlagValue     = "value"
	FlagRPC       = "evm-rpc"
	FlagNonce     = "nonce"
)

// JSON‑RPC envelope for sei_associate
type SeiAssociateRequest struct {
	JSONRPC string                    `json:"jsonrpc"`
	Method  string                    `json:"method"`
	Params  []evmrpc.AssociateRequest `json:"params"`
	ID      string                    `json:"id"`
}

// -----------------------------------------------------------------------------
// root tx command registration
// -----------------------------------------------------------------------------

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

	return cmd
}

// -----------------------------------------------------------------------------
// associate‑address command
// -----------------------------------------------------------------------------

func CmdAssociateAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "associate-address [optional priv key hex] --rpc=<url> --from=<sender>",
		Short: "associate EVM and Sei address for the sender",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
					return errors.New("only secp256k1 supported")
				}
				priv, err := legacy.PrivKeyFromBytes([]byte(localInfo.PrivKeyArmor))
				if err != nil {
					return err
				}
				privHex = hex.EncodeToString(priv.Bytes())
			}

			key, err := crypto.HexToECDSA(privHex)
			if err != nil {
				return err
			}

			emptyHash := crypto.Keccak256Hash([]byte{})
			sig, err := crypto.Sign(emptyHash[:], key)
			if err != nil {
				return err
			}
			R, S, _, err := ethtx.DecodeSignature(sig)
			if err != nil {
				return err
			}
			V := big.NewInt(int64(sig[64]))

			txData := evmrpc.AssociateRequest{
				V: hex.EncodeToString(V.Bytes()),
				R: hex.EncodeToString(R.Bytes()),
				S: hex.EncodeToString(S.Bytes()),
			}

			fullReq := SeiAssociateRequest{
				JSONRPC: "2.0",
				Method:  "sei_associate",
				Params:  []evmrpc.AssociateRequest{txData},
				ID:      "associate_addr",
			}

			bodyBytes, _ := json.Marshal(fullReq)
			rpc, _ := cmd.Flags().GetString(FlagRPC)
			req, _ := http.NewRequest(http.MethodPost, rpc, bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer res.Body.Close()
			out, _ := io.ReadAll(res.Body)
			fmt.Printf("Response: %s\n", string(out))
			return nil
		},
	}

	cmd.Flags().String(FlagRPC, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), "RPC endpoint to send request to")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// -----------------------------------------------------------------------------
// rest of your existing commands (send / deploy / etc.)
// copy them exactly as they were; they already compile
// -----------------------------------------------------------------------------
