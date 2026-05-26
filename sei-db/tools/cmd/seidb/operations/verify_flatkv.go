package operations

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	compositeverifier "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/composite/verifier"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/spf13/cobra"
)

func VerifyFlatKVCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-flatkv",
		Short: "Verify FlatKV data integrity: recompute LtHash and optionally compare against memiavl",
		Run:   executeVerifyFlatKV,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "FlatKV data directory")
	cmd.PersistentFlags().Int64("height", 0, "Block height (0 = latest)")
	cmd.PersistentFlags().String("memiavl-dir", "", "Optional memiavl directory for cross-store comparison")

	return cmd
}

func executeVerifyFlatKV(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	height, _ := cmd.Flags().GetInt64("height")
	memiavlDir, _ := cmd.Flags().GetString("memiavl-dir")

	if dbDir == "" {
		panic("Must provide database dir")
	}

	store, err := openFlatKVReadOnly(dbDir, height)
	if err != nil {
		panic(err)
	}
	defer func() { _ = store.Close() }()

	fmt.Printf("=== FlatKV Verification ===\n")
	fmt.Printf("Version:        %d\n", store.Version())
	fmt.Printf("Stored hash:    %X\n", store.CommittedRootHash())
	fmt.Println()

	// Phase 1: LtHash verification — recompute from full scan
	ok := verifyLtHashFromScan(store.CommitStore)

	// Phase 2: Cross-store comparison with memiavl (if requested)
	if memiavlDir != "" {
		fmt.Println()
		crossOK := verifyCrossStore(store.CommitStore, memiavlDir)
		ok = ok && crossOK
	}

	fmt.Println()
	if ok {
		fmt.Println("RESULT: ALL CHECKS PASSED")
	} else {
		fmt.Println("RESULT: VERIFICATION FAILED")
	}
}

// verifyLtHashFromScan recomputes the LtHash from scratch by iterating
// every key-value pair in the store and comparing against the stored hash.
func verifyLtHashFromScan(store *flatkv.CommitStore) bool {
	fmt.Println("--- Phase 1: LtHash Recomputation ---")
	start := time.Now()

	iter := store.RawGlobalIterator()
	defer func() { _ = iter.Close() }()

	var (
		pairs   []lthash.KVPairWithLastValue
		numKeys uint64
	)

	const batchSize = 100000

	result := lthash.New()

	if iter.First() {
		for iter.Valid() {
			key := make([]byte, len(iter.Key()))
			copy(key, iter.Key())
			value := make([]byte, len(iter.Value()))
			copy(value, iter.Value())

			pairs = append(pairs, lthash.KVPairWithLastValue{
				Key:   key,
				Value: value,
			})
			numKeys++

			if len(pairs) >= batchSize {
				batchResult, _ := lthash.ComputeLtHash(nil, pairs)
				result.MixIn(batchResult)
				pairs = pairs[:0]

				if numKeys%1000000 == 0 {
					fmt.Printf("  scanned %d keys...\n", numKeys)
				}
			}

			iter.Next()
		}
	}

	if err := iter.Error(); err != nil {
		fmt.Printf("  ERROR: iterator error: %v\n", err)
		return false
	}

	// Process remaining pairs
	if len(pairs) > 0 {
		batchResult, _ := lthash.ComputeLtHash(nil, pairs)
		result.MixIn(batchResult)
	}

	elapsed := time.Since(start)
	recomputedChecksum := result.Checksum()
	storedHash := store.CommittedRootHash()

	fmt.Printf("  Keys scanned:     %d\n", numKeys)
	fmt.Printf("  Time:             %s\n", elapsed)
	fmt.Printf("  Recomputed hash:  %X\n", recomputedChecksum[:])
	fmt.Printf("  Stored hash:      %X\n", storedHash)

	if bytes.Equal(recomputedChecksum[:], storedHash) {
		fmt.Println("  STATUS: PASS — LtHash matches")
		return true
	}

	fmt.Println("  STATUS: FAIL — LtHash mismatch!")
	return false
}

