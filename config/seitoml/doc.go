// Package seitoml reads, edits and writes the node's sei.toml.
//
// The file holds only what an operator decided. A key present in it is authoritative; a key
// absent from it resolves to the running binary's baseline for the node's mode. Nothing here
// writes a baseline into the file, because a value the binary put there would be
// indistinguishable from one an operator chose.
//
// Editing preserves the document. An operator may hand-edit the file, and comments are how they
// explain a choice to whoever reads it next, so set and unset change the one line they are asked
// to change and leave the rest byte for byte. That is why this package edits a parsed document
// rather than re-rendering a decoded map.
//
// Every write is atomic. A configuration file truncated by a crash mid-write is one a node cannot
// boot from, so a save lands in full or not at all.
package seitoml
