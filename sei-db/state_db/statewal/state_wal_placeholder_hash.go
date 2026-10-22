package statewal

import "crypto/sha256"

// computePlaceholderHash returns the hash of a serialized block: SHA-256 over its serialized mutations, or the zero
// hash for a block in the legacy changeset format.
//
// STOPGAP: this is a placeholder, intentionally not a BUD as the BUD spec defines it.
func computePlaceholderHash(serialized []byte) [32]byte {
	// This placeholder exists so the API that delivers hashes can be built and run on a private testnet before the
	// real BUD lands. It is replaced wholesale then. The iterator recomputes a block's hash from its serialization
	// rather than reading a stored one.
	if len(serialized) == 0 || serialized[0] != mutationFormatVersion {
		return [32]byte{}
	}
	return sha256.Sum256(serialized[1:])
}
