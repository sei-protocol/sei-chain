package blocksim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
)

const (
	minHashSize         = 20
	minCannedRandomSize = unit.MB
)

// Configuration for the blocksim benchmark.
type BlocksimConfig struct {

	// The size of each simulated transaction, in bytes. Each transaction in a block will contain
	// this many bytes of random data.
	BytesPerTransaction uint64

	// The number of transactions included in each generated block.
	TransactionsPerBlock uint64

	// Additional bytes of random data added to the block itself, beyond the transaction data. This
	// simulates block-level metadata or other non-transaction payload.
	ExtraBytesPerBlock uint64

	// The size of each block hash, in bytes.
	BlockHashSize uint64

	// The size of each transaction hash, in bytes.
	TransactionHashSize uint64

	// The capacity of the queue that holds generated blocks before they are consumed by the
	// benchmark. A larger queue allows the block generator to run further ahead of the consumer.
	StagedBlockQueueSize uint64

	// The size of the CannedRandom buffer, in bytes. Altering this value for a pre-existing run
	// will change the random data generated, don't change it unless you are starting a new run
	// from scratch.
	CannedRandomSize uint64

	// The number of blocks to keep in the database after pruning.
	UnprunedBlocks uint64
}

// Returns the default configuration for the blocksim benchmark.
func DefaultBlocksimConfig() *BlocksimConfig {
	return &BlocksimConfig{
		BytesPerTransaction:  512,
		TransactionsPerBlock: 1024,
		ExtraBytesPerBlock:   256,
		BlockHashSize:        32,
		TransactionHashSize:  32,
		StagedBlockQueueSize: 8,
		CannedRandomSize:     unit.GB,
		UnprunedBlocks:       100_000,
	}
}

// StringifiedConfig returns the config as human-readable, multi-line JSON.
func (c *BlocksimConfig) StringifiedConfig() (string, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Validate checks that the configuration is sane and returns an error if not.
func (c *BlocksimConfig) Validate() error {
	if c.BytesPerTransaction < 1 {
		return fmt.Errorf("BytesPerTransaction must be at least 1 (got %d)", c.BytesPerTransaction)
	}
	if c.TransactionsPerBlock < 1 {
		return fmt.Errorf("TransactionsPerBlock must be at least 1 (got %d)", c.TransactionsPerBlock)
	}
	if c.BlockHashSize < minHashSize {
		return fmt.Errorf("BlockHashSize must be at least %d (got %d)", minHashSize, c.BlockHashSize)
	}
	if c.TransactionHashSize < minHashSize {
		return fmt.Errorf("TransactionHashSize must be at least %d (got %d)", minHashSize, c.TransactionHashSize)
	}
	if c.StagedBlockQueueSize < 1 {
		return fmt.Errorf("StagedBlockQueueSize must be at least 1 (got %d)", c.StagedBlockQueueSize)
	}
	if c.CannedRandomSize < minCannedRandomSize {
		return fmt.Errorf("CannedRandomSize must be at least %d (got %d)",
			minCannedRandomSize, c.CannedRandomSize)
	}
	if c.UnprunedBlocks < 1 {
		return fmt.Errorf("UnprunedBlocks must be at least 1 (got %d)", c.UnprunedBlocks)
	}
	return nil
}

// LoadConfigFromFile parses a JSON config file at the given path.
// Returns defaults with file values overlaid. Fails if the file contains
// unrecognized configuration keys.
func LoadConfigFromFile(path string) (*BlocksimConfig, error) {
	cfg := DefaultBlocksimConfig()
	//nolint:gosec // G304 - path comes from CLI arg, filepath.Clean used to mitigate traversal
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open config file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Printf("failed to close config file: %v\n", err)
		}
	}()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}
