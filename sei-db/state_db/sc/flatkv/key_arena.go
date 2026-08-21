package flatkv

import (
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// keyArenaChunkSize is how much a single arena chunk holds. Large enough that a block's worth of
// physical keys costs tens of allocations rather than tens of thousands, small enough that a chunk
// outliving its block wastes little.
const keyArenaChunkSize = 64 * 1024

// keyArena hands out immutable strings carved from large chunks, so that retaining one string per
// physical key costs one allocation per chunk instead of one per key.
//
// # Lifetime
//
// A string from here shares its backing array with every other string carved from the same chunk, so
// the chunk lives as long as the longest-lived of them. That is the whole point when the strings die
// together — a block's keys are dropped as a set when the block's version retires — and it is a leak
// in slow motion when they do not. Anything that keeps a key past its block's retirement must copy
// it: see the clones in the snapshot engine, at the two places a key outlives the version that wrote
// it.
//
// # Mutation
//
// A chunk is written once, left alone, and never revisited after it fills. The strings alias it, so
// writing to a chunk after carving from it would mutate a string in place.
type keyArena struct {
	// The chunk being carved from. Nil until the first key.
	chunk []byte

	// How much of chunk has been handed out.
	used int
}

// intern copies key into the arena and returns it as a string.
//
// A key too large for a chunk gets its own exact-sized allocation rather than a chunk of its own,
// since one oversized key should not strand the rest of a chunk.
func (a *keyArena) intern(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	if len(key) > keyArenaChunkSize {
		return string(key)
	}

	if a.chunk == nil || a.used+len(key) > len(a.chunk) {
		// The old chunk is abandoned rather than filled to the last byte: the strings already carved
		// from it keep it alive for as long as they live, and chasing its final few bytes would only
		// tie one more key's lifetime to it.
		a.chunk = make([]byte, keyArenaChunkSize)
		a.used = 0
	}

	start := a.used
	a.used += copy(a.chunk[start:], key)
	return util.UnsafeBytesToString(a.chunk[start:a.used])
}
