package migration

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// WAL record payload format:
//
//	[u32 BE: old DB change set count]
//	for each:
//	  [u32 BE: byte length]
//	  [byte length bytes: proto-marshaled NamedChangeSet]
//	[u32 BE: new DB change set count]
//	for each:
//	  [u32 BE: byte length]
//	  [byte length bytes: proto-marshaled NamedChangeSet]
//
// The record holds everything needed to replay a single ApplyChangeSets
// batch to both databases: the list destined for the old DB (including the
// MigrationStore OldDBBatchIDKey update) and the list destined for the new
// DB (including the MigrationStore NewDBBatchIDKey and boundary update).
// Each NamedChangeSet is encoded with its own generated proto marshaller;
// this file only provides the framing.

// encodeWALRecord packs two change set lists into a single WAL payload.
func encodeWALRecord(oldDBChangeSets, newDBChangeSets []*proto.NamedChangeSet) ([]byte, error) {
	oldBytes, err := marshalChangeSets(oldDBChangeSets)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal old DB change sets: %w", err)
	}
	newBytes, err := marshalChangeSets(newDBChangeSets)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal new DB change sets: %w", err)
	}

	size := 4 + 4 // two count headers
	for _, b := range oldBytes {
		size += 4 + len(b)
	}
	for _, b := range newBytes {
		size += 4 + len(b)
	}

	buf := make([]byte, 0, size)
	buf, err = appendLengthPrefixedList(buf, oldBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to append old DB change sets: %w", err)
	}
	buf, err = appendLengthPrefixedList(buf, newBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to append new DB change sets: %w", err)
	}
	return buf, nil
}

// decodeWALRecord is the inverse of encodeWALRecord.
func decodeWALRecord(data []byte) (oldDBChangeSets, newDBChangeSets []*proto.NamedChangeSet, err error) {
	oldDBChangeSets, data, err = readChangeSetList(data)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode old DB change sets: %w", err)
	}
	newDBChangeSets, data, err = readChangeSetList(data)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode new DB change sets: %w", err)
	}
	if len(data) != 0 {
		return nil, nil, fmt.Errorf("unexpected trailing data: %d bytes", len(data))
	}
	return oldDBChangeSets, newDBChangeSets, nil
}

func marshalChangeSets(changeSets []*proto.NamedChangeSet) ([][]byte, error) {
	out := make([][]byte, len(changeSets))
	for i, cs := range changeSets {
		b, err := cs.Marshal()
		if err != nil {
			return nil, fmt.Errorf("change set %d: %w", i, err)
		}
		out[i] = b
	}
	return out, nil
}

func appendLengthPrefixedList(buf []byte, items [][]byte) ([]byte, error) {
	if len(items) > math.MaxUint32 {
		return nil, fmt.Errorf("too many change sets: %d exceeds uint32 max", len(items))
	}
	// #nosec G115 -- guarded by MaxUint32 check above.
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(items)))
	for _, item := range items {
		if len(item) > math.MaxUint32 {
			return nil, fmt.Errorf("change set size %d exceeds uint32 max", len(item))
		}
		// #nosec G115 -- guarded by MaxUint32 check above.
		buf = binary.BigEndian.AppendUint32(buf, uint32(len(item)))
		buf = append(buf, item...)
	}
	return buf, nil
}

func readChangeSetList(data []byte) ([]*proto.NamedChangeSet, []byte, error) {
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("short read on count header")
	}
	count := binary.BigEndian.Uint32(data[:4])
	data = data[4:]
	items := make([]*proto.NamedChangeSet, 0, count)
	for i := uint32(0); i < count; i++ {
		if len(data) < 4 {
			return nil, nil, fmt.Errorf("short read on item %d length header", i)
		}
		itemLen := binary.BigEndian.Uint32(data[:4])
		data = data[4:]
		if uint64(len(data)) < uint64(itemLen) {
			return nil, nil, fmt.Errorf("short read on item %d body", i)
		}
		var cs proto.NamedChangeSet
		if err := cs.Unmarshal(data[:itemLen]); err != nil {
			return nil, nil, fmt.Errorf("failed to unmarshal item %d: %w", i, err)
		}
		items = append(items, &cs)
		data = data[itemLen:]
	}
	return items, data, nil
}
