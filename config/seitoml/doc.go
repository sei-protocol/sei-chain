// Package seitoml reads, edits and writes the node's sei.toml.
//
// A File is a mutable in-memory document for one goroutine at a time.
//
// The file holds only what an operator decided. A key present in it is authoritative; a key absent
// from it resolves to the running binary's default for the node's mode. Nothing here writes a
// default into the file, because a value the binary put there reads exactly like one an operator
// chose.
//
// Two keys at the top level describe the file rather than configure the node, and Values leaves both
// out.
//
//	schema_version   which migration the file has reached
//	node_mode        which mode's defaults its values were chosen against
//
// schema_version counts migrations, one per migration, and is deliberately not a release version. Parse
// refuses a file whose counter is absent, below the first schema, or ahead of the one this binary
// understands, so every verb below answers from a file whose shape is established. Nothing here migrates
// a file; this package reads and writes the counter, and the chain that acts on it arrives with the
// migrations. Nothing here resolves a value or knows what keys exist.
//
// # Editing Preserves The Document
//
// An operator hand-edits this file, and the comments in it are how they explain a choice to whoever
// reads it next. So Set and Unset change the value they name and add no line the caller did not ask
// for, leaving every other line of content as it was. A comment above a key and a comment beside a
// value both survive an edit, and a key's own comment leaves with it when the key is unset.
//
// Vertical spacing normalises once, on the first save of a file nothing has saved before, and holds
// from then on.
//
// Every write is atomic. A save lands in full or not at all, and a failed save leaves no temporary file
// behind.
//
// A value read from the file is written back as the same type.
//
// # One Decoder Decides What Parses
//
// The decoder is the one a node reads its configuration with. viper decodes TOML with
// pelletier/go-toml/v2, so this package decodes with it too, which makes "this file parses" and "this
// node can boot from it" one statement.
//
// So the question a shape has to answer is put to that decoder rather than to a list kept here. Parse
// asks it, and Set renders the document and asks it again, undoing the write and naming the key when the
// answer is no. Two of the decoder's answers are also checked here ahead of it, a repeated heading and a
// repeated key, because those name the key and say what an edit would reach where the decoder names a
// line.
//
// The editing parser is a second library doing a different job: it locates lines and preserves comments,
// and stops short of interpreting a literal.
//
// # What This File May Hold
//
// These are refused although the decoder accepts them, because this package has to write back what it
// reads:
//
//   - an infinity or a NaN, which have no form to write
//   - a date or a time, which nothing configures a node with and which cannot be written back as the
//     type it was read as
//   - an inline table, whose keys flatten into the same space a table's do, so an edit to one of them
//     has no line of its own to change
//   - an array of tables, which flattens to one key holding a list of tables, so no entry has a line of
//     its own to edit
//
// A key is a lower-case bare TOML key: letters, digits, underscores and hyphens. Anything else has to be
// quoted where it is written, and a quoted key is spelled one way by the decoder and another by a
// lookup, so Values would report a key Get answers absent for. Set, Unset and Get fold a caller's key to
// lower case and then hold it to that rule, so a key one of them writes is a key the file reads back.
package seitoml
