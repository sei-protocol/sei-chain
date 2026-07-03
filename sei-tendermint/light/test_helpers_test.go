package light_test

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func mustGenesisChainID(cfg *config.Config) string {
	genDoc, err := types.GenesisDocFromFile(cfg.GenesisFile())
	if err != nil {
		panic(err)
	}
	return genDoc.ChainID
}
