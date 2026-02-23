package cryptosim

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// Hash64 returns a well-distributed 64-bit hash of x.
// It implements the SplitMix64 finalizer, a fast non-cryptographic mixing
// function with excellent avalanche properties. It is suitable for hash tables,
// sharding, randomized iteration, and benchmarks, but it is NOT
// cryptographically secure.
//
// The function is a bijection over uint64 (no collisions as a mapping).
//
// References:
//   - Steele, Lea, Flood. "Fast Splittable Pseudorandom Number Generators"
//     (OOPSLA 2014): https://doi.org/10.1145/2660193.2660195
//   - Public domain reference implementation:
//     http://xorshift.di.unimi.it/splitmix64.c
func Hash64(x int64) int64 {
	z := uint64(x)
	z += 0x9e3779b97f4a7c15
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z = z ^ (z >> 31)
	return int64(z)
}

// PositiveHash64 returns the absolute value of Hash64(x). It never returns a negative value.
// When Hash64(x) is math.MinInt64, returns math.MaxInt64 since the true absolute value does not fit in int64.
func PositiveHash64(x int64) int64 {
	result := Hash64(x)
	if result == math.MinInt64 {
		return math.MaxInt64
	}
	if result < 0 {
		return -result
	}
	return result
}

// resolveAndCreateDataDir expands ~ to the home directory and creates the directory if it doesn't exist.
func resolveAndCreateDataDir(dataDir string) (string, error) {
	if dataDir == "~" || strings.HasPrefix(dataDir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		if dataDir == "~" {
			dataDir = home
		} else {
			dataDir = filepath.Join(home, dataDir[2:])
		}
	}
	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create data directory: %w", err)
		}
	}
	return dataDir, nil
}
