// Package seitoml reads, edits and writes the node's sei.toml.
//
// The file holds only what an operator decided. A key present in it is authoritative; a key absent
// from it resolves to the running binary's default for the node's mode. Nothing here writes a
// default into the file, because a value the binary put there reads exactly like one an operator
// chose.
//
// Two keys at the top level describe the file rather than configure the node, and Values leaves both
// out so a check comparing written keys against the declared set never reports them as keys no section
// owns.
//
//	schema_version   which migration the file has reached
//	node_mode        which mode's defaults its values were chosen against
//
// schema_version is a counter that rises by exactly one per migration, and a migration chain reads it
// to decide which steps a file still needs. It is deliberately not a release version. Most releases
// change no schema, so a release version could not answer whether the schema moved between two of
// them without a release-to-schema table, which is this counter reintroduced as an indirection.
// Releases also do not form the total order a chain needs: a hotfix can ship after a later minor, so
// ordering steps by release would run them in an order nobody intended.
//
// Nothing here migrates a file. This package reads the counter and writes it; the chain that acts on it
// arrives with the migrations themselves. A file whose counter is ahead of this binary's is refused,
// because a release migrates the file on the node's own disk and rolling the binary back does not roll
// the file back with it. Read anyway, the older binary would apply only the keys it still recognises.
//
// Editing preserves the document. An operator may hand-edit the file, and comments are how they explain
// a choice to whoever reads it next, so set and unset change the one line they name and leave every other
// line of content untouched. Vertical spacing is normalised once, on the first save of a
// file nothing has saved before, and holds steady after that. This is why the package edits a parsed
// document rather than re-rendering a decoded map, which would drop every comment in the file.
//
// Every write is atomic. A node cannot boot from a configuration file a crash truncated mid-write,
// so a save lands in full or not at all.
//
// A value has to survive a round trip as its own type. TOML tells a float from an integer by the
// fractional part, so an integral float needs one written explicitly or it reads back as an integer,
// and a key declared as a float would resolve as one type from a node's own files and another from its
// sei.toml. Infinities and NaN have no form here at all, and are refused in both directions rather than
// written as a line no reader can load.
//
// # What This File May Hold
//
// Values come from the decoder a node reads its configuration with. viper decodes TOML with
// pelletier/go-toml/v2, so this package does too, which makes "this file parses here" and "the node can
// boot from it" the same statement rather than two that can drift apart.
//
// That decoder is therefore the authority on shape, and Parse asks it rather than keeping a list. A
// name used for both a value and a table, a table defined twice, a key written twice, a table given a
// heading after an ancestor's dotted key already created it: all of it is one question with one answer.
// A list kept here instead missed three of those, and every one of them produced a file this package
// would save and no node could read.
//
// The editing parser is a second library and does a different job: it locates lines and preserves
// comments, and stops short of interpreting a literal, because deciding what an underscore-separated
// integer or a multi-line string means is a second implementation of the specification and a
// hand-written one went wrong in four places.
//
// Every edit is asked the same question. Set writes the key, renders, and offers the result to the
// decoder; if the document no longer reads, the write is undone and the key is named. So a shape nobody
// anticipated is refused as surely as one somebody did, which is the property enumerating shapes by hand
// could not give.
//
// Four things that decoder allows are refused anyway, because this package has to write back what it
// reads:
//
//   - an infinity or a NaN, which have no form to write
//   - a date or a time, which nothing configures a node with and which cannot be written back as the
//     type it was read as
//   - an inline table, whose keys flatten into the same space a table's do, so an edit to one of them
//     has no line of its own to change
//   - an array of tables, where every entry but the last disappears from the flattened key space
//
// A key is a bare TOML key: lower-case letters, digits, underscores and hyphens. Anything else has to be
// quoted where it is written, and a quoted key is spelled one way by the decoder and another by a
// lookup, so Values would report a key Get answers absent for. Set, Unset and Get apply that same rule
// to the key a caller hands them, so a key one of them writes is a key the file reads back.
package seitoml
