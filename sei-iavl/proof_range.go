package iavl

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"

	iavlproto "github.com/cosmos/iavl/proto"
)

type RangeProof struct {
	// You don't need the right path because
	// it can be derived from what we have.
	LeftPath   PathToLeaf      `json:"left_path"`
	InnerNodes []PathToLeaf    `json:"inner_nodes"`
	Leaves     []ProofLeafNode `json:"leaves"`

	// memoize
	rootHash     []byte // valid iff rootVerified is true
	rootVerified bool
	treeEnd      bool // valid iff rootVerified is true
}

// Keys returns all the keys in the RangeProof.  NOTE: The keys here may
// include more keys than provided by tree.GetRangeWithProof or
// MutableTree.GetVersionedRangeWithProof.  The keys returned there are only
// in the provided [startKey,endKey){limit} range.  The keys returned here may
// include extra keys, such as:
// - the key before startKey if startKey is provided and doesn't exist;
// - the key after a queried key with tree.GetWithProof, when the key is absent.
func (proof *RangeProof) Keys() (keys [][]byte) {
	if proof == nil {
		return nil
	}
	for _, leaf := range proof.Leaves {
		keys = append(keys, leaf.Key)
	}
	return keys
}

// String returns a string representation of the proof.
func (proof *RangeProof) String() string {
	if proof == nil {
		return "<nil-RangeProof>"
	}
	return proof.StringIndented("")
}

func (proof *RangeProof) StringIndented(indent string) string {
	istrs := make([]string, 0, len(proof.InnerNodes))
	for _, ptl := range proof.InnerNodes {
		istrs = append(istrs, ptl.stringIndented(indent+"    "))
	}
	lstrs := make([]string, 0, len(proof.Leaves))
	for _, leaf := range proof.Leaves {
		lstrs = append(lstrs, leaf.stringIndented(indent+"    "))
	}
	return fmt.Sprintf(`RangeProof{
%s  LeftPath: %v
%s  InnerNodes:
%s    %v
%s  Leaves:
%s    %v
%s  (rootVerified): %v
%s  (rootHash): %X
%s  (treeEnd): %v
%s}`,
		indent, proof.LeftPath.stringIndented(indent+"  "),
		indent,
		indent, strings.Join(istrs, "\n"+indent+"    "),
		indent,
		indent, strings.Join(lstrs, "\n"+indent+"    "),
		indent, proof.rootVerified,
		indent, proof.rootHash,
		indent, proof.treeEnd,
		indent)
}

// The index of the first leaf (of the whole tree).
// Returns -1 if the proof is nil.
func (proof *RangeProof) LeftIndex() int64 {
	if proof == nil {
		return -1
	}
	return proof.LeftPath.Index()
}

// Also see LeftIndex().
// Verify that a key has some value.
// Does not assume that the proof itself is valid, call Verify() first.
func (proof *RangeProof) VerifyItem(key, value []byte) error {
	if proof == nil {
		return errors.Wrap(ErrInvalidProof, "proof is nil")
	}

	if !proof.rootVerified {
		return errors.New("must call Verify(root) first")
	}

	leaves := proof.Leaves
	i := sort.Search(len(leaves), func(i int) bool {
		return bytes.Compare(key, leaves[i].Key) <= 0
	})

	if i >= len(leaves) || !bytes.Equal(leaves[i].Key, key) {
		return errors.Wrap(ErrInvalidProof, "leaf key not found in proof")
	}

	h := sha256.Sum256(value)
	valueHash := h[:]
	if !bytes.Equal(leaves[i].ValueHash, valueHash) {
		return errors.Wrap(ErrInvalidProof, "leaf value hash not same")
	}

	return nil
}

