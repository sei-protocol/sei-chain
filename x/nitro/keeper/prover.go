package keeper

import (
	"bytes"
	"crypto/sha256"

	"github.com/sei-protocol/sei-chain/x/nitro/types"
)

const (
	RIGHT_SIBLING = 1
	LEFT_SIBLING = -1
)

// this function goes over all merkle proof hashes, and check if the merkle root generated is the same as the provided root
func (k *Keeper) Validate(root []byte, proof *types.MerkleProof) error {
	if len(proof.Hash) != len(proof.Direction) {
		return types.ErrValidateMerkleProof
	}

	i := 0
	newHash := []byte(proof.Commitment)
	for i < len(proof.Hash) {
		if proof.Direction[i] == RIGHT_SIBLING {
			newHash = Hash(newHash, []byte(proof.Hash[i]))
		} else {
			newHash = Hash([]byte(proof.Hash[i]), newHash)
		}
		i++
	}

	if !bytes.Equal(root, newHash) {
		return types.ErrInvalidateMerkleProof
	}
	return nil
}

func Hash(val1, val2 []byte) []byte {
	sum := sha256.Sum256(append(val1, val2...))
	return sum[:]
}
