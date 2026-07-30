package statewal

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// The version byte prefixed to every serialized changeset payload. Bumped if the changeset encoding changes,
// so deserialization can detect and reject an unknown format rather than misparsing it.
const changesetFormatVersion = byte(1)

// appendChangeset appends the framing [uvarint marshaled length][marshaled NamedChangeSet] for ncs to buf and
// returns the extended buffer. It frames a single changeset; serializeChangesets calls it once per changeset
// to build a block's WAL record payload.
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

// serializeChangesets encodes a changeset list as a version byte followed by the concatenation
// [version]([uvarint length][marshaled])* — the payload of a single block's WAL record. The block number is
// not encoded: it is the WAL record's index.
func serializeChangesets(cs []*proto.NamedChangeSet) ([]byte, error) {
	buf := []byte{changesetFormatVersion}
	var err error
	for _, ncs := range cs {
		buf, err = appendChangeset(buf, ncs)
		if err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// deserializeChangesets decodes the payload produced by serializeChangesets, after checking its leading
// version byte. Because the enclosing WAL record is length-delimited and CRC-verified by the underlying WAL,
// any truncation encountered here indicates corruption and is reported as an error rather than tolerated.
func deserializeChangesets(data []byte) ([]*proto.NamedChangeSet, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty changeset payload: missing version byte")
	}
	if version := data[0]; version != changesetFormatVersion {
		return nil, fmt.Errorf("unsupported changeset format version %d", version)
	}

	var result []*proto.NamedChangeSet
	rest := data[1:]
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