// Verify that proof is valid absence proof for key.
// Does not assume that the proof itself is valid.
// For that, use Verify(root).
func (proof *RangeProof) VerifyAbsence(key []byte) error {
	if proof == nil {
		return errors.Wrap(ErrInvalidProof, "proof is nil")
	}
	if !proof.rootVerified {
		return errors.New("must call Verify(root) first")
	}
	cmp := bytes.Compare(key, proof.Leaves[0].Key)
	if cmp < 0 {
		if proof.LeftPath.isLeftmost() {
			return nil
		}
		return errors.New("absence not proved by left path")

	} else if cmp == 0 {
		return errors.New("absence disproved via first item #0")
	}
	if len(proof.LeftPath) == 0 {
		return nil // proof ok
	}
	if proof.LeftPath.isRightmost() {
		return nil
	}

	// See if any of the leaves are greater than key.
	for i := 1; i < len(proof.Leaves); i++ {
		leaf := proof.Leaves[i]
		cmp := bytes.Compare(key, leaf.Key)
		switch {
		case cmp < 0:
			return nil // proof ok
		case cmp == 0:
			return fmt.Errorf("absence disproved via item #%v", i)
		default:
			// if i == len(proof.Leaves)-1 {
			// If last item, check whether
			// it's the last item in the tree.

			// }
			continue
		}
	}

	// It's still a valid proof if our last leaf is the rightmost child.
	if proof.treeEnd {
		return nil // OK!
	}

	// It's not a valid absence proof.
	if len(proof.Leaves) < 2 {
		return errors.New("absence not proved by right leaf (need another leaf?)")
	}
	return errors.New("absence not proved by right leaf")

}

// Verify that proof is valid.
func (proof *RangeProof) Verify(root []byte) error {
	if proof == nil {
		return errors.Wrap(ErrInvalidProof, "proof is nil")
	}
	err := proof.verify(root)
	return err
}

func (proof *RangeProof) verify(root []byte) (err error) {
	rootHash := proof.rootHash
	if rootHash == nil {
		derivedHash, err := proof.computeRootHash()
		if err != nil {
			return err
		}
		rootHash = derivedHash
	}
	if !bytes.Equal(rootHash, root) {
		return errors.Wrap(ErrInvalidRoot, "root hash doesn't match")
	}
	proof.rootVerified = true
	return nil
}

// ComputeRootHash computes the root hash with leaves.
// Returns nil if error or proof is nil.
// Does not verify the root hash.
func (proof *RangeProof) ComputeRootHash() []byte {
	if proof == nil {
		return nil
	}

	rootHash, _ := proof.computeRootHash()

	return rootHash
}

func (proof *RangeProof) computeRootHash() (rootHash []byte, err error) {
	rootHash, treeEnd, err := proof._computeRootHash()
	if err == nil {
		proof.rootHash = rootHash // memoize
		proof.treeEnd = treeEnd   // memoize
	}
	return rootHash, err
}

