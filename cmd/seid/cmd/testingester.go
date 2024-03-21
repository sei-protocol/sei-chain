package cmd

import (
	"encoding/json"
	"fmt"
	"math/big"

	"os"

	ethtests "github.com/ethereum/go-ethereum/tests"
)

func testIngester(testFilePath string) *ethtests.BlockTest {
	// Read the entire file content
	file, err := os.Open(testFilePath)
	if err != nil {
		panic(err)
	}
	var tests map[string]ethtests.BlockTest
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&tests)
	if err != nil {
		panic(err)
	}
	for name, bt := range tests {
		btP := &bt
		if name == "SimpleTx_Shanghai" {
			if len(btP.Json.Blocks) > 0 {
				if btP.Json.Blocks[0].BlockHeader.Number.Cmp(big.NewInt(1)) == 0 {
					for _, block := range btP.Json.Blocks {
						fmt.Println("in testIngester, replacing block number")
						block.BlockHeader.Number = block.BlockHeader.Number.Add(block.BlockHeader.Number, big.NewInt(1))
					}
				}
			}
			return btP
		}
	}
	fmt.Println("Returning empty block test")
	return emptyBlockTest
}
