// Package seitoml reads, edits and writes the node's sei.toml.
//
// A File is a mutable in-memory document for one goroutine at a time.
//
// Apart from the two keys below, the file holds only what an operator decided. A key present in it is
// authoritative; a key absent from it resolves to the running binary's default for the node's mode.
// Nothing here writes a default into the file, because a value the binary put there reads exactly like
// one an operator chose.
//
// Two keys at the top level describe the file rather than configure the node, and Values leaves both
// out.
//
//	schema_version   which migration the file has reached
//	node_mode        which mode's defaults its values were chosen against
//
// schema_version counts migrations, one per migration, and is deliberately not a release version. Parse
// reads both keys before it returns a file, so every verb below answers for one whose schema and mode are
// established. It refuses a counter that is absent, not a whole number, below the first schema, or ahead
// of the one this binary understands, and a mode that is absent, not text, or empty.
//
// # What This Package Is Not
//
// Nothing here migrates a file. This package reads and writes the counter; whatever runs the steps
// arrives with them, and no step exists here.
//
// Nothing here resolves a value or knows what keys exist. A key this file carries may be one no section
// declares, and this package does not say so.
//
// Nothing here writes a default, and nothing here decides whether the values make a bootable node.
//
// # Editing Preserves the Document
//
// An operator hand-edits this file, and the comments in it are how they explain a choice to whoever reads
// it next. So Set and Unset write the key they name and, when its table is new, that table's heading.
// They add nothing else, and every other line of content stays as it was. A comment above a key and a
// comment beside a value both survive an edit, and a key's own comment leaves with it when the key is
// unset.
//
// A table is named one way, by a heading, so there is no second spelling for an insert to choose between.
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
// pelletier/go-toml/v2, so this package decodes with it too, and a file this package accepts is a file
// the node's own decoder accepts. Whether its values make a bootable node is answered elsewhere.
//
// So the question a shape has to answer is put to that decoder rather than to a list kept here. Parse asks
// it. So does a Set that adds a key: it inserts, renders, asks again, and undoes the write and names the
// key when the answer is no. A Set that replaces a value on an existing line changes no shape and does
// not ask, nor does Unset, and Save asks once more over the whole document before anything reaches disk.
// So a shape nobody anticipated is refused as surely as one somebody did, and nothing reaches a node's
// disk unread.
//
// Two shapes are checked here as well, and deliberately: a repeated key and a repeated heading. The
// decoder refuses both, so nothing about whether the file loads rests on these; what they add is the
// diagnosis. They name the dotted key an operator typed and say that an edit reaches only the first,
// where the decoder names a line and reports that the name already exists. Both are the mistake
// hand-editing produces most.
//
// The refusals in the next section are the other direction: shapes the decoder accepts and this file
// does not carry.
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
//   - a dotted key, whose segments before the last name tables with no line of their own, so a key
//     added to one of those tables has nowhere to go
//   - an array of tables, which flattens to one key holding a list of tables, so no entry has a line of
//     its own to edit
//
// Every segment of a key is a lower-case bare TOML key: letters, digits, underscores and hyphens. Anything else has to be
// quoted where it is written, and a quoted key is spelled one way by the decoder and another by a
// lookup, so Values would report a key Get answers absent for. Set, Unset and Get fold a caller's key to
// lower case and then hold it to that rule, so a key one of them writes is a key the file reads back.
package seitoml