func (proof *RangeProof) _computeRootHash() (rootHash []byte, treeEnd bool, err error) {
	if len(proof.Leaves) == 0 {
		return nil, false, errors.Wrap(ErrInvalidProof, "no leaves")
	}
	if len(proof.InnerNodes)+1 != len(proof.Leaves) {
		return nil, false, errors.Wrap(ErrInvalidProof, "InnerNodes vs Leaves length mismatch, leaves should be 1 more.") //nolint:revive
	}

	// Start from the left path and prove each leaf.

	// shared across recursive calls
	var leaves = proof.Leaves
	var innersq = proof.InnerNodes
	var COMPUTEHASH func(path PathToLeaf, rightmost bool) (hash []byte, treeEnd bool, done bool, err error)

	// rightmost: is the root a rightmost child of the tree?
	// treeEnd: true iff the last leaf is the last item of the tree.
	// Returns the (possibly intermediate, possibly root) hash.
	COMPUTEHASH = func(path PathToLeaf, rightmost bool) (hash []byte, treeEnd bool, done bool, err error) {

		// Pop next leaf.
		nleaf, rleaves := leaves[0], leaves[1:]
		leaves = rleaves

		// Compute hash.
		hash, err = (pathWithLeaf{
			Path: path,
			Leaf: nleaf,
		}).computeRootHash()

		if err != nil {
			return nil, treeEnd, false, err
		}

		// If we don't have any leaves left, we're done.
		if len(leaves) == 0 {
			rightmost = rightmost && path.isRightmost()
			return hash, rightmost, true, nil
		}

		// Prove along path (until we run out of leaves).
		for len(path) > 0 {

			// Drop the leaf-most (last-most) inner nodes from path
			// until we encounter one with a left hash.
			// We assume that the left side is already verified.
			// rpath: rest of path
			// lpath: last path item
			rpath, lpath := path[:len(path)-1], path[len(path)-1]
			path = rpath
			if len(lpath.Right) == 0 {
				continue
			}

			// Pop next inners, a PathToLeaf (e.g. []ProofInnerNode).
			inners, rinnersq := innersq[0], innersq[1:]
			innersq = rinnersq

			// Recursively verify inners against remaining leaves.
			derivedRoot, treeEnd, done, err := COMPUTEHASH(inners, rightmost && rpath.isRightmost())
			if err != nil {
				return nil, treeEnd, false, errors.Wrap(err, "recursive COMPUTEHASH call")
			}

			if !bytes.Equal(derivedRoot, lpath.Right) {
				return nil, treeEnd, false, errors.Wrapf(ErrInvalidRoot, "intermediate root hash %X doesn't match, got %X", lpath.Right, derivedRoot)
			}

			if done {
				return hash, treeEnd, true, nil
			}
		}

		// We're not done yet (leaves left over). No error, not done either.
		// Technically if rightmost, we know there's an error "left over leaves
		// -- malformed proof", but we return that at the top level, below.
		return hash, false, false, nil
	}

	// Verify!
	path := proof.LeftPath
	rootHash, treeEnd, done, err := COMPUTEHASH(path, true)
	if err != nil {
		return nil, treeEnd, errors.Wrap(err, "root COMPUTEHASH call")
	} else if !done {
		return nil, treeEnd, errors.Wrap(ErrInvalidProof, "left over leaves -- malformed proof")
	}

	// Ok!
	return rootHash, treeEnd, nil
}

// toProto converts the proof to a Protobuf representation, for use in ValueOp and AbsenceOp.
func (proof *RangeProof) ToProto() *iavlproto.RangeProof {
	pb := &iavlproto.RangeProof{
		LeftPath:   make([]*iavlproto.ProofInnerNode, 0, len(proof.LeftPath)),
		InnerNodes: make([]*iavlproto.PathToLeaf, 0, len(proof.InnerNodes)),
		Leaves:     make([]*iavlproto.ProofLeafNode, 0, len(proof.Leaves)),
	}
	for _, inner := range proof.LeftPath {
		pb.LeftPath = append(pb.LeftPath, inner.toProto())
	}
	for _, path := range proof.InnerNodes {
		pbPath := make([]*iavlproto.ProofInnerNode, 0, len(path))
		for _, inner := range path {
			pbPath = append(pbPath, inner.toProto())
		}
		pb.InnerNodes = append(pb.InnerNodes, &iavlproto.PathToLeaf{Inners: pbPath})
	}
	for _, leaf := range proof.Leaves {
		pb.Leaves = append(pb.Leaves, leaf.toProto())
	}

	return pb
}

// rangeProofFromProto generates a RangeProof from a Protobuf RangeProof.
func RangeProofFromProto(pbProof *iavlproto.RangeProof) (RangeProof, error) {
	proof := RangeProof{}

	for _, pbInner := range pbProof.LeftPath {
		inner, err := proofInnerNodeFromProto(pbInner)
		if err != nil {
			return proof, err
		}
		proof.LeftPath = append(proof.LeftPath, inner)
	}

	for _, pbPath := range pbProof.InnerNodes {
		var path PathToLeaf // leave as nil unless populated, for Amino compatibility
		if pbPath != nil {
			for _, pbInner := range pbPath.Inners {
				inner, err := proofInnerNodeFromProto(pbInner)
				if err != nil {
					return proof, err
				}
				path = append(path, inner)
			}
		}
		proof.InnerNodes = append(proof.InnerNodes, path)
	}

	for _, pbLeaf := range pbProof.Leaves {
		leaf, err := proofLeafNodeFromProto(pbLeaf)
		if err != nil {
			return proof, err
		}
		proof.Leaves = append(proof.Leaves, leaf)
	}
	return proof, nil
}