// verifyCrossStore opens a memiavl DB and verifies that every decoded FlatKV
// exporter row matches the corresponding memiavl module/key/value.
//
// This mirrors the production forward-subset oracle semantics:
// FlatKV rows must round-trip to memiavl, including decoded account rows
// (nonce/codehash) and non-EVM legacy-module rows.
func verifyCrossStore(flatkvStore *flatkv.CommitStore, memiavlDir string) bool {
	fmt.Println("--- Phase 2: Cross-Store Comparison (FlatKV vs memiavl) ---")

	height := flatkvStore.Version()

	opts := memiavl.Options{
		Dir:             memiavlDir,
		ZeroCopy:        true,
		ReadOnly:        true,
		CreateIfMissing: false,
	}
	db, err := memiavl.OpenDB(height, opts)
	if err != nil {
		fmt.Printf("  ERROR: failed to open memiavl at height %d: %v\n", height, err)
		return false
	}
	defer func() { _ = db.Close() }()

	fmt.Printf("  Opened memiavl at version %d\n", db.Version())
	if db.Version() != height {
		fmt.Printf("  WARNING: memiavl version %d != FlatKV version %d\n", db.Version(), height)
	}

	start := time.Now()
	var (
		rowsExamined uint64
		mismatches   uint64
	)

	exp, err := flatkvStore.Exporter(height)
	if err != nil {
		fmt.Printf("  ERROR: failed to open FlatKV exporter at height %d: %v\n", height, err)
		return false
	}
	defer func() { _ = exp.Close() }()

	for {
		item, err := exp.Next()
		if err != nil {
			if errors.Is(err, errorutils.ErrorExportDone) {
				break
			}
			fmt.Printf("  ERROR: exporter iteration failed: %v\n", err)
			return false
		}
		node, ok := item.(*sctypes.SnapshotNode)
		if !ok {
			fmt.Printf("  ERROR: unexpected exporter item type %T\n", item)
			return false
		}

		decodedRows, err := compositeverifier.DecodeFlatKVNode(node)
		if err != nil {
			mismatches++
			if mismatches <= 10 {
				fmt.Printf("  DECODE ERROR: physical key %X: %v\n", node.Key, err)
			}
			continue
		}
		for _, row := range decodedRows {
			rowsExamined++
			tree := db.TreeByName(row.Module)
			if tree == nil {
				mismatches++
				if mismatches <= 10 {
					fmt.Printf("  MISSING MODULE: module=%s key=%X exists in FlatKV but memiavl tree is absent\n", row.Module, row.Key)
				}
				continue
			}

			memiavlValue := tree.Get(row.Key)
			if memiavlValue == nil {
				mismatches++
				if mismatches <= 10 {
					fmt.Printf("  MISSING: module=%s key=%X exists in FlatKV but not in memiavl\n", row.Module, row.Key)
				}
				continue
			}
			if !bytes.Equal(memiavlValue, row.Value) {
				mismatches++
				if mismatches <= 10 {
					fmt.Printf("  MISMATCH: module=%s key=%X\n    memiavl value: %X\n    flatkv  value: %X\n",
						row.Module, row.Key, memiavlValue, row.Value)
				}
			}

			if rowsExamined%1000000 == 0 {
				fmt.Printf("  checked %d decoded rows...\n", rowsExamined)
			}
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("  Decoded FlatKV rows checked: %d\n", rowsExamined)
	fmt.Printf("  Mismatches:                 %d\n", mismatches)
	fmt.Printf("  Time:                       %s\n", elapsed)

	if mismatches == 0 {
		fmt.Println("  STATUS: PASS — all decoded FlatKV rows match memiavl")
		return true
	}

	fmt.Println("  STATUS: FAIL — discrepancies found")
	return false
}
