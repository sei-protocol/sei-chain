package operations

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/ss"
	"github.com/sei-protocol/sei-db/tools/cmd/seidb/benchmark"
	"github.com/spf13/cobra"
)

type evmFreqBucket struct {
	lo, hi int64
}

func ScanEvmKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan-evm-keys",
		Short: "Scan SS for EVM module: total unique keys updated between two heights and update frequency histogram",
		Run:   executeScanEvmKeys,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "Database Directory")
	cmd.PersistentFlags().StringP("db-backend", "b", "pebbledb", "DB Backend (pebbledb or rocksdb)")
	cmd.PersistentFlags().Int64("start-height", 0, "Start height (exclusive). Only count keys with version > start-height. 0 = no lower bound")
	cmd.PersistentFlags().Int64("end-height", 0, "End height (inclusive). Only count keys with version <= end-height. 0 = no upper bound")

	return cmd
}

func executeScanEvmKeys(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	dbBackend, _ := cmd.Flags().GetString("db-backend")
	startHeight, _ := cmd.Flags().GetInt64("start-height")
	endHeight, _ := cmd.Flags().GetInt64("end-height")

	if dbDir == "" {
		panic("Must provide database dir")
	}

	_, isAcceptedBackend := benchmark.ValidDBBackends[dbBackend]
	if !isAcceptedBackend {
		panic(fmt.Sprintf("Unsupported db backend: %s\n", dbBackend))
	}

	if startHeight > 0 && endHeight > 0 && startHeight >= endHeight {
		panic(fmt.Sprintf("start-height (%d) must be less than end-height (%d)", startHeight, endHeight))
	}

	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Backend = dbBackend
	ssConfig.DBDirectory = dbDir
	ssConfig.KeepRecent = 0
	backend, err := ss.NewStateStore(logger.NewNopLogger(), dbDir, ssConfig)
	if err != nil {
		panic(err)
	}
	defer func() { _ = backend.Close() }()

	latestVersion := backend.GetLatestVersion()
	earliestVersion := backend.GetEarliestVersion()
	fmt.Printf("SS store version range: [%d, %d]\n", earliestVersion, latestVersion)

	rangeDesc := "all versions"
	if startHeight > 0 && endHeight > 0 {
		rangeDesc = fmt.Sprintf("height (%d, %d]", startHeight, endHeight)
	} else if startHeight > 0 {
		rangeDesc = fmt.Sprintf("height > %d", startHeight)
	} else if endHeight > 0 {
		rangeDesc = fmt.Sprintf("height <= %d", endHeight)
	}
	fmt.Printf("Scanning EVM module keys for %s...\n\n", rangeDesc)

	keyUpdateCounts := make(map[string]int64)
	var totalEntries int64
	var scannedEntries int64

	_, err = backend.RawIterate("evm", func(key, value []byte, version int64) bool {
		scannedEntries++
		if scannedEntries%10_000_000 == 0 {
			fmt.Printf("  progress: scanned %dM entries, %d qualifying, %d unique keys so far...\n",
				scannedEntries/1_000_000, totalEntries, len(keyUpdateCounts))
		}

		if startHeight > 0 && version <= startHeight {
			return false
		}
		if endHeight > 0 && version > endHeight {
			return false
		}

		totalEntries++
		keyUpdateCounts[string(key)]++

		return false
	})
	if err != nil {
		panic(err)
	}

	totalUniqueKeys := int64(len(keyUpdateCounts))

	fmt.Printf("\n========================================\n")
	fmt.Printf(" EVM Module SS Scan — %s\n", rangeDesc)
	fmt.Printf("========================================\n\n")

	fmt.Printf("Total MVCC entries scanned:  %d\n", scannedEntries)
	fmt.Printf("Qualifying entries:          %d\n", totalEntries)
	fmt.Printf("Total unique keys updated:   %d\n\n", totalUniqueKeys)

	printEvmFrequencyHistogram(keyUpdateCounts, totalUniqueKeys, rangeDesc)
}

func printEvmFrequencyHistogram(keyUpdateCounts map[string]int64, totalUniqueKeys int64, rangeDesc string) {
	freqHistogram := make(map[int64]int64)
	for _, count := range keyUpdateCounts {
		freqHistogram[count]++
	}

	frequencies := make([]int64, 0, len(freqHistogram))
	for f := range freqHistogram {
		frequencies = append(frequencies, f)
	}
	sort.Slice(frequencies, func(i, j int) bool { return frequencies[i] < frequencies[j] })

	fmt.Printf("--- Key Update Frequency Histogram ---\n")
	fmt.Printf("(How many keys were updated exactly N times within %s)\n\n", rangeDesc)

	if totalUniqueKeys == 0 {
		fmt.Println("No keys found.")
		return
	}

	if len(frequencies) <= 60 {
		fmt.Printf("%15s %15s %10s %15s\n", "Update Count", "Num Keys", "Pct", "Cumulative %")
		fmt.Printf("%s\n", strings.Repeat("-", 60))
		var cumulativeKeys int64
		for _, freq := range frequencies {
			count := freqHistogram[freq]
			cumulativeKeys += count
			pct := float64(count) / float64(totalUniqueKeys) * 100.0
			cumPct := float64(cumulativeKeys) / float64(totalUniqueKeys) * 100.0
			fmt.Printf("%15d %15d %9.2f%% %14.2f%%\n", freq, count, pct, cumPct)
		}
	} else {
		printEvmBucketedHistogram(frequencies, freqHistogram, totalUniqueKeys)
	}
	fmt.Println()
}

func printEvmBucketedHistogram(frequencies []int64, freqHistogram map[int64]int64, totalUniqueKeys int64) {
	maxFreq := frequencies[len(frequencies)-1]
	buckets := buildEvmLogBuckets(1, maxFreq)

	fmt.Printf("%20s %15s %10s %15s\n", "Update Range", "Num Keys", "Pct", "Cumulative %")
	fmt.Printf("%s\n", strings.Repeat("-", 65))

	var cumulativeKeys int64
	for _, b := range buckets {
		var bucketCount int64
		for _, freq := range frequencies {
			if freq >= b.lo && freq <= b.hi {
				bucketCount += freqHistogram[freq]
			}
		}
		if bucketCount == 0 {
			continue
		}
		cumulativeKeys += bucketCount
		pct := float64(bucketCount) / float64(totalUniqueKeys) * 100.0
		cumPct := float64(cumulativeKeys) / float64(totalUniqueKeys) * 100.0
		var label string
		if b.lo == b.hi {
			label = fmt.Sprintf("%d", b.lo)
		} else {
			label = fmt.Sprintf("%d - %d", b.lo, b.hi)
		}
		fmt.Printf("%20s %15d %9.2f%% %14.2f%%\n", label, bucketCount, pct, cumPct)
	}
}

func buildEvmLogBuckets(minVal, maxVal int64) []evmFreqBucket {
	var buckets []evmFreqBucket
	for i := minVal; i <= 10 && i <= maxVal; i++ {
		buckets = append(buckets, evmFreqBucket{lo: i, hi: i})
	}
	if maxVal <= 10 {
		return buckets
	}
	lo := int64(11)
	for lo <= maxVal {
		exp := math.Floor(math.Log10(float64(lo)))
		step := int64(math.Pow(10, exp))
		hi := lo + step - 1
		if hi > maxVal {
			hi = maxVal
		}
		buckets = append(buckets, evmFreqBucket{lo: lo, hi: hi})
		lo = hi + 1
	}
	return buckets
}
