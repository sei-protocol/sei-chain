package bud

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

const (
	// budProofVersion is the version of the serialized BUD proof format produced by this package.
	budProofVersion uint8 = 1

	// Byte offsets of the fields before the budlet in the serialized BUD proof format.
	budProofVersionOffset    = 0
	budProofBUDVersionOffset = budProofVersionOffset + 1
	budProofBudletOffset     = budProofBUDVersionOffset + 1

	// budProofPositionSize is the length of the count and index that follow the budlet.
	budProofPositionSize = 8 + 8
)

// BUDProof proves that a budlet is a leaf of the BUD tree under a BUD. A BUDProof is immutable; create one with
// BUDTree.BuildBUDProof() or DeserializeBUDProof().
type BUDProof struct {
	// The budlet proven to be a leaf.
	budlet Budlet

	// The number of leaves in the BUD tree.
	count uint64

	// The position of the budlet among the leaves, which are sorted by key.
	index uint64

	// The sibling hashes on the path from the budlet's leaf to the root, leaf end first.
	siblings [][32]byte
}

// DeserializeBUDProof parses a BUD proof from the serialized BUD proof format. It returns an error unless data is
// exactly one serialized BUD proof with a supported BUD proof version and BUD version, a valid budlet, an index
// less than its count, and the number of siblings its count and index define.
func DeserializeBUDProof(
	// The serialized BUD proof.
	data []byte,
) (*BUDProof, error) {
	proof, rest, err := readBUDProof(data)
	if err != nil {
		return nil, fmt.Errorf("deserializing BUD proof: %w", err)
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("serialized BUD proof is followed by %d more bytes", len(rest))
	}
	return proof, nil
}

// ComputeBUD returns the BUD of the BUD tree the proof places its budlet in. The proof shows that a block wrote
// the budlet only when the result equals that block's BUD, taken from a trusted source.
func (p *BUDProof) ComputeBUD() apphash.BUD {
	node := budLeafHash(&p.budlet)
	siblings := p.siblings
	position := p.index
	for size := p.count; size > 1; size = size/2 + size%2 {
		switch {
		case position%2 == 1:
			node = budInnerHash(siblings[0], node)
			siblings = siblings[1:]
		case position+1 < size:
			node = budInnerHash(node, siblings[0])
			siblings = siblings[1:]
		default:
			// The node is carried up unpaired, so it has no sibling at this level.
		}
		position /= 2
	}
	return hashBUD(p.count, node)
}

// Budlet returns the budlet the proof is about.
func (p *BUDProof) Budlet() *Budlet {
	return &p.budlet
}

// Count returns the number of leaves in the BUD tree.
func (p *BUDProof) Count() uint64 {
	return p.count
}

// Index returns the position of the budlet among the BUD tree's leaves, which are sorted by key.
func (p *BUDProof) Index() uint64 {
	return p.index
}

// Siblings returns the sibling hashes on the path from the budlet's leaf to the root, leaf end first. The caller
// must not mutate them.
func (p *BUDProof) Siblings() [][32]byte {
	return p.siblings
}

// Serialize returns the BUD proof in the serialized BUD proof format.
func (p *BUDProof) Serialize() []byte {
	budlet := p.budlet.Serialize()
	serialized := make([]byte, 0, budProofBudletOffset+len(budlet)+budProofPositionSize+32*len(p.siblings))
	serialized = append(serialized, budProofVersion, budVersion)
	serialized = append(serialized, budlet...)
	serialized = binary.BigEndian.AppendUint64(serialized, p.count)
	serialized = binary.BigEndian.AppendUint64(serialized, p.index)
	for _, sibling := range p.siblings {
		serialized = append(serialized, sibling[:]...)
	}
	return serialized
}

// readBUDProof parses the serialized BUD proof at the start of data, returning it and the bytes after it.
func readBUDProof(
	// Bytes that start with a serialized BUD proof.
	data []byte,
) (*BUDProof, []byte, error) {
	if len(data) < 1 {
		return nil, nil, fmt.Errorf("serialized BUD proof is empty")
	}
	if version := data[budProofVersionOffset]; version != budProofVersion {
		return nil, nil, fmt.Errorf("unsupported BUD proof version %d, want %d", version, budProofVersion)
	}
	if len(data) < budProofBudletOffset {
		return nil, nil, fmt.Errorf("serialized BUD proof ends before its BUD version")
	}
	if version := data[budProofBUDVersionOffset]; version != budVersion {
		return nil, nil, fmt.Errorf("BUD proof is for unsupported BUD version %d, want %d", version, budVersion)
	}

	budlet, rest, err := readBudlet(data[budProofBudletOffset:])
	if err != nil {
		return nil, nil, fmt.Errorf("reading budlet: %w", err)
	}
	if len(rest) < budProofPositionSize {
		return nil, nil, fmt.Errorf("serialized BUD proof ends before its count and index")
	}
	count := binary.BigEndian.Uint64(rest)
	index := binary.BigEndian.Uint64(rest[8:])
	rest = rest[budProofPositionSize:]
	if index >= count {
		return nil, nil, fmt.Errorf("BUD proof index %d is not less than its count %d", index, count)
	}

	siblingCount := budTreeSiblingCount(count, index)
	if len(rest) < 32*siblingCount {
		return nil, nil, fmt.Errorf("serialized BUD proof has %d bytes of siblings, want %d",
			len(rest), 32*siblingCount)
	}
	var siblings [][32]byte
	for i := 0; i < siblingCount; i++ {
		siblings = append(siblings, [32]byte(rest[32*i:32*i+32]))
	}
	rest = rest[32*siblingCount:]

	return &BUDProof{budlet: *budlet, count: count, index: index, siblings: siblings}, rest, nil
}

// validate returns an error unless the proof is one DeserializeBUDProof() could return: a budlet NewBudlet() would
// accept, an index less than its count, and the number of siblings its count and index define.
func (p *BUDProof) validate() error {
	if err := p.budlet.validate(); err != nil {
		return fmt.Errorf("invalid budlet: %w", err)
	}
	if p.index >= p.count {
		return fmt.Errorf("index %d is not less than count %d", p.index, p.count)
	}
	if want := budTreeSiblingCount(p.count, p.index); len(p.siblings) != want {
		return fmt.Errorf("%d siblings, want %d", len(p.siblings), want)
	}
	return nil
}
