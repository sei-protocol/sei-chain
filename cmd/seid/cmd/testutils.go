package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/giga/executor/testing"
)

func TestUtilsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "testutils",
		Short: "Testing utility commands",
		Long:  "Commands for testing and debugging utilities",
	}

	cmd.AddCommand(TxReadsCmd())

	return cmd
}

func TxReadsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tx-reads",
		Short: "Get transaction reads for a given transaction hash",
		Long:  "Get transaction reads (SLOAD operations) for a given transaction hash by tracing the transaction execution",
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := cmd.Flags().GetString("url")
			if err != nil {
				return fmt.Errorf("error retrieving url: %w", err)
			}
			if url == "" {
				return fmt.Errorf("url is required")
			}

			txHashStr, err := cmd.Flags().GetString("tx-hash")
			if err != nil {
				return fmt.Errorf("error retrieving tx-hash: %w", err)
			}
			if txHashStr == "" {
				return fmt.Errorf("tx-hash is required")
			}

			txHash := common.HexToHash(txHashStr)
			if txHash == (common.Hash{}) {
				return fmt.Errorf("invalid transaction hash: %s", txHashStr)
			}

			reads := testing.GetTransactionReads(url, txHash)

			// Convert nested map to a format suitable for JSON output
			// Outer map: contract address -> inner map
			// Inner map: state key -> state value
			outputMap := make(map[string]map[string]string)
			for contractAddr, stateMap := range reads {
				stateOutput := make(map[string]string)
				for stateKey, stateValue := range stateMap {
					stateOutput[stateKey.Hex()] = stateValue.Hex()
				}
				outputMap[contractAddr.Hex()] = stateOutput
			}

			// Format output as JSON for readability
			output, err := json.MarshalIndent(outputMap, "", "  ")
			if err != nil {
				return fmt.Errorf("error marshaling output: %w", err)
			}

			fmt.Println("Transaction Reads:")
			fmt.Println("Format: { contract_address: { state_key: state_value } }")
			fmt.Println()
			fmt.Println(string(output))

			return nil
		},
	}

	cmd.Flags().String("url", "http://localhost:8545", "RPC server URL (full URL including protocol and port)")
	cmd.Flags().String("tx-hash", "", "Transaction hash (required)")

	return cmd
}
