package bud

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

const (
	// budStateProofVersion is the version of the serialized BUD state proof format produced by this package.
	budStateProofVersion uint8 = 1

	// Byte offsets of the fixed fields in the serialized BUD state proof format.
	budStateProofVersionOffset = 0
	budStateProofCountOffset   = budStateProofVersionOffset + 1
	budStateProofPairsOffset   = budStateProofCountOffset + 1

	// appHashDataLengthSize is the length of the prefix giving the size of each serialized app hash data.
	appHashDataLengthSize = 4
)

// BUDStateProof proves the value of one key over a range of block heights, from one or two BUD proofs, each
// paired with the app hash data of its block. With one BUD proof the range is that block. With two, the key was
// written at the first block and next written at the second, so its value is the first budlet's value from the
// first block until the second, and the second budlet's value at the second. Every BUDStateProof is internally
// consistent; trusting it still requires authenticating each of AppHashes().
type BUDStateProof struct {
	// The app hash data of each BUD proof's block, in height order.
	appHashData []*apphash.AppHashData

	// The BUD proofs, each against the BUD in the app hash data at the same position.
	budProofs []*BUDProof
}

// NewBUDStateProof returns the BUD state proof made of one or two BUD proofs, each paired with the app hash data
// at the same position. It returns an error unless each BUD proof computes the BUD in its pair's app hash data.
// With two, it also returns an error unless both budlets have the same key, the second budlet's previous height
// is the first block's height, the first block's height is the lower, and both blocks share a chain ID and app
// hash version.
func NewBUDStateProof(
	// The app hash data of each BUD proof's block, in height order. Must hold as many entries as budProofs.
	// Retained, so it must not be mutated afterward.
	appHashData []*apphash.AppHashData,
	// The BUD proofs, one or two, each against the BUD in the app hash data at the same position. Retained, so it
	// must not be mutated afterward.
	budProofs []*BUDProof,
) (*BUDStateProof, error) {
	if len(appHashData) != len(budProofs) {
		return nil, fmt.Errorf("creating BUD state proof: %d app hash data for %d BUD proofs",
			len(appHashData), len(budProofs))
	}
	if len(budProofs) != 1 && len(budProofs) != 2 {
		return nil, fmt.Errorf("creating BUD state proof: %d BUD proofs, want 1 or 2", len(budProofs))
	}
	if err := checkBUDsMatchAppHashData(appHashData, budProofs); err != nil {
		return nil, fmt.Errorf("creating BUD state proof: %w", err)
	}
	if len(budProofs) == 2 {
		err := checkConsecutiveWrites(appHashData[0], budProofs[0], appHashData[1], budProofs[1])
		if err != nil {
			return nil, fmt.Errorf("creating BUD state proof: %w", err)
		}
	}
	return &BUDStateProof{appHashData: appHashData, budProofs: budProofs}, nil
}

// DeserializeBUDStateProof parses a BUD state proof from the serialized BUD state proof format. It returns an
// error unless data starts with a supported version, holds exactly the pairs of app hash data and BUD proof it
// declares, each of which deserializes, and those pairs are ones NewBUDStateProof() would accept.
func DeserializeBUDStateProof(data []byte) (*BUDStateProof, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("serialized BUD state proof is empty")
	}
	if version := data[budStateProofVersionOffset]; version != budStateProofVersion {
		return nil, fmt.Errorf("unsupported BUD state proof version %d, want %d", version, budStateProofVersion)
	}
	if len(data) < budStateProofPairsOffset {
		return nil, fmt.Errorf("serialized BUD state proof ends before its BUD proof count")
	}
	count := int(data[budStateProofCountOffset])
	if count != 1 && count != 2 {
		return nil, fmt.Errorf("serialized BUD state proof declares %d BUD proofs, want 1 or 2", count)
	}

	appHashData := make([]*apphash.AppHashData, 0, count)
	budProofs := make([]*BUDProof, 0, count)
	rest := data[budStateProofPairsOffset:]
	for i := 0; i < count; i++ {
		ahd, after, err := readAppHashData(rest)
		if err != nil {
			return nil, fmt.Errorf("deserializing BUD state proof pair %d: %w", i, err)
		}
		proof, after, err := readBUDProof(after)
		if err != nil {
			return nil, fmt.Errorf("deserializing BUD state proof pair %d: %w", i, err)
		}
		appHashData = append(appHashData, ahd)
		budProofs = append(budProofs, proof)
		rest = after
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("serialized BUD state proof has %d bytes after its %d pairs", len(rest), count)
	}

	stateProof, err := NewBUDStateProof(appHashData, budProofs)
	if err != nil {
		return nil, fmt.Errorf("deserializing BUD state proof: %w", err)
	}
	return stateProof, nil
}

// Key returns the key whose value the proof is about. The caller must not mutate it.
func (p *BUDStateProof) Key() []byte {
	return p.budProofs[0].budlet.key
}

