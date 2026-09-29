package flatkv

import (
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// keyArenaChunkSize is how many bytes of keys one arena chunk holds.
const keyArenaChunkSize = 64 * 1024

// keyArena hands out immutable strings carved from shared chunks.
//
// A string from here keeps its whole chunk alive, so anything that retains one past the version that wrote it must
// copy it.
type keyArena struct {
	// The chunk being carved from. Nil until the first key.
	chunk []byte

	// How much of chunk has been handed out.
	used int
}

// intern copies key into the arena and returns it as a string. A key longer than a chunk gets its own allocation.
func (a *keyArena) intern(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	if len(key) > keyArenaChunkSize {
		return string(key)
	}

	if a.chunk == nil || a.used+len(key) > len(a.chunk) {
		// The old chunk's unused tail is abandoned. The strings already carved from it alias it, so it is never
		// written again.
		a.chunk = make([]byte, keyArenaChunkSize)
		a.used = 0
	}

	start := a.used
	a.used += copy(a.chunk[start:], key)
	return util.UnsafeBytesToString(a.chunk[start:a.used])
}
