// Package seitoml reads, edits and writes the node's sei.toml.
//
// The file holds only what an operator decided. A key present in it is authoritative; a key absent
// from it resolves to the running binary's baseline for the node's mode. Nothing here writes a
// baseline into the file, because a value the binary put there reads exactly like one an operator
// chose.
//
// Three keys at the top level describe the file rather than configure the node, and Values leaves
// all three out so a check comparing written keys against the declared set never reports them as
// keys no section owns.
//
//	schema_version   which migration the file has reached
//	node_mode        which mode's baselines its values were chosen against
//	generated_by     which release last produced or transformed it
//
// The first two are machinery and the third is not, and the difference matters enough to state.
//
// schema_version is a counter that rises by exactly one per migration, and it is what the chain reads
// to decide which steps a file still needs. It is deliberately not a release version. Most releases
// change no schema, so a release version could not answer whether the schema moved between two of
// them without a release-to-schema table, which is this counter reintroduced as an indirection.
// Releases also do not form the total order a chain needs: a hotfix can ship after a later minor, so
// ordering steps by release would run them in an order nobody intended.
//
// generated_by is provenance. Nothing reads it to decide anything, which is what lets it be absent
// without consequence: the release reaches the binary through a linker flag the release build sets,
// so a binary built any other way knows none and a file it writes simply omits the key. Anything
// branching on it would turn every development build into a node that cannot read its own
// configuration, and a test drives every reader over a file recording a release, no release, and a
// release no build ever was, requiring identical answers.
//
// Editing preserves the document. An operator may hand-edit the file, and comments are how they
// explain a choice to whoever reads it next, so set and unset change the one line they name and
// leave the rest byte for byte. That is why this package edits a parsed document rather than
// re-rendering a decoded map.
//
// Every write is atomic. A node cannot boot from a configuration file a crash truncated mid-write,
// so a save lands in full or not at all.
package seitoml