// keyStart is inclusive and keyEnd is exclusive.
// If keyStart or keyEnd don't exist, the leaf before keyStart
// or after keyEnd will also be included, but not be included in values.
// If keyEnd-1 exists, no later leaves will be included.
// If keyStart >= keyEnd and both not nil, errors out.
// Limit is never exceeded.

func (t *ImmutableTree) getRangeProof(keyStart, keyEnd []byte, limit int) (proof *RangeProof, keys, values [][]byte, err error) {
	if keyStart != nil && keyEnd != nil && bytes.Compare(keyStart, keyEnd) >= 0 {
		return nil, nil, nil, fmt.Errorf("if keyStart and keyEnd are present, need keyStart < keyEnd")
	}
	if limit < 0 {
		return nil, nil, nil, fmt.Errorf("limit must be greater or equal to 0 -- 0 means no limit")
	}
	if t.root == nil {
		return nil, nil, nil, nil
	}

	_, _, err = t.root.hashWithCount() // Ensure that all hashes are calculated.
	if err != nil {
		return nil, nil, nil, err
	}

	// Get the first key/value pair proof, which provides us with the left key.
	path, left, err := t.root.PathToLeaf(t, keyStart)
	if err != nil {
		// Key doesn't exist, but instead we got the prev leaf (or the
		// first or last leaf), which provides proof of absence).
		err = nil
	}
	startOK := keyStart == nil || bytes.Compare(keyStart, left.key) <= 0
	endOK := keyEnd == nil || bytes.Compare(left.key, keyEnd) < 0
	// If left.key is in range, add it to key/values.
	if startOK && endOK {
		keys = append(keys, left.key) // == keyStart
		values = append(values, left.value)
	}

	h := sha256.Sum256(left.value)
	var leaves = []ProofLeafNode{
		{
			Key:       left.key,
			ValueHash: h[:],
			Version:   left.version,
		},
	}

	// 1: Special case if limit is 1.
	// 2: Special case if keyEnd is left.key+1.
	_stop := false
	if limit == 1 {
		_stop = true // case 1
	} else if keyEnd != nil && bytes.Compare(cpIncr(left.key), keyEnd) >= 0 {
		_stop = true // case 2
	}
	if _stop {
		return &RangeProof{
			LeftPath: path,
			Leaves:   leaves,
		}, keys, values, nil
	}

	// Get the key after left.key to iterate from.
	afterLeft := cpIncr(left.key)

	// Traverse starting from afterLeft, until keyEnd or the next leaf
	// after keyEnd.
	var allPathToLeafs = []PathToLeaf(nil)
	var currentPathToLeaf = PathToLeaf(nil)
	var leafCount = 1 // from left above.
	var pathCount = 0

	t.root.traverseInRange(t, afterLeft, nil, true, false, false,
		func(node *Node) (stop bool) {

			// Track when we diverge from path, or when we've exhausted path,
			// since the first allPathToLeafs shouldn't include it.
			if pathCount != -1 {
				if len(path) <= pathCount {
					// We're done with path counting.
					pathCount = -1
				} else {
					pn := path[pathCount]
					if pn.Height != node.height ||
						pn.Left != nil && !bytes.Equal(pn.Left, node.leftHash) ||
						pn.Right != nil && !bytes.Equal(pn.Right, node.rightHash) {

						// We've diverged, so start appending to allPathToLeaf.
						pathCount = -1
					} else {
						pathCount++
					}
				}
			}

			if node.height == 0 { // Leaf node
				// Append all paths that we tracked so far to get to this leaf node.
				allPathToLeafs = append(allPathToLeafs, currentPathToLeaf)
				// Start a new one to track as we traverse the tree.
				currentPathToLeaf = PathToLeaf(nil)

				h := sha256.Sum256(node.value)
				leaves = append(leaves, ProofLeafNode{
					Key:       node.key,
					ValueHash: h[:],
					Version:   node.version,
				})

				leafCount++

				// Maybe terminate because we found enough leaves.
				if limit > 0 && limit <= leafCount {
					return true
				}

				// Terminate if we've found keyEnd or after.
				if keyEnd != nil && bytes.Compare(node.key, keyEnd) >= 0 {
					return true
				}

				// Value is in range, append to keys and values.
				keys = append(keys, node.key)
				values = append(values, node.value)

				// Terminate if we've found keyEnd-1 or after.
				// We don't want to fetch any leaves for it.
				if keyEnd != nil && bytes.Compare(cpIncr(node.key), keyEnd) >= 0 {
					return true
				}

			} else if pathCount < 0 { // Inner node.
				// Only store if the node is not stored in currentPathToLeaf already. We track if we are
				// still going through PathToLeaf using pathCount. When pathCount goes to -1, we
				// start storing the other paths we took to get to the leaf nodes. Also we skip
				// storing the left node, since we are traversing the tree starting from the left
				// and don't need to store unnecessary info as we only need to go down the right
				// path.
				currentPathToLeaf = append(currentPathToLeaf, ProofInnerNode{
					Height:  node.height,
					Size:    node.size,
					Version: node.version,
					Left:    nil,
					Right:   node.rightHash,
				})
			}
			return false
		},
	)

	return &RangeProof{
		LeftPath:   path,
		InnerNodes: allPathToLeafs,
		Leaves:     leaves,
	}, keys, values, nil
}

