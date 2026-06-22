package hashlog

import "github.com/sei-protocol/sei-chain/sei-db/proto"

// Hashes the diff of a block's state. Order sensistive.
//
// This is NOT a cryptographically secure hash function. It's purpose is to be a canary in the coal mine for deviations
// in a block's diff, not to detect adversarially crafted collisions. This method needs to be fast with low overhead.
func hashDiff(cs []*proto.NamedChangeSet) []byte {
	return nil
}
