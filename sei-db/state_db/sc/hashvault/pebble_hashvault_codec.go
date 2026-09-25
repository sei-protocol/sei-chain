package hashvault

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
)

// Wire format
//
// Hash entries live under keys "h" <height>, where <height> is fixed-width 20-digit zero-padded
// ASCII decimal (twenty digits is the exact width of math.MaxUint64). Fixed width is what makes
// Pebble's lexicographic key order match numeric height order, which is what lets Prune wipe a
// contiguous range with a single DeleteRange. ASCII (vs binary BE) is chosen so a raw Pebble dump
// shows recognizable numbers (e.g. "h00000000000000000042") rather than opaque bytes.
//
// Each hash entry's value is the raw block hash followed by a 32-byte SHA-256 trailer computed
// over (be-uint64 height || hash). The trailer is what protects against the validator
// double-voting after silent corruption: the height is folded into the SHA so a stale entry
// returned from the wrong key path also fails verification. The trailer's height encoding is
// binary BE because that representation never appears on disk in raw form (it's consumed entirely
// by the SHA), so the human-readability argument doesn't apply there.
//
// The prune boundary lives under the single key "prune_boundary". Its value is the boundary
// height as variable-width unpadded ASCII decimal (e.g. "5" or "18446744073709551615"). No
// padding because there's only one such row and no range scan to satisfy. No checksum: the
// boundary is GC bookkeeping, a silent flip is not slashable, and Pebble's own block-level CRC
// catches bit-rot in normal operation.
//
// "h" and "prune_boundary" have disjoint first bytes ('h' vs 'p'), so the two namespaces can never
// alias regardless of what digits follow the hash prefix.

const checksumSize = sha256.Size

// heightDigits is the on-disk width (in ASCII bytes) of every encoded height. math.MaxUint64 is
// 18446744073709551615, exactly 20 digits.
const heightDigits = 20

var (
	hashKeyPrefix    = []byte("h")
	pruneBoundaryKey = []byte("prune_boundary")
)

// hashKey returns the Pebble key for the given block height: hashKeyPrefix followed by the height
// as 20-digit zero-padded ASCII decimal.
func hashKey(height uint64) []byte {
	out := make([]byte, 0, len(hashKeyPrefix)+heightDigits)
	out = append(out, hashKeyPrefix...)
	return appendHeight(out, height)
}

// decodeHashKey is the inverse of hashKey: validates the length and prefix, then parses the
// trailing decimal digits. Returns ErrCorruption on any malformedness.
func decodeHashKey(key []byte) (uint64, error) {
	if len(key) != len(hashKeyPrefix)+heightDigits {
		return 0, fmt.Errorf("%w: unexpected hash key length %d", ErrCorruption, len(key))
	}
	if !bytes.HasPrefix(key, hashKeyPrefix) {
		return 0, fmt.Errorf("%w: hash key missing prefix", ErrCorruption)
	}
	return parseHeight(key[len(hashKeyPrefix):])
}

// hashKeyUpperBound returns an end-exclusive Pebble key that is strictly greater than every key
// hashKey can produce (i.e. up to and including hashKey(math.MaxUint64)). Safe to use as an
// IterOptions.UpperBound or as the upper end of a DeleteRange covering the entire hash namespace.
func hashKeyUpperBound() []byte {
	// One byte longer than any valid hash key, so lex-greater than all of them.
	return append(hashKey(math.MaxUint64), 0x00)
}

// encodeHashValue returns the on-disk value for the given (height, hash) pair: the hash bytes
// followed by SHA-256(be(height) || hash).
func encodeHashValue(height uint64, hash []byte) []byte {
	out := make([]byte, 0, len(hash)+checksumSize)
	out = append(out, hash...)
	out = append(out, hashChecksum(height, hash)...)
	return out
}

// decodeHashValue verifies the trailing SHA-256 of raw against (height, hash[:len(raw)-32]) and
// returns the hash bytes on success. Returns ErrCorruption if the trailer is missing or wrong.
func decodeHashValue(height uint64, raw []byte) ([]byte, error) {
	if len(raw) < checksumSize {
		return nil, fmt.Errorf("%w: value too short for height %d (%d bytes)", ErrCorruption, height, len(raw))
	}
	split := len(raw) - checksumSize
	hash := raw[:split]
	trailer := raw[split:]
	expected := hashChecksum(height, hash)
	if !bytes.Equal(trailer, expected) {
		return nil, fmt.Errorf("%w: checksum mismatch for height %d", ErrCorruption, height)
	}
	return bytes.Clone(hash), nil
}

// encodeBoundaryValue returns the on-disk value for the prune boundary: variable-width unpadded
// ASCII decimal. There's only one boundary row in the DB and nothing range-scans the value, so
// fixed-width padding (as used for keys) buys nothing here.
func encodeBoundaryValue(boundary uint64) []byte {
	return strconv.AppendUint(nil, boundary, 10)
}

// decodeBoundaryValue parses an ASCII-decimal boundary value. Empty/oversized/non-digit inputs all
// trip ErrCorruption; Pebble's own CRC handles bit-rot within an otherwise-valid value.
func decodeBoundaryValue(raw []byte) (uint64, error) {
	// math.MaxUint64 is 20 digits; anything longer can't be a valid uint64 and is suspect.
	if len(raw) == 0 || len(raw) > heightDigits {
		return 0, fmt.Errorf("%w: unexpected boundary value length %d", ErrCorruption, len(raw))
	}
	n, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid boundary digits %q: %v", ErrCorruption, raw, err)
	}
	return n, nil
}

// hashChecksum returns SHA-256(be(height) || hash). The height is encoded as binary BE here, not
// ASCII, because the result is hashed in place and never appears on disk in raw form.
func hashChecksum(height uint64, hash []byte) []byte {
	h := sha256.New()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], height)
	_, _ = h.Write(buf[:])
	_, _ = h.Write(hash)
	return h.Sum(nil)
}

// appendHeight appends 20-digit zero-padded decimal to dst and returns the result.
func appendHeight(dst []byte, height uint64) []byte {
	var buf [heightDigits]byte
	i := len(buf)
	for height > 0 {
		i--
		buf[i] = byte('0' + height%10)
		height /= 10
	}
	for i > 0 {
		i--
		buf[i] = '0'
	}
	return append(dst, buf[:]...)
}

// parseHeight parses exactly heightDigits decimal bytes into a uint64. Returns ErrCorruption on
// any non-digit byte; uint64 cannot overflow because 20 digits is the exact width of math.MaxUint64
// and ParseUint with bitSize=64 rejects values above MaxUint64.
func parseHeight(raw []byte) (uint64, error) {
	if len(raw) != heightDigits {
		return 0, fmt.Errorf("%w: expected %d digits, got %d", ErrCorruption, heightDigits, len(raw))
	}
	n, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid height digits %q: %v", ErrCorruption, raw, err)
	}
	return n, nil
}