//----------------------------------------

// GetWithProof gets the value under the key if it exists, or returns nil.
// A proof of existence or absence is returned alongside the value.
func (t *ImmutableTree) GetWithProof(key []byte) (value []byte, proof *RangeProof, err error) {
	proof, _, values, err := t.getRangeProof(key, cpIncr(key), 2)
	if err != nil {
		return nil, nil, errors.Wrap(err, "constructing range proof")
	}
	if len(values) > 0 && bytes.Equal(proof.Leaves[0].Key, key) {
		return values[0], proof, nil
	}
	return nil, proof, nil
}

// GetRangeWithProof gets key/value pairs within the specified range and limit.
func (t *ImmutableTree) GetRangeWithProof(startKey []byte, endKey []byte, limit int) (keys, values [][]byte, proof *RangeProof, err error) {
	proof, keys, values, err = t.getRangeProof(startKey, endKey, limit)
	return
}

// GetVersionedWithProof gets the value under the key at the specified version
// if it exists, or returns nil.
func (tree *MutableTree) GetVersionedWithProof(key []byte, version int64) ([]byte, *RangeProof, error) {
	if tree.VersionExists(version) {
		t, err := tree.GetImmutable(version)
		if err != nil {
			return nil, nil, err
		}

		return t.GetWithProof(key)
	}
	return nil, nil, errors.Wrap(ErrVersionDoesNotExist, "")
}

// GetVersionedRangeWithProof gets key/value pairs within the specified range
// and limit.
func (tree *MutableTree) GetVersionedRangeWithProof(startKey, endKey []byte, limit int, version int64) (
	keys, values [][]byte, proof *RangeProof, err error) {

	if tree.VersionExists(version) {
		t, err := tree.GetImmutable(version)
		if err != nil {
			return nil, nil, nil, err
		}
		return t.GetRangeWithProof(startKey, endKey, limit)
	}
	return nil, nil, nil, errors.Wrap(ErrVersionDoesNotExist, "")
}
