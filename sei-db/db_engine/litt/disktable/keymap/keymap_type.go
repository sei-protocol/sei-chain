//go:build littdb_wip

package keymap

// KeymapType represents the type of a keymap.
type KeymapType string

// PebbleDBKeymapType is the type of a PebbleDBKeymap.
const PebbleDBKeymapType = "PebbleDBKeymap"

// UnsafePebbleDBKeymapType is similar to PebbleDBKeymapType, but it is not safe to use in production.
// It runs a lot faster, but with weaker crash recovery guarantees.
const UnsafePebbleDBKeymapType = "UnsafePebbleDBKeymap"

// MemKeymapType is the type of a MemKeymap.
const MemKeymapType = "MemKeymap"
