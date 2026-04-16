package mempool

func TestConfig() *Config {
	cfg := DefaultConfig()
	cfg.CacheSize = 1000
	cfg.DropUtilisationThreshold = 0.0
	// Disable TTL purging in tests.
	cfg.TTLNumBlocks = 0
	cfg.TTLDuration = 0
	return cfg
}
