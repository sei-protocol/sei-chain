package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto/merkle"
	sm "github.com/tendermint/tendermint/internal/state"
)

const (
	flagStateDBPath = "state-db-path"
)

// MakeGetLastResultsHashCommand constructs a command to print LastResultsHash computation details.
func MakeGetLastResultsHashCommand(conf *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-last-results-hash <height>",
		Short: "Print the LastResultsHash computation details for a given block height",
		Long: `Print all fields that go into the LastResultsHash calculation for a given block height.

This is useful for debugging consensus breaks where nodes have mismatched LastResultsHash values.
The command loads the FinalizeBlockResponses from the state database and prints the deterministic
fields (Code, Data, GasWanted, GasUsed) for each transaction result.

Example:
  $ tendermint get-last-results-hash 12345
  $ tendermint get-last-results-hash 12345 --state-db-path /path/to/state.db
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGetLastResultsHash(cmd, args, conf)
		},
	}

	cmd.Flags().String(flagStateDBPath, "", "Path to the state.db directory (default: <tendermint-home>/data/state.db)")

	return cmd
}

func runGetLastResultsHash(cmd *cobra.Command, args []string, conf *config.Config) error {
	height, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid height %q: %w", args[0], err)
	}

	dbPath, err := cmd.Flags().GetString(flagStateDBPath)
	if err != nil {
		return err
	}
	if dbPath == "" {
		// Use default path based on tendermint home
		dbPath = filepath.Join(conf.RootDir, "data", "state.db")
	}

	fmt.Printf("Opening state database: %s\n", dbPath)

	db, err := openStateDB(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open state database: %w", err)
	}
	defer db.Close()

	stateStore := sm.NewStore(db)

	// Load the FinalizeBlockResponses for the given height
	fbResp, err := stateStore.LoadFinalizeBlockResponses(height)
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

func openStateDB(dbPath string) (dbm.DB, error) {
	// Ensure path ends with .db for consistency check
	if filepath.Ext(dbPath) != ".db" {
		dbPath = dbPath + ".db"
	}

	// Check if directory exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("state database not found at %s", dbPath)
	}

	// For GoLevelDB, we need to strip .db extension and split path
	dir := dbPath[:len(dbPath)-3] // remove .db

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	// Split into directory and name
	parentDir := filepath.Dir(absDir)
	name := filepath.Base(absDir)

	db, err := dbm.NewGoLevelDB(name, parentDir)
	if err != nil {
		return nil, err
	}

	return db, nil
}
