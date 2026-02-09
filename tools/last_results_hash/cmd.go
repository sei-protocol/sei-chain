package lastresultshash

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/orderedcode"
	"github.com/spf13/cobra"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto/merkle"
	dbm "github.com/tendermint/tm-db"
)

const (
	// prefixFinalizeBlockResponses matches the prefix used in sei-tendermint/internal/state/store.go
	prefixFinalizeBlockResponses = int64(14)
)

func finalizeBlockResponsesKey(height int64) []byte {
	res, err := orderedcode.Append(nil, prefixFinalizeBlockResponses, height)
	if err != nil {
		panic(err)
	}
	return res
}

// GetLastResultsHashCmd returns the cobra command for getting LastResultsHash details.
func GetLastResultsHashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-last-results-hash <height>",
		Short: "Print the LastResultsHash computation details for a given block height",
		Long: `Print all fields that go into the LastResultsHash calculation for a given block height.

This is useful for debugging consensus breaks where nodes have mismatched LastResultsHash values.
The command loads the FinalizeBlockResponses from the state database and prints the deterministic
fields (Code, Data, GasWanted, GasUsed) for each transaction result.

Example:
  $ seid tools get-last-results-hash 12345
  $ seid tools get-last-results-hash 12345 --home-dir /path/to/.sei
`,
		Args: cobra.ExactArgs(1),
		RunE: runGetLastResultsHash,
	}

	cmd.Flags().String("home-dir", "", "Sei home directory (default: $HOME/.sei)")

	return cmd
}

func runGetLastResultsHash(cmd *cobra.Command, args []string) error {
	height, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid height %q: %w", args[0], err)
	}

	homeDir, err := cmd.Flags().GetString("home-dir")
	if err != nil {
		return err
	}
	if homeDir == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user home directory: %w", err)
		}
		homeDir = filepath.Join(userHome, ".sei")
	}

	dataDir := filepath.Join(homeDir, "data")
	fmt.Printf("Opening state database in: %s\n", dataDir)

	db, err := dbm.NewGoLevelDB("state", dataDir)
	if err != nil {
		return fmt.Errorf("failed to open state database: %w", err)
	}
	defer db.Close()

	// Load the FinalizeBlockResponses for the given height
	fbResp, err := loadFinalizeBlockResponses(db, height)
	if err != nil {
		return fmt.Errorf("failed to load FinalizeBlockResponses for height %d: %w", height, err)
	}

	if fbResp == nil {
		return fmt.Errorf("FinalizeBlockResponses is nil for height %d", height)
	}

	// Marshal and compute hash
	rs, err := abci.MarshalTxResults(fbResp.TxResults)
	if err != nil {
		return fmt.Errorf("failed to marshal TxResults: %w", err)
	}
	computedHash := merkle.HashFromByteSlices(rs)

	fmt.Printf("\n=== LastResultsHash Computation Details ===\n")
	fmt.Printf("Height: %d\n", height)
	fmt.Printf("TxCount: %d\n", len(fbResp.TxResults))
	fmt.Printf("ComputedHash: %X\n", computedHash)
	fmt.Printf("\n")

	if len(fbResp.TxResults) == 0 {
		fmt.Printf("No transactions in this block.\n")
		return nil
	}

	fmt.Printf("=== Transaction Results (Deterministic Fields) ===\n")
	fmt.Printf("These fields are used to compute LastResultsHash:\n")
	fmt.Printf("  - Code: response code (0 = success)\n")
	fmt.Printf("  - Data: response data\n")
	fmt.Printf("  - GasWanted: requested gas\n")
	fmt.Printf("  - GasUsed: actual gas consumed\n")
	fmt.Printf("\n")

	for i, txRes := range fbResp.TxResults {
		fmt.Printf("--- TxResult[%d] ---\n", i)
		fmt.Printf("  Code:      %d\n", txRes.Code)
		fmt.Printf("  GasWanted: %d\n", txRes.GasWanted)
		fmt.Printf("  GasUsed:   %d\n", txRes.GasUsed)
		fmt.Printf("  DataLen:   %d\n", len(txRes.Data))
		if len(txRes.Data) > 0 {
			fmt.Printf("  DataHex:   %X\n", txRes.Data)
		}
		fmt.Printf("\n")
	}

	return nil
}

func loadFinalizeBlockResponses(db dbm.DB, height int64) (*abci.ResponseFinalizeBlock, error) {
	buf, err := db.Get(finalizeBlockResponsesKey(height))
	if err != nil {
		return nil, err
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("no FinalizeBlockResponses found for height %d", height)
	}

	finalizeBlockResponses := new(abci.ResponseFinalizeBlock)
	err = finalizeBlockResponses.Unmarshal(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal FinalizeBlockResponses: %w", err)
	}

	return finalizeBlockResponses, nil
}
