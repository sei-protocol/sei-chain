package types

import (
	"fmt"
	"math"

	"github.com/klauspost/compress/s2"
)

// s2WorstCaseExpansion bounds the number of bytes by which s2.Encode's output can exceed its input.
// S2 prefixes a block with a varint of the decompressed length (<=5 bytes for any input <= 4 GiB) and,
// for incompressible input, emits it as one literal run with a header (<=5 bytes); the true worst case
// is ~10 bytes. s2.Encode panics (ErrTooLarge) rather than return output larger than MaxUint32, so a
// value within this many bytes of the addressable ceiling must be stored uncompressed. Rounded up from
// the true bound for margin; TestS2MaxCompressibleSizeIsSafe pins it to the library's real behavior.
const s2WorstCaseExpansion = 16

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

// MaxCompressibleSize returns the largest input length this algorithm will compress. Inputs longer than
// this are stored uncompressed because the algorithm's worst-case output could reach the uint32
// addressable value ceiling (for S2, Encode panics rather than emit such output). CompressionNone
// imposes no limit of its own.
func (a CompressionAlgorithm) MaxCompressibleSize() uint64 {
	switch a {
	case CompressionS2:
		return math.MaxUint32 - s2WorstCaseExpansion
	default:
		return math.MaxUint64
	}
}

// EncodeValue returns the on-disk representation of a value for a compressed segment: a one-byte
// algorithm tag followed by the value body. The tag records how the body was encoded, so each value is
// self-describing and decodes independently of the segment's configured algorithm.
//
// The body is the smaller of the compressed and raw forms (never store an expanded blob), and the value
// is stored raw whenever compression is disabled, the input exceeds the algorithm's MaxCompressibleSize
// (so Compress is never called on an unencodable input), or compression does not shrink it. The caller
// must not mutate src.
func EncodeValue(algorithm CompressionAlgorithm, src []byte) ([]byte, error) {
	if algorithm != CompressionNone && uint64(len(src)) <= algorithm.MaxCompressibleSize() {
		compressed, err := Compress(algorithm, src)
		if err != nil {
			return nil, err
		}
		if len(compressed) < len(src) {
			return frameValue(algorithm, compressed), nil
		}
	}
	return frameValue(CompressionNone, src), nil
}

// DecodeValue reverses EncodeValue: it reads the leading algorithm tag and returns the decompressed
// body. The caller must not mutate src or the returned slice.
func DecodeValue(src []byte) ([]byte, error) {
	if len(src) == 0 {
		return nil, fmt.Errorf("cannot decode value: missing algorithm tag byte")
	}
	return Decompress(CompressionAlgorithm(src[0]), src[1:])
}

// frameValue builds the [algorithm-tag][body] on-disk blob.
func frameValue(algorithm CompressionAlgorithm, body []byte) []byte {
	blob := make([]byte, 1+len(body))
	blob[0] = byte(algorithm)
	copy(blob[1:], body)
	return blob
}

// Compress returns the compressed form of src using the given algorithm. For CompressionNone it returns
// src unchanged (the result may alias src). The caller must not mutate src or the returned slice.
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
// same algorithm. For CompressionNone it returns src unchanged (the result may alias src). The caller
// must not mutate src or the returned slice.
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
