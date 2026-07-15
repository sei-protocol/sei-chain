package types

import (
	"fmt"

	"github.com/klauspost/compress/s2"
)

// CompressionAlgorithm identifies the algorithm used to compress values before they are written to a
// segment's value file. It is stored (as a single byte) in each segment's metadata file so that a
// segment is always decompressed with the algorithm it was written with, independent of the table's
// current configuration.
//
// The zero value is CompressionNone, so a table or segment that never opts in behaves exactly as an
// uncompressed one.
type CompressionAlgorithm uint8

const (
	// CompressionNone means values are stored verbatim, with no compression. This is the default.
	CompressionNone CompressionAlgorithm = 0

	// CompressionS2 compresses each value with the S2 block codec (github.com/klauspost/compress/s2),
	// a high-throughput, Snappy-compatible algorithm. The encoded block is self-describing: the
	// decompressed length is recoverable from the block itself, so it is not stored separately.
	CompressionS2 CompressionAlgorithm = 1
)

// Validate returns an error if the algorithm is not a value this build understands. It is called both
// when validating table configuration and when deserializing segment metadata, so an unknown byte in a
// metadata file (e.g. one written by a newer build) is reported rather than silently mishandled.
func (a CompressionAlgorithm) Validate() error {
	switch a {
	case CompressionNone, CompressionS2:
		return nil
	default:
		return fmt.Errorf("unknown compression algorithm: %d", uint8(a))
	}
}

// String returns a human-readable name for the algorithm.
func (a CompressionAlgorithm) String() string {
	switch a {
	case CompressionNone:
		return "none"
	case CompressionS2:
		return "s2"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(a))
	}
}

// Compress returns the compressed form of src using the given algorithm. For CompressionNone it returns
// src unchanged. The returned slice is a freshly allocated buffer that does not alias src.
func Compress(algorithm CompressionAlgorithm, src []byte) ([]byte, error) {
	switch algorithm {
	case CompressionNone:
		return src, nil
	case CompressionS2:
		return s2.Encode(nil, src), nil
	default:
		return nil, fmt.Errorf("cannot compress with unknown algorithm: %d", uint8(algorithm))
	}
}

// Decompress returns the decompressed form of src, which must have been produced by Compress with the
// same algorithm. For CompressionNone it returns src unchanged.
func Decompress(algorithm CompressionAlgorithm, src []byte) ([]byte, error) {
	switch algorithm {
	case CompressionNone:
		return src, nil
	case CompressionS2:
		decompressed, err := s2.Decode(nil, src)
		if err != nil {
			return nil, fmt.Errorf("failed to s2-decode value: %w", err)
		}
		return decompressed, nil
	default:
		return nil, fmt.Errorf("cannot decompress with unknown algorithm: %d", uint8(algorithm))
	}
}
