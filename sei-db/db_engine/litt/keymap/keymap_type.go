package keymap

// KeymapType represents the type of a keymap.
type KeymapType string

// LevelDBKeymapType is the type of a LevelDBKeymap.
const LevelDBKeymapType = "LevelDBKeymap"

// UnsafeLevelDBKeymapType is similar to LevelDBKeymapType, but it is not safe to use in production.
// It runs a lot faster, but with weaker crash recovery guarantees.
const UnsafeLevelDBKeymapType = "UnsafeLevelDBKeymap"

// PebbleKeymapType is the type of a PebbleKeymap.
const PebbleKeymapType = "PebbleKeymap"

// UnsafePebbleKeymapType is similar to PebbleKeymapType, but it is not safe to use in production.
// It runs a lot faster, but with weaker crash recovery guarantees.
const UnsafePebbleKeymapType = "UnsafePebbleKeymap"

// MemKeymapType is the type of a MemKeymap.
const MemKeymapType = "MemKeymap"
