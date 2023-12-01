package memiavl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	ics23 "github.com/confio/ics23/go"
	"github.com/cosmos/iavl"
)

/*
GetMembershipProof will produce a CommitmentProof that the given key (and queries value) exists in the iavl tree.
If the key doesn't exist in the tree, this will return an error.
*/
func (t *Tree) GetMembershipProof(key []byte) (*ics23.CommitmentProof, error) {
	exist, err := t.createExistenceProof(key)
	if err != nil {
		return nil, err
	}
	proof := &ics23.CommitmentProof{
		Proof: &ics23.CommitmentProof_Exist{
			Exist: exist,
		},
	}
	return proof, nil
}

// VerifyMembership returns true iff proof is an ExistenceProof for the given key.
func (t *Tree) VerifyMembership(proof *ics23.CommitmentProof, key []byte) bool {
	val := t.Get(key)
	root := t.RootHash()
	return ics23.VerifyMembership(ics23.IavlSpec, root, proof, key, val)
}

/*
GetNonMembershipProof will produce a CommitmentProof that the given key doesn't exist in the iavl tree.
If the key exists in the tree, this will return an error.
*/
func (t *Tree) GetNonMembershipProof(key []byte) (*ics23.CommitmentProof, error) {
	// idx is one node right of what we want....
	var err error
	idx, val := t.GetWithIndex(key)
	if val != nil {
		return nil, fmt.Errorf("cannot create NonExistanceProof when Key in State")
	}

	nonexist := &ics23.NonExistenceProof{
		Key: key,
	}

	if idx >= 1 {
		leftkey, _ := t.GetByIndex(idx - 1)
		nonexist.Left, err = t.createExistenceProof(leftkey)
		if err != nil {
			return nil, err
		}
	}

	// this will be nil if nothing right of the queried key
	rightkey, _ := t.GetByIndex(idx)
	if rightkey != nil {
		nonexist.Right, err = t.createExistenceProof(rightkey)
		if err != nil {
			return nil, err
		}
	}

	proof := &ics23.CommitmentProof{
		Proof: &ics23.CommitmentProof_Nonexist{
			Nonexist: nonexist,
		},
	}
	return proof, nil
}

// VerifyNonMembership returns true iff proof is a NonExistenceProof for the given key.
func (t *Tree) VerifyNonMembership(proof *ics23.CommitmentProof, key []byte) bool {
	root := t.RootHash()
	return ics23.VerifyNonMembership(ics23.IavlSpec, root, proof, key)
}

// createExistenceProof will get the proof from the tree and convert the proof into a valid
// existence proof, if that's what it is.
func (t *Tree) createExistenceProof(key []byte) (*ics23.ExistenceProof, error) {
	path, node, err := pathToLeaf(t.root, key)
	return &ics23.ExistenceProof{
		Key:   node.Key(),
		Value: node.Value(),
		Leaf:  convertLeafOp(int64(node.Version())),
		Path:  convertInnerOps(path),
	}, err
}

func convertLeafOp(version int64) *ics23.LeafOp {
	// this is adapted from iavl/proof.go:proofLeafNode.Hash()
	prefix := convertVarIntToBytes(0)
	prefix = append(prefix, convertVarIntToBytes(1)...)
	prefix = append(prefix, convertVarIntToBytes(version)...)

	return &ics23.LeafOp{
		Hash:         ics23.HashOp_SHA256,
		PrehashValue: ics23.HashOp_SHA256,
		Length:       ics23.LengthOp_VAR_PROTO,
		Prefix:       prefix,
	}
}

// we cannot get the proofInnerNode type, so we need to do the whole path in one function
func convertInnerOps(path iavl.PathToLeaf) []*ics23.InnerOp {
	steps := make([]*ics23.InnerOp, 0, len(path))

	// lengthByte is the length prefix prepended to each of the sha256 sub-hashes
	var lengthByte byte = 0x20

	// we need to go in reverse order, iavl starts from root to leaf,
	// we want to go up from the leaf to the root
	for i := len(path) - 1; i >= 0; i-- {
		// this is adapted from iavl/proof.go:proofInnerNode.Hash()
		prefix := convertVarIntToBytes(int64(path[i].Height))
		prefix = append(prefix, convertVarIntToBytes(path[i].Size)...)
		prefix = append(prefix, convertVarIntToBytes(path[i].Version)...)

		var suffix []byte
		if len(path[i].Left) > 0 {
			// length prefixed left side
			prefix = append(prefix, lengthByte)
			prefix = append(prefix, path[i].Left...)
			// prepend the length prefix for child
			prefix = append(prefix, lengthByte)
		} else {
			// prepend the length prefix for child
			prefix = append(prefix, lengthByte)
			// length-prefixed right side
			suffix = []byte{lengthByte}
			suffix = append(suffix, path[i].Right...)
		}

		op := &ics23.InnerOp{
			Hash:   ics23.HashOp_SHA256,
			Prefix: prefix,
			Suffix: suffix,
		}
		steps = append(steps, op)
	}
	return steps
}

func convertVarIntToBytes(orig int64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutVarint(buf[:], orig)
	return buf[:n]
}

func pathToLeaf(node Node, key []byte) (iavl.PathToLeaf, Node, error) {
	var path iavl.PathToLeaf

	for {
		height := node.Height()
		if height == 0 {
			if bytes.Equal(node.Key(), key) {
				return path, node, nil
			}

			return path, node, errors.New("key does not exist")
		}

		if bytes.Compare(key, node.Key()) < 0 {
			// left side
			right := node.Right()
			path = append(path, iavl.ProofInnerNode{
				Height:  int8(height),
				Size:    node.Size(),
				Version: int64(node.Version()),
				Left:    nil,
				Right:   right.Hash(),
			})
			node = node.Left()
			continue
		}

		// right side
		left := node.Left()
		path = append(path, iavl.ProofInnerNode{
			Height:  int8(height),
			Size:    node.Size(),
			Version: int64(node.Version()),
			Left:    left.Hash(),
			Right:   nil,
		})
		node = node.Right()
	}
}
