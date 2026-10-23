package statewal

import "crypto/sha256"

// computePlaceholderHash returns the hash of a serialized block: SHA-256 over its serialized mutations. It returns an
// error if the block does not begin with mutationFormatVersion.
//
// STOPGAP: this is a placeholder, intentionally not a BUD as the BUD spec defines it.
func computePlaceholderHash(serialized []byte) ([32]byte, error) {
	// This placeholder exists so the API that delivers hashes can be built and run on a private testnet before the
	// real BUD lands. It is replaced wholesale then. The iterator recomputes a block's hash from its serialization
	// rather than reading a stored one.
	if err := requireMutationFormat(serialized); err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(serialized[1:]), nil
}
