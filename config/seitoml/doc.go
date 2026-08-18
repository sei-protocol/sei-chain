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
// Editing preserves the document. An operator may hand-edit the file, and comments are how they
// explain a choice to whoever reads it next, so set and unset change the one line they name and leave
// every other line of content untouched. Vertical spacing is normalised once, on the first save of a
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
// # Shapes This File Does Not Carry
//
// TOML permits more shapes than a node's configuration uses, and Parse refuses these rather than
// reading them into something a later verb cannot write back:
//
//   - an inline table, whose keys flatten into the same space a table's do, so editing one of them
//     defines the table a second time and produces a file a conforming reader will not load
//   - an array of tables, where every entry but the last disappears from the flattened key space
//   - a table heading that appears twice, where an edit reaches the first and a read answers from the
//     last
//   - a key or heading segment that is not lower case, which is read under a name that is not the one
//     written
//   - a key outside a bare TOML key, meaning anything but lower-case letters, digits, underscores and
//     hyphens, because a quoted key is spelled one way by the decoder and another by a lookup
//   - a key written twice in one table, which an edit reaches only the first of
//   - a date or a time, which nothing configures a node with, and which cannot be written back as the
//     type it was read as
//   - one name used for both a value and a table, which names the same thing twice
//
// Set, Unset and Get apply the same rule to the key a caller hands them, so a key one of them writes is
// a key the file reads back. Set also refuses a key that would name a value where a table already is,
// or the reverse, because the file that produces parses cleanly and can never be read.
//
// Each was previously accepted and then lost or corrupted further in. Refusing at the door is what
// lets every verb below assume the document holds only shapes it can read and write back, and it is
// only free while no operator has written a file that uses them.
//
// Reading and editing use different libraries, on purpose. The editing parser locates lines and
// preserves comments, and stops short of interpreting a literal; deciding what an underscore-separated
// integer or a multi-line string means is a second implementation of the specification. Values come
// from a conforming decoder instead, so the shape of a literal is somebody else's problem and the file
// reads the way every other TOML reader reads it.
//
// Two things that decoder allows are still refused here, because this package has to write back what it
// reads: an infinity or a NaN, which have no form to write, and a date, which would come back as a time
// this package cannot render.
package seitoml
