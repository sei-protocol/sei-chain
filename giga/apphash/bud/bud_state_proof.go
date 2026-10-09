package bud

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

const (
	// budStateProofVersion is the version of the serialized BUD state proof format produced by this package.
	budStateProofVersion uint8 = 1

	// Byte offsets of the fixed fields in the serialized BUD state proof format.
	budStateProofVersionOffset     = 0
	budStateProofAppHashDataOffset = budStateProofVersionOffset + 1

	// appHashDataLengthSize is the length of the prefix giving the size of the serialized app hash data.
	appHashDataLengthSize = 4
)

// BUDStateProof proves the value of one key over a range of block heights: the key's previous value from the
// budlet's not-modified-since height up to the height of the block that wrote it, and the value written at that
// height. It proves nothing until the caller has also:
//   - authenticated AppHash()
//   - confirmed that ChainID() is the chain it expects
//   - confirmed that Key() is the key it asked about
//   - confirmed that the heights it asked about lie within StartHeight() and EndHeight()
type BUDStateProof struct {
	// The app hash data of the block that wrote the key.
	appHashData *apphash.AppHashData

	// The BUD proof of the write, against the BUD in appHashData.
	budProof *BUDProof
}

// NewBUDStateProof returns the BUD state proof made of a BUD proof and the app hash data of its block. It returns
// an error unless the BUD proof computes the BUD in the app hash data and its budlet's not-modified-since height is
// below the block's height.
func NewBUDStateProof(
	// The app hash data of the block that wrote the key. Retained, so it must not be mutated afterward.
	appHashData *apphash.AppHashData,
	// The BUD proof of the write, against the BUD in appHashData. Retained, so it must not be mutated afterward.
	budProof *BUDProof,
) (*BUDStateProof, error) {
	if appHashData == nil {
		return nil, fmt.Errorf("creating BUD state proof: app hash data is nil")
	}
	if budProof == nil {
		return nil, fmt.Errorf("creating BUD state proof: BUD proof is nil")
	}
	if err := budProof.validate(); err != nil {
		return nil, fmt.Errorf("creating BUD state proof: invalid BUD proof: %w", err)
	}
	if computed, want := budProof.ComputeBUD(), appHashData.BUD(); computed != want {
		return nil, fmt.Errorf("creating BUD state proof: BUD proof computes %x, but app hash data holds %x",
			computed, want)
	}
	if notModifiedSince := budProof.budlet.notModifiedSince; notModifiedSince >= appHashData.BlockHeight() {
		return nil, fmt.Errorf("creating BUD state proof: not-modified-since height %d is not below block height %d",
			notModifiedSince, appHashData.BlockHeight())
	}
	return &BUDStateProof{appHashData: appHashData, budProof: budProof}, nil
}

// DeserializeBUDStateProof parses a BUD state proof from the serialized BUD state proof format. It returns an
// error unless data is exactly a supported version followed by an app hash data and a BUD proof, each of which
// deserializes, that NewBUDStateProof() would accept.
func DeserializeBUDStateProof(
	// The serialized BUD state proof.
	data []byte,
) (*BUDStateProof, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("serialized BUD state proof is empty")
	}
	if version := data[budStateProofVersionOffset]; version != budStateProofVersion {
		return nil, fmt.Errorf("unsupported BUD state proof version %d, want %d", version, budStateProofVersion)
	}

	appHashData, rest, err := readAppHashData(data[budStateProofAppHashDataOffset:])
	if err != nil {
		return nil, fmt.Errorf("deserializing BUD state proof: %w", err)
	}
	budProof, rest, err := readBUDProof(rest)
	if err != nil {
		return nil, fmt.Errorf("deserializing BUD state proof: %w", err)
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("serialized BUD state proof has %d bytes after its BUD proof", len(rest))
	}

	stateProof, err := NewBUDStateProof(appHashData, budProof)
	if err != nil {
		return nil, fmt.Errorf("deserializing BUD state proof: %w", err)
	}
	return stateProof, nil
}

// Key returns the key whose value the proof is about. The caller must not mutate it.
func (p *BUDStateProof) Key() []byte {
	_, budProof := p.parts()
	return budProof.budlet.key
}

// ChainID returns the EVM chain ID of the chain the proof's block belongs to.
func (p *BUDStateProof) ChainID() uint64 {
	appHashData, _ := p.parts()
	return appHashData.ChainID()
}

// StartHeight returns the lowest block height the proof covers: its budlet's not-modified-since height.
func (p *BUDStateProof) StartHeight() uint64 {
	_, budProof := p.parts()
	return budProof.budlet.notModifiedSince
}

// EndHeight returns the highest block height the proof covers: the height of the block that wrote the key.
func (p *BUDStateProof) EndHeight() uint64 {
	appHashData, _ := p.parts()
	return appHashData.BlockHeight()
}

// ValueAt returns the value the key held at height, nil if the key was absent, or false when the proof does not
// cover height. The caller must not mutate the value.
func (p *BUDStateProof) ValueAt(
	// The block height to look up.
	height uint64,
) ([]byte, bool) {
	if p.budProof == nil || height < p.StartHeight() || height > p.EndHeight() {
		return nil, false
	}
	if height == p.EndHeight() {
		return p.budProof.budlet.value, true
	}
	return p.budProof.budlet.previousValue, true
}

// AppHash returns the app hash of the block that wrote the key. The caller must authenticate it before trusting
// the proof.
func (p *BUDStateProof) AppHash() [32]byte {
	appHashData, _ := p.parts()
	return appHashData.AppHash()
}

// Serialize returns the proof in the serialized BUD state proof format.
func (p *BUDStateProof) Serialize() []byte {
	appHashData, budProof := p.parts()
	ahd := appHashData.Serialize()
	ahdLength := uint32(len(ahd)) //nolint:gosec // G115 - app hash data is a few hundred bytes

	serialized := []byte{budStateProofVersion}
	serialized = binary.BigEndian.AppendUint32(serialized, ahdLength)
	serialized = append(serialized, ahd...)
	return append(serialized, budProof.Serialize()...)
}

// parts returns the proof's app hash data and BUD proof, or zero values for a zero-value proof.
func (p *BUDStateProof) parts() (*apphash.AppHashData, *BUDProof) {
	if p.appHashData == nil || p.budProof == nil {
		return &apphash.AppHashData{}, &BUDProof{}
	}
	return p.appHashData, p.budProof
}

// readAppHashData parses the length-prefixed serialized app hash data at the start of data, returning it and the
// bytes after it.
func readAppHashData(
	// Bytes that start with a length-prefixed serialized app hash data.
	data []byte,
) (*apphash.AppHashData, []byte, error) {
	if len(data) < appHashDataLengthSize {
		return nil, nil, fmt.Errorf("serialized BUD state proof ends before its app hash data length")
	}
	length := uint64(binary.BigEndian.Uint32(data))
	data = data[appHashDataLengthSize:]
	if uint64(len(data)) < length {
		return nil, nil, fmt.Errorf("serialized BUD state proof ends inside its %d byte app hash data", length)
	}
	ahd, err := apphash.Deserialize(data[:length])
	if err != nil {
		return nil, nil, fmt.Errorf("deserializing app hash data: %w", err)
	}
	return ahd, data[length:], nil
}
