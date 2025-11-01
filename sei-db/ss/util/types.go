package util

// HashCalculator defines the interface for calculating chained state hash.
type HashCalculator interface {
	HashSingle(data []byte) []byte
	HashTwo(dataA []byte, dataB []byte) []byte
	ComputeHashes() [][]byte
}
