package iavl

import (
	"bytes"
	"fmt"

	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-chain/tools/hash_verification/hasher"
	"github.com/sei-protocol/sei-chain/tools/utils"
	"github.com/sei-protocol/sei-db/ss/types"
	dbm "github.com/tendermint/tm-db"
)

type HashScanner struct {
	db             dbm.DB
	latestVersion  int64
	blocksInterval int64
	hashResult     map[string][][]byte
}

func NewHashScanner(db dbm.DB, blocksInterval int64) *HashScanner {
	latestVersion := rootmulti.GetLatestVersion(db)
	fmt.Printf("Detected IAVL latest version: %d\n", latestVersion)
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
			fmt.Printf("Module %s height %d hash is: %X\n", moduleName, s.blocksInterval*(int64(i)+1), hashResult)
		}
	}
}

func (s *HashScanner) scanAllHeights(module string) [][]byte {
	dataCh := make(chan types.RawSnapshotNode, 10000)
	hashCalculator := hasher.NewXorHashCalculator(s.blocksInterval, int(s.latestVersion/s.blocksInterval+1), dataCh)
	fmt.Printf("Starting to scan module: %s\n", module)
	go func() {
		prefixDB := dbm.NewPrefixDB(s.db, []byte(utils.BuildRawPrefix(module)))
		itr, err := prefixDB.Iterator(nil, nil)
		count := 0
		if err != nil {
			panic(fmt.Errorf("failed to create iterator: %w", err))
		}
		defer itr.Close()
		for ; itr.Valid(); itr.Next() {
			value := bytes.Clone(itr.Value())
			node, err := iavl.MakeNode(value)
			if err != nil {
				panic(fmt.Errorf("failed to parse iavl node: %w", err))
			}

			// Only scan leaf nodes
			if node.GetHeight() != 0 {
				continue
			}
			snapshotNode := types.RawSnapshotNode{
				StoreKey: module,
				Key:      node.GetNodeKey(),
				Value:    node.GetValue(),
				Version:  node.GetVersion(),
			}
			dataCh <- snapshotNode
			count++
			if count%1000000 == 0 {
				fmt.Printf("Scanned %d items for module %s\n", count, module)
			}
		}
		close(dataCh)
	}()
	allHashes := hashCalculator.ComputeHashes()
	s.hashResult[module] = allHashes
	return allHashes
}
