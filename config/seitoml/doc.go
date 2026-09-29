// Package seitoml reads, edits and writes the node's sei.toml. A File is a mutable in-memory document
// for one goroutine at a time.
//
// The file holds only values an operator chose, plus two keys describing the file itself, which Values
// leaves out:
//
//	schema_version   the migration the file has reached; not a release version
//	node_mode        the mode whose defaults the values were chosen against
//
// Parse refuses a file whose schema_version or node_mode is missing or invalid.
//
// Set and Unset change only the key they name, and its table heading when the table is new; comments and
// all other lines are preserved. Saves are atomic. A value is written back as the type it was read as.
//
// Values are decoded with pelletier/go-toml/v2, the decoder viper uses, so a file this package accepts
// is one the node can read. Parse, a Set that adds a key, and Save each re-check the document with it.
// Repeated keys and headings get their own error naming the key.
//
// Although the decoder accepts them, the file refuses shapes this package cannot write back or edit in
// place: infinity and NaN, dates and times, inline tables, dotted keys, and arrays of tables. Every key
// segment must be a lower-case bare key (letters, digits, underscores, hyphens); Set, Unset and Get
// lower-case the key they are given.
package seitoml
