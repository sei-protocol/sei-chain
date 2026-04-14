package mempool

func TestConfig() *Config {
	cfg := DefaultConfig()
	cfg.CacheSize = 1000
	cfg.DropUtilisationThreshold = 0.0
	return cfg
}
