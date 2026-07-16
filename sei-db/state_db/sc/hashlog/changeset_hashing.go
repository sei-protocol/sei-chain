package hashlog

import (
	"encoding/binary"

	"github.com/cespare/xxhash/v2"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Hashes the changeset of a block's state. Order sensitive.
//
// This is NOT a cryptographically secure hash function. Its purpose is to be a canary in the coal mine for
// deviations in a block's changeset, not to detect adversarially crafted collisions. This method needs to be fast
// with low overhead.
//
// The encoding is fully self-delimiting so that distinct inputs cannot collide merely because their byte
// streams happen to line up:
//   - Every field (name, key, value) is length-prefixed, so e.g. keys "ab"+"" cannot alias "a"+"b".
//   - The change-set count and each change set's pair count are written up front, so a pair can never be
//     confused with the start of the next change set, and the grouping of pairs into change sets is significant.
//
// The counts are computed over only the non-nil entries that are actually folded in, so the framing stays
// consistent with the data even when nil entries are defensively skipped.
func hashChangeset(cs []*proto.NamedChangeSet) []byte {
	digest := xxhash.New()

	var lengthBuf [binary.MaxVarintLen64]byte
	writeUvarint := func(v uint64) {
		n := binary.PutUvarint(lengthBuf[:], v)
		_, _ = digest.Write(lengthBuf[:n])
	}
	writeBytes := func(b []byte) {
		writeUvarint(uint64(len(b)))
		_, _ = digest.Write(b)
	}

	// Frame the stream with the number of change sets actually hashed. Nil entries are skipped defensively:
	// this runs on the background hasher goroutine, where a nil-pointer dereference would panic and take down
	// the node rather than just losing one changeset hash.
	changeSetCount := uint64(0)
	for _, ncs := range cs {
		if ncs != nil {
			changeSetCount++
		}
	}
	writeUvarint(changeSetCount)

	for _, ncs := range cs {
		if ncs == nil {
			continue
		}
		writeBytes([]byte(ncs.Name))

		pairCount := uint64(0)
		for _, pair := range ncs.Changeset.Pairs {
			if pair != nil {
				pairCount++
			}
		}
		writeUvarint(pairCount)

		for _, pair := range ncs.Changeset.Pairs {
			if pair == nil {
				continue
			}
			if pair.Delete {
				_, _ = digest.Write([]byte{1})
			} else {
				_, _ = digest.Write([]byte{0})
			}
			writeBytes(pair.Key)
			writeBytes(pair.Value)
		}
	}

	sum := make([]byte, 8)
	binary.BigEndian.PutUint64(sum, digest.Sum64())
	return sum
}
