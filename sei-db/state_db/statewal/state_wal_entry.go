package statewal

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// A decoded block entry from the state WAL: a block number and the changesets written for that block.
type Entry struct {

	// The block number associated with this entry.
	BlockNumber uint64

	// The changesets associated with this block, in write order.
	Changeset []*proto.NamedChangeSet
}

// NewEntry constructs an entry for the given block number and changesets.
func NewEntry(blockNumber uint64, changeset []*proto.NamedChangeSet) *Entry {
	return &Entry{
		BlockNumber: blockNumber,
		Changeset:   changeset,
	}
}

// appendChangeset appends the framing [uvarint marshaled length][marshaled NamedChangeSet] for ncs to buf and
// returns the extended buffer. This is the incremental unit the serializer goroutine accumulates across the
// multiple Write calls of a single block before appending the whole block as one WAL record.
func appendChangeset(buf []byte, ncs *proto.NamedChangeSet) ([]byte, error) {
	if ncs == nil {
		return nil, fmt.Errorf("changeset is nil")
	}
	marshaled, err := ncs.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal changeset: %w", err)
	}
	var scratch [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(scratch[:], uint64(len(marshaled)))
	buf = append(buf, scratch[:n]...)
	buf = append(buf, marshaled...)
	return buf, nil
}

// serializeChangesets encodes a changeset list as the concatenation ([uvarint length][marshaled])* — the
// payload of a single block's WAL record. The block number is not encoded: it is the WAL record's index.
func serializeChangesets(cs []*proto.NamedChangeSet) ([]byte, error) {
	var buf []byte
	var err error
	for _, ncs := range cs {
		buf, err = appendChangeset(buf, ncs)
		if err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// deserializeChangesets decodes the payload produced by serializeChangesets. Because the enclosing WAL record
// is length-delimited and CRC-verified by the underlying WAL, any truncation encountered here indicates
// corruption and is reported as an error rather than tolerated.
func deserializeChangesets(data []byte) ([]*proto.NamedChangeSet, error) {
	var result []*proto.NamedChangeSet
	rest := data
	for len(rest) > 0 {
		length, n := binary.Uvarint(rest)
		if n <= 0 {
			return nil, fmt.Errorf("corrupt changeset length prefix")
		}
		rest = rest[n:]
		if uint64(len(rest)) < length {
			return nil, fmt.Errorf("changeset payload truncated: need %d bytes, have %d", length, len(rest))
		}
		ncs := &proto.NamedChangeSet{}
		if err := ncs.Unmarshal(rest[:length]); err != nil {
			return nil, fmt.Errorf("failed to unmarshal changeset: %w", err)
		}
		rest = rest[length:]
		result = append(result, ncs)
	}
	return result, nil
}
