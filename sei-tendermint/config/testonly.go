package config

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestLoadGenesis(cfg *Config) *types.GenesisDoc {
	return utils.OrPanic1(types.GenesisDocFromFile(cfg.GenesisFile()))
}
