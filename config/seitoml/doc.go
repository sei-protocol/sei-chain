// Package seitoml reads, edits and writes the node's sei.toml.
//
// The file holds only what an operator decided. A key present in it is authoritative; a key absent
// from it resolves to the running binary's baseline for the node's mode. Nothing here writes a
// baseline into the file, because a value the binary put there reads exactly like one an operator
// chose.
//
// Editing preserves the document. An operator may hand-edit the file, and comments are how they
// explain a choice to whoever reads it next, so set and unset change the one line they name and
// leave the rest byte for byte. That is why this package edits a parsed document rather than
// re-rendering a decoded map.
//
// Every write is atomic. A node cannot boot from a configuration file a crash truncated mid-write,
// so a save lands in full or not at all.
package seitoml
