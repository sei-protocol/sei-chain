package receipthash

import "fmt"

// ReceiptHasherConfig configures a ReceiptHasher.
type ReceiptHasherConfig struct {

	// The number of blocks whose receipts may wait to be hashed before Submit() blocks. Higher values absorb longer
	// stalls, at the cost of memory.
	BufferSize int
}

// DefaultReceiptHasherConfig returns a ReceiptHasherConfig holding every default.
func DefaultReceiptHasherConfig() ReceiptHasherConfig {
	return ReceiptHasherConfig{
		BufferSize: 16,
	}
}

// Validate returns an error if the configuration is invalid.
func (c *ReceiptHasherConfig) Validate() error {
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be positive, got %d", c.BufferSize)
	}
	return nil
}
