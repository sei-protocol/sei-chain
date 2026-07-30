package operations

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/tools/utils"
)

// flatkvAnalysisModuleName is the logical module name used for the FlatKV
// row in the shared DynamoDB state-size table. Consumers key off this name
// to distinguish FlatKV from memIAVL module rows.
const flatkvAnalysisModuleName = "flatkv"

// FlatKVStateSizeResult holds the complete analysis of a FlatKV store.
type FlatKVStateSizeResult struct {
	// Total holds the aggregate size stats across every physical row.
	Total FlatKVDBSize

	// Per-DB breakdown (account, code, storage, misc).
	DBSizes map[string]*FlatKVDBSize

	// Top EVM contracts by storage size.
	ContractSizes map[string]*utils.ContractSizeEntry
}

// FlatKVDBSize holds size stats for one logical DB.
type FlatKVDBSize struct {
	NumKeys   uint64
	KeySize   uint64
	ValueSize uint64
	TotalSize uint64
}

// collectFlatKVStateSize iterates every physical row in the FlatKV store and
// aggregates size stats per logical DB, plus a top-100 EVM contract table.
func collectFlatKVStateSize(store *flatkv.CommitStore) (*FlatKVStateSizeResult, error) {
	result := &FlatKVStateSizeResult{
		DBSizes:       make(map[string]*FlatKVDBSize),
		ContractSizes: make(map[string]*utils.ContractSizeEntry),
	}

	iter, err := store.RawGlobalIterator()
	if err != nil {
		return nil, fmt.Errorf("raw global iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()
		keySize := uint64(len(key))
		valueSize := uint64(len(value))
		totalSize := keySize + valueSize

		result.Total.NumKeys++
		result.Total.KeySize += keySize
		result.Total.ValueSize += valueSize
		result.Total.TotalSize += totalSize

		dbName := classifyFlatKVPhysicalKey(key)
		if _, ok := result.DBSizes[dbName]; !ok {
			result.DBSizes[dbName] = &FlatKVDBSize{}
		}
		db := result.DBSizes[dbName]
		db.NumKeys++
		db.KeySize += keySize
		db.ValueSize += valueSize
		db.TotalSize += totalSize

		if dbName == flatkvBucketStorage {
			addr := extractFlatKVContractAddress(key)
			if addr != "" {
				if _, ok := result.ContractSizes[addr]; !ok {
					result.ContractSizes[addr] = &utils.ContractSizeEntry{Address: addr}
				}
				entry := result.ContractSizes[addr]
				entry.TotalSize += totalSize
				entry.KeyCount++
			}
		}

		if result.Total.NumKeys%10000000 == 0 {
			fmt.Printf("  scanned %d flatkv keys...\n", result.Total.NumKeys)
		}
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterate flatkv: %w", err)
	}

	result.ContractSizes = limitFlatKVTopContracts(result.ContractSizes, 100)
	return result, nil
}

// classifyFlatKVPhysicalKey determines which logical DB a physical key
// belongs to. Physical format: "<module>/" + type_prefix_byte + stripped_key.
// Non-evm modules and evm keys with an unrecognised type prefix are bucketed
// into "misc". The kind switch mirrors CommitStore.routePhysicalKey so the
// classification stays in sync with FlatKV's actual write routing.
func classifyFlatKVPhysicalKey(key []byte) string {
	moduleName, innerKey, err := ktype.StripModulePrefix(key)
	if err != nil || moduleName != keys.EVMStoreKey {
		return flatkvBucketMisc
	}
	kind, _ := keys.ParseEVMKey(innerKey)
	switch kind {
	case ktype.EVMKeyAccount, keys.EVMKeyCodeHash:
		return flatkvBucketAccount
	case keys.EVMKeyCode:
		return flatkvBucketCode
	case keys.EVMKeyStorage:
		return flatkvBucketStorage
	default:
		return flatkvBucketMisc
	}
}

// extractFlatKVContractAddress extracts the hex address from an evm storage
// physical key. Physical format: "evm/" + 0x03 + addr(20) + slot(32).
func extractFlatKVContractAddress(key []byte) string {
	_, innerKey, err := ktype.StripModulePrefix(key)
	if err != nil || len(innerKey) < 21 {
		return ""
	}
	return fmt.Sprintf("%X", innerKey[1:21])
}

func limitFlatKVTopContracts(contracts map[string]*utils.ContractSizeEntry, limit int) map[string]*utils.ContractSizeEntry {
	if len(contracts) <= limit {
		return contracts
	}
	slice := make([]utils.ContractSizeEntry, 0, len(contracts))
	for _, c := range contracts {
		slice = append(slice, *c)
	}
	sort.Slice(slice, func(i, j int) bool { return slice[i].TotalSize > slice[j].TotalSize })

	result := make(map[string]*utils.ContractSizeEntry, limit)
	for i := 0; i < limit; i++ {
		c := slice[i]
		result[c.Address] = &c
	}
	return result
}

// printFlatKVResults prints a FlatKV section to stdout formatted to match
// the surrounding memIAVL module output from state_size.go.
func printFlatKVResults(r *FlatKVStateSizeResult, height int64) {
	fmt.Printf("\n=== FlatKV state size (version %d) ===\n", height)
	fmt.Printf("Total keys:       %d\n", r.Total.NumKeys)
	fmt.Printf("Total key size:   %d bytes (%.2f MB)\n", r.Total.KeySize, float64(r.Total.KeySize)/1024/1024)
	fmt.Printf("Total value size: %d bytes (%.2f MB)\n", r.Total.ValueSize, float64(r.Total.ValueSize)/1024/1024)
	fmt.Printf("Total size:       %d bytes (%.2f MB)\n", r.Total.TotalSize, float64(r.Total.TotalSize)/1024/1024)

	fmt.Printf("\n--- FlatKV per-DB breakdown ---\n")
	fmt.Printf("%-12s %15s %15s %15s %15s\n", "DB", "Keys", "Key Size", "Value Size", "Total Size")
	fmt.Printf("%s\n", strings.Repeat("-", 75))

	dbOrder := []string{flatkvBucketAccount, flatkvBucketCode, flatkvBucketStorage, flatkvBucketMisc}
	for _, name := range dbOrder {
		db, ok := r.DBSizes[name]
		if !ok {
			continue
		}
		fmt.Printf("%-12s %15d %12d KB %12d KB %12d KB\n",
			name, db.NumKeys, db.KeySize/1024, db.ValueSize/1024, db.TotalSize/1024)
	}

	if len(r.ContractSizes) > 0 {
		fmt.Printf("\n--- FlatKV top EVM contracts by storage size ---\n")
		fmt.Printf("%-42s %15s %10s\n", "Contract Address", "Total Size", "Key Count")
		fmt.Printf("%s\n", strings.Repeat("-", 70))

		slice := make([]utils.ContractSizeEntry, 0, len(r.ContractSizes))
		for _, c := range r.ContractSizes {
			slice = append(slice, *c)
		}
		sort.Slice(slice, func(i, j int) bool { return slice[i].TotalSize > slice[j].TotalSize })

		for _, c := range slice {
			fmt.Printf("0x%-40s %15d %10d\n", c.Address, c.TotalSize, c.KeyCount)
		}
	}
}

// flatkvStateSizeAnalysis packages a FlatKV scan result as a
// *utils.StateSizeAnalysis so it can be pushed to DynamoDB alongside the
// memIAVL module rows in a single batch.
//
// The per-DB breakdown is rendered into PrefixBreakdown using the same JSON
// map shape memIAVL uses ({"<bucket>": PrefixSize}), so downstream consumers
// can parse both module types uniformly.
func flatkvStateSizeAnalysis(r *FlatKVStateSizeResult, height int64) *utils.StateSizeAnalysis {
	prefixMap := make(map[string]*utils.PrefixSize, len(r.DBSizes))
	for dbName, db := range r.DBSizes {
		prefixMap[dbName] = &utils.PrefixSize{
			KeySize:   db.KeySize,
			ValueSize: db.ValueSize,
			TotalSize: db.TotalSize,
			KeyCount:  db.NumKeys,
		}
	}
	prefixJSON, _ := json.Marshal(prefixMap)

	contractSlice := make([]utils.ContractSizeEntry, 0, len(r.ContractSizes))
	for _, c := range r.ContractSizes {
		contractSlice = append(contractSlice, *c)
	}
	contractJSON, _ := json.Marshal(contractSlice)

	return &utils.StateSizeAnalysis{
		BlockHeight:       height,
		ModuleName:        flatkvAnalysisModuleName,
		TotalNumKeys:      r.Total.NumKeys,
		TotalKeySize:      r.Total.KeySize,
		TotalValueSize:    r.Total.ValueSize,
		TotalSize:         r.Total.TotalSize,
		PrefixBreakdown:   string(prefixJSON),
		ContractBreakdown: string(contractJSON),
	}
}
