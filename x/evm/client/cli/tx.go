package cli

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
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

	return cmd
}

func CmdAssociateAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "associate-address",
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
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:8545", evmrpc.LocalAddress), strings.NewReader(body))
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

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
