package hashlog

import (
	"encoding/binary"

	"github.com/cespare/xxhash/v2"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Hashes the diff of a block's state. Order sensitive.
//
// This is NOT a cryptographically secure hash function. Its purpose is to be a canary in the coal mine for
// deviations in a block's diff, not to detect adversarially crafted collisions. This method needs to be fast
// with low overhead.
//
// Every field is length-prefixed before being folded into the digest so that distinct inputs cannot collide
// merely because their concatenations happen to be equal (e.g. keys "ab"+"" vs "a"+"b").
func hashDiff(cs []*proto.NamedChangeSet) []byte {
	digest := xxhash.New()

	var lengthBuf [binary.MaxVarintLen64]byte
	writeBytes := func(b []byte) {
		n := binary.PutUvarint(lengthBuf[:], uint64(len(b)))
		_, _ = digest.Write(lengthBuf[:n])
		_, _ = digest.Write(b)
	}

	for _, ncs := range cs {
		// Defensively skip nil entries: this runs on the background hasher goroutine, where a nil-pointer
		// dereference would panic and take down the node rather than just losing one diff hash.
		if ncs == nil {
			continue
		}
		writeBytes([]byte(ncs.Name))
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
