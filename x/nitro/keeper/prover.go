package keeper

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"github.com/sei-protocol/sei-chain/x/nitro/types"
)

const (
	RightSibling = 1
	LeftSibling  = -1
)

// this function goes over all merkle proof hashes, and check if the merkle root generated is the same as the provided root
func (k *Keeper) Validate(root []byte, proof *types.MerkleProof) error {
	if len(proof.Hash) != len(proof.Direction) {
		return types.ErrValidateMerkleProof
	}

	i := 0
	newHash := []byte(proof.Commitment)
	for i < len(proof.Hash) {
		if proof.Direction[i] == RightSibling {
			newHash = Hash(newHash, []byte(proof.Hash[i]))
		} else {
			newHash = Hash([]byte(proof.Hash[i]), newHash)
		}
		i++
	}

	if !bytes.Equal(root, newHash) {
		return types.ErrInvalidMerkleProof
	}
	return nil
}

func Hash(val1, val2 []byte) []byte {
	sum := sha256.Sum256(append(val1, val2...))
	return sum[:]
}

func AccountToValue(account types.Account) ([]byte, error) {
	value := [sha256.Size]byte{}
	lamportbz := make([]byte, 8)
	binary.BigEndian.PutUint64(lamportbz, account.Lamports)
	rentepochbz := make([]byte, 8)
	binary.BigEndian.PutUint64(rentepochbz, account.RentEpoch)
	databz, err := hex.DecodeString(account.Data)
	if err != nil {
		return nil, err
	}
	value = sha256.Sum256(append(append(lamportbz, rentepochbz...), databz...))

	return value[:], nil
}
