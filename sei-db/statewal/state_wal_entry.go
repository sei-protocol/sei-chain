package statewal

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// The kind of a WAL record. Every serialized entry begins with one of these bytes.
type entryKind byte

const (
	// A changeset record: a block number plus the set of changes written for that block.
	kindChangeset entryKind = 1
	// An end-of-block record: marks that no more changes will be written for a block number. On reload, a
	// block whose changeset records are not followed by an end-of-block marker is discarded.
	kindEndOfBlock entryKind = 2
)

// A WAL entry for state.
//
// An entry is either a changeset record (EndOfBlock is false, Changeset holds the block's changes) or an
// end-of-block marker (EndOfBlock is true, Changeset is nil). A single block may be described by several
// changeset records followed by exactly one end-of-block marker.
type Entry struct {

	// The block number associated with this entry.
	BlockNumber uint64

	// The changeset associated with this entry. Nil for end-of-block markers.
	Changeset []*proto.NamedChangeSet

	// True if this entry marks the end of a block. End-of-block entries carry no changeset.
	EndOfBlock bool
}

// Constructor for a changeset entry.
func NewEntry(blockNumber uint64, changeset []*proto.NamedChangeSet) *Entry {
	return &Entry{
		BlockNumber: blockNumber,
		Changeset:   changeset,
	}
}

// Constructor for an end-of-block marker entry.
func NewEndOfBlockEntry(blockNumber uint64) *Entry {
	return &Entry{
		BlockNumber: blockNumber,
		EndOfBlock:  true,
	}
}

// Serialize the WAL entry to bytes. The returned bytes are the record payload; the file layer is responsible
// for framing (length prefix and checksum). The layout is:
//
//	[1-byte kind][uvarint block number]
//
// followed, for changeset records only, by:
//
//	[uvarint changeset count]([uvarint marshaled length][marshaled NamedChangeSet])*
func (e *Entry) Serialize() ([]byte, error) {
	var buf []byte
	var scratch [binary.MaxVarintLen64]byte

	if e.EndOfBlock {
		buf = append(buf, byte(kindEndOfBlock))
		n := binary.PutUvarint(scratch[:], e.BlockNumber)
		buf = append(buf, scratch[:n]...)
		return buf, nil
	}

	buf = append(buf, byte(kindChangeset))
	n := binary.PutUvarint(scratch[:], e.BlockNumber)
	buf = append(buf, scratch[:n]...)

	n = binary.PutUvarint(scratch[:], uint64(len(e.Changeset)))
	buf = append(buf, scratch[:n]...)

	for i, ncs := range e.Changeset {
		if ncs == nil {
			return nil, fmt.Errorf("changeset %d is nil", i)
		}
		marshaled, err := ncs.Marshal()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal changeset %d: %w", i, err)
		}
		n = binary.PutUvarint(scratch[:], uint64(len(marshaled)))
		buf = append(buf, scratch[:n]...)
		buf = append(buf, marshaled...)
	}

	return buf, nil
}

// DeserializeEntry parses a record payload previously produced by Serialize.
func DeserializeEntry(data []byte) (
	// The resulting WAL entry.
	entry *Entry,
	// If true, the WAL entry was successfully deserialized.
	// If false, the data was truncated or otherwise incomplete and entry is nil.
	ok bool,
	// Returns an error if the data could not be deserialized due to an unexpected error (e.g. a corrupt
	// protobuf payload). Does not return an error if the data is simply truncated.
	err error,
) {
	if len(data) == 0 {
		return nil, false, nil
	}

	kind := entryKind(data[0])
	rest := data[1:]

	blockNumber, n := binary.Uvarint(rest)
	if n <= 0 {
		return nil, false, nil
	}
	rest = rest[n:]

	switch kind {
	case kindEndOfBlock:
		return NewEndOfBlockEntry(blockNumber), true, nil
	case kindChangeset:
		count, n := binary.Uvarint(rest)
		if n <= 0 {
			return nil, false, nil
		}
		rest = rest[n:]

		changeset := make([]*proto.NamedChangeSet, 0, count)
		for i := uint64(0); i < count; i++ {
			length, n := binary.Uvarint(rest)
			if n <= 0 {
				return nil, false, nil
			}
			rest = rest[n:]
			if uint64(len(rest)) < length {
				return nil, false, nil
			}
			ncs := &proto.NamedChangeSet{}
			if err := ncs.Unmarshal(rest[:length]); err != nil {
				return nil, false, fmt.Errorf("failed to unmarshal changeset %d: %w", i, err)
			}
			rest = rest[length:]
			changeset = append(changeset, ncs)
		}
		return NewEntry(blockNumber, changeset), true, nil
	default:
		return nil, false, fmt.Errorf("unknown WAL entry kind %d", kind)
	}
}
