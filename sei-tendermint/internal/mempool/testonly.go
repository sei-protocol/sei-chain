package mempool

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestConfig() *Config {
	cfg := DefaultConfig()
	cfg.CacheSize = 1000
	cfg.DropUtilisationThreshold = 0.0
	// Disable TTL purging in tests.
	cfg.TTLNumBlocks = utils.None[int64]()
	cfg.TTLDuration = utils.None[time.Duration]()
	return cfg
}
