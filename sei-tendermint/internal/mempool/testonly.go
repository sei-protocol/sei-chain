package mempool

func TestConfig() *Config {
	cfg := DefaultConfig()
	cfg.CacheSize = 1000
	cfg.DropUtilisationThreshold = 0.0
	// Disable TTL purging in tests: the mempool starts at height=-1, so txs
	// added before the first block get height=-1. With TTLNumBlocks>0,
	// purgeExpiredTxs would instantly evict them on the first Update call
	// when blockHeight is large (e.g. random InitialHeight).
	cfg.TTLNumBlocks = 0
	cfg.TTLDuration = 0
	return cfg
}
