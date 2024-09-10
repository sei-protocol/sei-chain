package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"

	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

const (
	FlagAllowList = "allow-list"
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

	cmd.AddCommand(
		NewCreateDenomCmd(),
		NewMintCmd(),
		NewBurnCmd(),
		NewChangeAdminCmd(),
		NewSetDenomMetadataCmd(),
	)

	return cmd
}

// NewCreateDenomCmd broadcast MsgCreateDenom
func NewCreateDenomCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-denom [subdenom] [flags]",
		Short: "create a new denom from an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			queryClientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := evmtypes.NewQueryClient(queryClientCtx)

			allowListFilePath, err := cmd.Flags().GetString(FlagAllowList)
			if err != nil {
				return err
			}

			txf := tx.NewFactoryCLI(clientCtx, cmd.Flags()).WithTxConfig(clientCtx.TxConfig).WithAccountRetriever(clientCtx.AccountRetriever)

			msg := types.NewMsgCreateDenom(
				clientCtx.GetFromAddress().String(),
				args[0],
			)

			// only parse allow list if it is provided
			if allowListFilePath != "" {
				// Parse the allow list
				allowList, err := ParseAllowListJSON(allowListFilePath, queryClient)
				if err != nil {
					return err
				}
				msg.AllowList = &allowList
			}

			return tx.GenerateOrBroadcastTxWithFactory(clientCtx, txf, msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().String(FlagAllowList, "", "Path to the allow list JSON file with an array of addresses "+
		"that are allowed to send/receive the token. The file should have the following format: {\"addresses\": "+
		"[\"addr1\", \"addr2\"]}, where addr1 and addr2 are bech32 Sei native addresses.")
	return cmd
}

// NewMintCmd broadcast MsgMint
func NewMintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mint [amount] [flags]",
		Short: "Mint a denom to an address. Must have admin authority to do so.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			txf := tx.NewFactoryCLI(clientCtx, cmd.Flags()).WithTxConfig(clientCtx.TxConfig).WithAccountRetriever(clientCtx.AccountRetriever)

			amount, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return err
			}

			msg := types.NewMsgMint(
				clientCtx.GetFromAddress().String(),
				amount,
			)

			return tx.GenerateOrBroadcastTxWithFactory(clientCtx, txf, msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewBurnCmd broadcast MsgBurn
func NewBurnCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "burn [amount] [flags]",
		Short: "Burn tokens from an address. Must have admin authority to do so.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			txf := tx.NewFactoryCLI(clientCtx, cmd.Flags()).WithTxConfig(clientCtx.TxConfig).WithAccountRetriever(clientCtx.AccountRetriever)

			amount, err := sdk.ParseCoinNormalized(args[0])
			if err != nil {
				return err
			}

			msg := types.NewMsgBurn(
				clientCtx.GetFromAddress().String(),
				amount,
			)

			return tx.GenerateOrBroadcastTxWithFactory(clientCtx, txf, msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewChangeAdminCmd broadcast MsgChangeAdmin
func NewChangeAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change-admin [denom] [new-admin-address] [flags]",
		Short: "Changes the admin address for a factory-created denom. Must have admin authority to do so.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			txf := tx.NewFactoryCLI(clientCtx, cmd.Flags()).WithTxConfig(clientCtx.TxConfig).WithAccountRetriever(clientCtx.AccountRetriever)

			msg := types.NewMsgChangeAdmin(
				clientCtx.GetFromAddress().String(),
				args[0],
				args[1],
			)

			return tx.GenerateOrBroadcastTxWithFactory(clientCtx, txf, msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func NewSetDenomMetadataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-denom-metadata [metadata-file] [flags]",
		Short: "Set metadata for a factory-created denom. Must have admin authority to do so.",
		Long: strings.TrimSpace(
			`
Example:
$ seid tx tokenfactory set-denom-metadata <path/to/metadata.json> --from=<key_or_address>

Where metadata.json contains:

{
  "description": "Update token metadata",
  "denom_units": [
	{
		"denom": "doge1",
		"exponent": 6,
		"aliases": ["d", "o", "g"]
	},
	{
		"denom": "doge2",
		"exponent": 3,
		"aliases": ["d", "o", "g"]
	}
  ],
  "base": "doge",
  "display": "DOGE",
  "name": "dogecoin",
  "symbol": "DOGE"
}`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			txf := tx.NewFactoryCLI(clientCtx, cmd.Flags()).WithTxConfig(clientCtx.TxConfig).WithAccountRetriever(clientCtx.AccountRetriever)

			metadata, err := ParseMetadataJSON(clientCtx.LegacyAmino, args[0])
			if err != nil {
				return err
			}

			msg := types.NewMsgSetDenomMetadata(
				clientCtx.GetFromAddress().String(),
				metadata,
			)

			return tx.GenerateOrBroadcastTxWithFactory(clientCtx, txf, msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func ParseMetadataJSON(cdc *codec.LegacyAmino, metadataFile string) (banktypes.Metadata, error) {
	proposal := banktypes.Metadata{}

	contents, err := os.ReadFile(metadataFile)
	if err != nil {
		return proposal, err
	}

	if err := cdc.UnmarshalJSON(contents, &proposal); err != nil {
		return proposal, err
	}

	return proposal, nil
}

func ParseAllowListJSON(allowListFile string, queryClient evmtypes.QueryClient) (banktypes.AllowList, error) {
	allowList := banktypes.AllowList{}

	file, err := os.Open(allowListFile)
	if err != nil {
		return allowList, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&allowList); err != nil {
		return allowList, err
	}

	addressMap := make(map[string]struct{})
	var uniqueAddresses []string
	for _, addr := range allowList.Addresses {
		if _, exists := addressMap[addr]; exists {
			continue // Skip duplicate addresses
		}
		addressMap[addr] = struct{}{}

		if common.IsHexAddress(addr) {
			res, err := queryClient.SeiAddressByEVMAddress(context.Background(), &evmtypes.QuerySeiAddressByEVMAddressRequest{EvmAddress: addr})
			if err != nil {
				return allowList, err
			}
			uniqueAddresses = append(uniqueAddresses, res.SeiAddress)
			continue
		}
		if _, err := sdk.AccAddressFromBech32(addr); err != nil {
			return allowList, fmt.Errorf("invalid address %s: %w", addr, err)
		}
		uniqueAddresses = append(uniqueAddresses, addr)
	}

	allowList.Addresses = uniqueAddresses
	return allowList, nil
}
