// Package statewaltest writes state WAL records in the legacy changeset format that earlier releases wrote, so tests
// can build the WALs a node upgrading from such a release would have on disk.
package statewaltest

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

// The version byte that begins a record in the legacy changeset format.
const legacyFormatVersion = byte(1)

// SerializeLegacyStateWALBlock returns the legacy record payload earlier releases wrote for a block: the version byte,
// then each changeset's protobuf encoding prefixed by its length as a uvarint.
func SerializeLegacyStateWALBlock(changeSets []*proto.NamedChangeSet) ([]byte, error) {
	buf := []byte{legacyFormatVersion}
	var lengthPrefix [binary.MaxVarintLen64]byte
	for i, changeSet := range changeSets {
		if changeSet == nil {
			return nil, fmt.Errorf("changeset %d is nil", i)
		}
		marshaled, err := changeSet.Marshal()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal changeset %d: %w", i, err)
		}
		n := binary.PutUvarint(lengthPrefix[:], uint64(len(marshaled)))
		buf = append(buf, lengthPrefix[:n]...)
		buf = append(buf, marshaled...)
	}
	return buf, nil
}

// WriteLegacyStateWALBlocks appends blocks to the state WAL at path in the legacy changeset format, creating the WAL
// if it does not exist. No state WAL may be open on path.
func WriteLegacyStateWALBlocks(
	// The directory of the state WAL.
	path string,
	// The block number of blocks[0]. It must follow the WAL's last block, if it has one.
	firstBlock uint64,
	// The changesets of each block, in block order.
	blocks [][]*proto.NamedChangeSet,
) error {
	wal, err := seiwal.NewGenericWAL[[]*proto.NamedChangeSet](
		seiwal.DefaultConfig(path, "changeset-format-writer"),
		SerializeLegacyStateWALBlock,
		func([]byte) ([]*proto.NamedChangeSet, error) {
			return nil, fmt.Errorf("the changeset format writer does not read")
		},
	)
	if err != nil {
		return fmt.Errorf("failed to open the state WAL at %s: %w", path, err)
	}
	block := firstBlock
	for _, changeSets := range blocks {
		if err := wal.Append(block, changeSets); err != nil {
			_ = wal.Close()
			return fmt.Errorf("failed to append block %d: %w", block, err)
		}
		block++
	}
	if err := wal.Flush(); err != nil {
		_ = wal.Close()
		return fmt.Errorf("failed to flush the state WAL: %w", err)
	}
	if err := wal.Close(); err != nil {
		return fmt.Errorf("failed to close the state WAL: %w", err)
	}
	return nil
}
