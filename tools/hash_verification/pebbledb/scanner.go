package pebbledb

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/tools/hash_verification/hasher"
	"github.com/sei-protocol/sei-chain/tools/utils"
)

type HashScanner struct {
	db             db_engine.StateStore
	latestVersion  int64
	blocksInterval int64
	hashResult     map[string][][]byte
}

func NewHashScanner(db db_engine.StateStore, blocksInterval int64) *HashScanner {
	latestVersion := db.GetLatestVersion()
	fmt.Printf("Detected Pebbledb latest version: %d\n", latestVersion)
	return &HashScanner{
		db:             db,
		latestVersion:  latestVersion,
		blocksInterval: blocksInterval,
		hashResult:     make(map[string][][]byte),
	}
}

func (s *HashScanner) ScanAllModules() {
	for _, moduleName := range utils.Modules {
		result := s.scanAllHeights(moduleName)
		for i, hashResult := range result {
			// Calculate the block range for this hash.
			beginBlockRange := s.blocksInterval * int64(i)
			endBlockRange := s.blocksInterval * (int64(i) + 1)

			fmt.Printf("Module %s block range %d-%d hash is: %X\n", moduleName, beginBlockRange, endBlockRange, hashResult)
		}
	}
}

func (s *HashScanner) scanAllHeights(module string) [][]byte {
	dataCh := make(chan db_engine.RawSnapshotNode, 10000)
	hashCalculator := hasher.NewXorHashCalculator(s.blocksInterval, int(s.latestVersion/s.blocksInterval+1), dataCh)
	fmt.Printf("Starting to scan module: %s\n", module)
	go func() {
		count := 0
		_, err := s.db.RawIterate(module, func(key, value []byte, version int64) bool {
			dataCh <- db_engine.RawSnapshotNode{
				StoreKey: module,
				Key:      key,
				Value:    value,
				Version:  version,
			}

			count++
			if count%1000000 == 0 {
				fmt.Printf("Scanned %d items for module %s\n", count, module)
			}

			return false
		})
		if err != nil {
			panic(fmt.Errorf("RawIterate error: %w", err))
		}
		close(dataCh)
	}()
	allHashes := hashCalculator.ComputeHashes()
	s.hashResult[module] = allHashes
	return allHashes
}
