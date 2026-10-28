package lthash

import "fmt"

// Config configures a HashEngine. The two queue sizes bound how far hashing may fall behind.
type Config struct {
	// CombineQueueSize is how many scheduled blocks may be waiting for the combiner before ScheduleHash()
	// blocks. Each costs the memory of its changed values.
	CombineQueueSize uint32

	// HashChanSize is the depth of the channel AwaitHash() reads from. Headroom for a consumer that reads
	// later than it schedules; one that stops reading entirely stalls the engine.
	HashChanSize uint32

	// ChunkSize is how many KV pairs one leaf-hash task carries. Splitting a module's pairs into
	// fixed-size chunks lets a single large module, such as the EVM storage database in a big block, fan
	// out across many workers instead of pinning one.
	ChunkSize uint32
}

// DefaultConfig returns the default HashEngine configuration.
func DefaultConfig() *Config {
	return &Config{
		CombineQueueSize: 8,
		HashChanSize:     1024,
		ChunkSize:        128,
	}
}

// Validate reports whether this configuration can be used to build an engine.
func (c *Config) Validate() error {
	if c.CombineQueueSize == 0 {
		return fmt.Errorf("combine queue size must be greater than 0")
	}
	if c.HashChanSize == 0 {
		return fmt.Errorf("hash chan size must be greater than 0")
	}
	if c.ChunkSize == 0 {
		return fmt.Errorf("chunk size must be greater than 0")
	}
	return nil
}
