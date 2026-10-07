package flatkv

import (
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
)

// keyArena hands out immutable strings carved from one buffer of fixed capacity.
//
// A string from here keeps the whole buffer alive, so anything that retains one past the version that wrote it must
// copy it. A string from here must never be returned through flatKV's public API: the caller could keep it, and with
// it the whole buffer, for any length of time.
type keyArena struct {
	// The bytes handed out so far. Its capacity is the arena's size, and it never grows past it.
	buf []byte
}

// newKeyArena returns an arena that holds capacity bytes of keys.
func newKeyArena(capacity int) keyArena {
	return keyArena{buf: make([]byte, 0, capacity)}
}

// intern copies key into the arena and returns it as a string. A key that does not fit in the arena's remaining
// capacity gets its own allocation.
func (a *keyArena) intern(key []byte) string {
	if len(key) > cap(a.buf)-len(a.buf) {
		return string(key)
	}

	// The append stays within capacity, so it never moves the bytes earlier strings alias.
	start := len(a.buf)
	a.buf = append(a.buf, key...)
	return utils.UnsafeBytesToString(a.buf[start:])
}