// StartHeight returns the lowest block height the proof covers.
func (p *BUDStateProof) StartHeight() uint64 {
	return p.appHashData[0].BlockHeight()
}

// EndHeight returns the highest block height the proof covers. It equals StartHeight() for a proof of one BUD
// proof.
func (p *BUDStateProof) EndHeight() uint64 {
	return p.appHashData[len(p.appHashData)-1].BlockHeight()
}

// ValueAt returns the value the key held at height, nil if the key was deleted, or false when the proof does not
// cover height. The caller must not mutate the value.
func (p *BUDStateProof) ValueAt(height uint64) ([]byte, bool) {
	if height < p.StartHeight() || height > p.EndHeight() {
		return nil, false
	}
	if height == p.EndHeight() {
		return p.budProofs[len(p.budProofs)-1].budlet.value, true
	}
	return p.budProofs[0].budlet.value, true
}

// AppHashes returns the app hash of each BUD proof's block, in height order: the first is the app hash at
// StartHeight(), and for a proof of two BUD proofs the second is the app hash at EndHeight(). The caller must
// authenticate each one before trusting the proof.
func (p *BUDStateProof) AppHashes() [][32]byte {
	appHashes := make([][32]byte, 0, len(p.appHashData))
	for _, ahd := range p.appHashData {
		appHashes = append(appHashes, ahd.AppHash())
	}
	return appHashes
}

// Serialize returns the proof in the serialized BUD state proof format.
func (p *BUDStateProof) Serialize() []byte {
	serialized := []byte{budStateProofVersion, uint8(len(p.budProofs))} //nolint:gosec // G115 - 1 or 2 proofs
	for i, budProof := range p.budProofs {
		ahd := p.appHashData[i].Serialize()
		ahdLength := uint32(len(ahd)) //nolint:gosec // G115 - app hash data is a few hundred bytes
		serialized = binary.BigEndian.AppendUint32(serialized, ahdLength)
		serialized = append(serialized, ahd...)
		serialized = append(serialized, budProof.Serialize()...)
	}
	return serialized
}

// checkBUDsMatchAppHashData returns an error unless each BUD proof computes the BUD in the app hash data at the
// same position.
func checkBUDsMatchAppHashData(
	// The app hash data of each BUD proof's block, as many as budProofs.
	appHashData []*apphash.AppHashData,
	// The BUD proofs to check.
	budProofs []*BUDProof,
) error {
	for i, budProof := range budProofs {
		if computed, want := budProof.ComputeBUD(), appHashData[i].BUD(); computed != want {
			return fmt.Errorf("BUD proof %d computes BUD %x, but its app hash data holds BUD %x",
				i, computed, want)
		}
	}
	return nil
}

// checkConsecutiveWrites returns an error unless the second BUD proof's budlet is the next write, after the first's,
// of the same key on the same chain.
func checkConsecutiveWrites(
	firstAppHashData *apphash.AppHashData,
	firstProof *BUDProof,
	secondAppHashData *apphash.AppHashData,
	secondProof *BUDProof,
) error {
	if !bytes.Equal(firstProof.budlet.key, secondProof.budlet.key) {
		return fmt.Errorf("BUD proofs are for keys %x and %x", firstProof.budlet.key, secondProof.budlet.key)
	}
	if firstAppHashData.BlockHeight() >= secondAppHashData.BlockHeight() {
		return fmt.Errorf("first block height %d is not below second block height %d",
			firstAppHashData.BlockHeight(), secondAppHashData.BlockHeight())
	}
	if secondProof.budlet.previousHeight != firstAppHashData.BlockHeight() {
		return fmt.Errorf("second budlet's previous height %d is not the first block height %d",
			secondProof.budlet.previousHeight, firstAppHashData.BlockHeight())
	}
	if firstAppHashData.ChainID() != secondAppHashData.ChainID() {
		return fmt.Errorf("blocks are on chains %d and %d",
			firstAppHashData.ChainID(), secondAppHashData.ChainID())
	}
	if firstAppHashData.Version() != secondAppHashData.Version() {
		return fmt.Errorf("blocks have app hash versions %d and %d",
			firstAppHashData.Version(), secondAppHashData.Version())
	}
	return nil
}

// readAppHashData parses the length-prefixed serialized app hash data at the start of data, returning it and the
// bytes after it.
func readAppHashData(data []byte) (*apphash.AppHashData, []byte, error) {
	if len(data) < appHashDataLengthSize {
		return nil, nil, fmt.Errorf("serialized BUD state proof ends before an app hash data length")
	}
	length := uint64(binary.BigEndian.Uint32(data))
	data = data[appHashDataLengthSize:]
	if uint64(len(data)) < length {
		return nil, nil, fmt.Errorf("serialized BUD state proof ends inside a %d byte app hash data", length)
	}
	ahd, err := apphash.Deserialize(data[:length])
	if err != nil {
		return nil, nil, fmt.Errorf("deserializing app hash data: %w", err)
	}
	return ahd, data[length:], nil
}
