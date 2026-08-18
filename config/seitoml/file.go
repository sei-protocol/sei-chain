package seitoml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/scanner"
)

// SchemaVersion is the schema this binary writes and reads.
const SchemaVersion = 1

// VersionKey records which schema the file follows.
//
// At the document's top level rather than inside a table, so reading it never depends on knowing
// the shape of the file it describes.
const VersionKey = "schema_version"

// ModeKey records which node mode the file's values resolve for.
//
// At the top level beside VersionKey, not inside a section, because the mode selects which defaults
// apply and so cannot itself have a per-mode default. It is also the only durable record of an
// archive node: seid init writes config.toml's mode as "full" for one, since Tendermint has no
// archive mode, so nothing else on disk distinguishes the two.
const ModeKey = "node_mode"

// newFileMode is the permission a file created here gets, and only that.
//
// A save onto an existing file inherits whatever mode that file already has, so this value describes
// the first save and nothing after it. Narrow rather than the usual 0644 because a configuration names
// the paths of a node's key files and its peers, and because it is the narrower of the two modes used
// by the files it consolidates. Widening one an operator deliberately narrowed is worse than a default
// nobody wanted, which is why the existing mode wins.
const newFileMode os.FileMode = 0o600

// File is a parsed sei.toml that survives editing with its comments and layout intact.
type File struct {
	doc *tomledit.Document
}

// Parse reads a document from r.
func Parse(r io.Reader) (*File, error) {
	doc, err := tomledit.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse sei.toml: %w", err)
	}
	f := &File{doc: doc}
	if err := f.refuseUnsupportedShapes(); err != nil {
		return nil, err
	}
	return f, nil
}

// refuseUnsupportedShapes rejects TOML this format does not carry.
//
// TOML permits more shapes than a node's configuration uses, and each of these was previously accepted
// and then lost or corrupted somewhere downstream: a mixed-case key read back under a different name,
// an inline table that Set split into a second definition of the same table, an array of tables whose
// earlier entries vanished from Values. Refusing at the door is what keeps one answer per key, and it
// is only free while no operator has written a file that uses them.
func (f *File) refuseUnsupportedShapes() error {
	headings := map[string]bool{}
	// Every entry in Sections is a named table, so each carries a heading; the global section is a field
	// of its own and is not in here.
	for _, s := range f.doc.Sections {
		if s.IsArray {
			return fmt.Errorf("[[%s]] is an array of tables, which this file does not carry; every key "+
				"holds one value, so a repeated section has no reading", s.Name)
		}
		if err := keyIsAddressable(s.Name); err != nil {
			return fmt.Errorf("table [%s]: %w", s.Name, err)
		}
		name := s.Name.String()
		if headings[name] {
			return fmt.Errorf("[%s] appears more than once, and an edit reaches only the first, so a "+
				"value written into this file would not be the one read back", name)
		}
		headings[name] = true
	}

	var bad error
	written := map[string]bool{}
	f.doc.Scan(func(full parser.Key, e *tomledit.Entry) bool {
		if e.KeyValue == nil {
			return true
		}
		if err := keyIsAddressable(full); err != nil {
			bad = err
			return false
		}
		if err := valueIsAddressable(full, e.Value); err != nil {
			bad = err
			return false
		}
		// One value per key, checked here rather than left to the decoder. A duplicate is the one shape
		// the editing parser accepts and a conforming decoder rejects, so without this the file parses
		// and then every read of it fails.
		key := full.String()
		if written[key] {
			bad = fmt.Errorf("%s is written more than once, and an edit reaches only the first, so a "+
				"value written into this file would not be the one read back", key)
			return false
		}
		written[key] = true
		return true
	})
	return bad
}

// keyIsAddressable reports whether every segment of a key can be read back as written.
//
// A source enumerates lower-cased, so an upper-case segment is read under a name that is not the one
// in the file, and a segment carrying a dot or a space cannot be split back into the segments it came
// from.
func keyIsAddressable(key parser.Key) error {
	for _, segment := range key {
		if segment != strings.ToLower(segment) {
			return fmt.Errorf("%q is not lower case, and this file's keys are read lower-cased, so it "+
				"would be read under a name that is not the one written here", segment)
		}
		if strings.ContainsAny(segment, ". ") {
			return fmt.Errorf("%q carries a dot or a space, so it cannot be addressed: a key is split "+
				"on dots, and no spelling of this one splits back into it", segment)
		}
	}
	return nil
}

// valueIsAddressable rejects an inline table, at the top level of a value or inside an array.
//
// An inline table holds several keys in one written value. Its leaves flatten into the same dotted
// space a table's do, so a caller works in that space and an edit there defines the table a second
// time, producing a file a conforming reader refuses to load.
func valueIsAddressable(key parser.Key, v parser.Value) error {
	switch x := v.X.(type) {
	case parser.Token:
		switch x.Type {
		case scanner.DateTime, scanner.LocalDate, scanner.LocalTime, scanner.LocalDateTime:
			return fmt.Errorf("%s is a date or a time, which this file does not carry; nothing "+
				"configures a node with one, and it cannot be written back as the type it was read as",
				key)
		}
	case parser.Inline:
		return fmt.Errorf("%s is an inline table, which this file does not carry; write it as a [%s] "+
			"table so each key it holds can be edited on its own line", key, key)
	case parser.Array:
		for _, item := range x {
			element, ok := item.(parser.Value)
			if !ok {
				continue
			}
			if err := valueIsAddressable(key, element); err != nil {
				return err
			}
		}
	}
	return nil
}

// Load reads the document at path.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the caller's configured path is the subject
	if err != nil {
		return nil, err
	}
	f, err := Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// New returns an empty document carrying this binary's schema version and the given node mode.
//
// The mode is required rather than optional. Every value a caller goes on to write resolves for one
// mode, and a file that does not say which cannot be compared against a binary's defaults or checked
// against the mode the node actually runs.
func New(mode string) (*File, error) {
	if mode == "" {
		return nil, fmt.Errorf("a sei.toml needs a node mode: every value in it resolves for one, and " +
			"a file that omits it cannot be compared against this binary's defaults")
	}
	f := &File{doc: &tomledit.Document{Global: &tomledit.Section{}}}
	if err := f.Set(VersionKey, SchemaVersion); err != nil {
		return nil, err
	}
	if err := f.Set(ModeKey, mode); err != nil {
		return nil, err
	}
	return f, nil
}

// Mode returns the node mode the file's values resolve for.
//
// An absent mode is an error rather than a guess. Guessing picks one binary's idea of a default and
// silently compares an archive node's file against a validator's defaults, which is the mistake
// this key exists to make impossible.
func (f *File) Mode() (string, error) {
	mode, present, err := f.stringValue(ModeKey)
	switch {
	case err != nil:
		return "", err
	case !present:
		return "", fmt.Errorf("sei.toml has no %s. Every value in it resolves for one node mode, so "+
			"without it nothing can tell an archive node's file from a validator's", ModeKey)
	case mode == "":
		return "", fmt.Errorf("%s is empty", ModeKey)
	}
	return mode, nil
}

// Version returns the schema version the file records.
//
// An absent or unparsable version is an error, never a zero. A migration chain reads this to decide
// which steps to run, so guessing here transforms a file whose shape nobody established.
func (f *File) Version() (int, error) {
	n, present, err := f.intValue(VersionKey)
	switch {
	case err != nil:
		return 0, err
	case !present:
		return 0, fmt.Errorf("sei.toml has no %s. Its shape cannot be established, so no migration "+
			"can safely run against it and no reader can know which keys it is expected to carry",
			VersionKey)
	}
	if int(n) > SchemaVersion {
		// The rollback case, and the reason the counter exists. A release migrates the file forward on
		// the node's own disk, so rolling the binary back does not roll the file back with it. Read
		// anyway, this binary would silently ignore every key the newer schema added or renamed and boot
		// on a configuration neither release produced.
		return 0, fmt.Errorf("sei.toml is at %s %d and this binary understands %d. It was written by a "+
			"newer release, so reading it would apply only the keys this binary still recognises",
			VersionKey, n, SchemaVersion)
	}
	return int(n), nil
}

// Bytes renders the document.
func (f *File) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := tomledit.Format(&buf, f.doc); err != nil {
		return nil, fmt.Errorf("render sei.toml: %w", err)
	}
	return buf.Bytes(), nil
}

// Save writes the document to path, atomically.
//
// The rename makes it atomic, and the temporary file sits in the destination's own directory so the
// rename stays within one filesystem. A crash at any point leaves either the previous file or the
// new one, never a truncated file a node cannot parse.
func (f *File) Save(path string) error {
	raw, err := f.Bytes()
	if err != nil {
		return err
	}

	mode, err := modeToWrite(path)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary file beside %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Removing a temporary file that was already renamed fails harmlessly; leaving one behind
		// after a failed write does not, since the next save would find the directory littered.
		_ = os.Remove(tmpName)
	}()

	if err := writeAndSync(tmp, raw, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	// Past this point the new file is what the node will read, so a failure to flush the directory
	// entry is not a failure of the save. Returning one would tell a caller their change did not land
	// when it did, and the next thing they do is write it again or open an incident.
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("%w: %w", ErrNotDurable, err)
	}
	return nil
}

// ErrNotDurable reports that a save installed the file and could not flush the directory entry.
//
// The values are in place and a reader sees them. Only their survival of a machine losing power before
// the filesystem flushes on its own is unproven, so a caller that treats this as a failed write is
// wrong about what happened.
var ErrNotDurable = errors.New("the file is installed and its directory entry is not yet flushed")

// modeToWrite returns the permission a save should use, and refuses a destination it must not replace.
//
// An existing file keeps its own mode, so a save never widens what an operator narrowed. A symbolic
// link is refused: renaming onto one replaces the link with a regular file, leaving whatever it pointed
// at holding the old values, and nothing about the result says the link is gone.
func modeToWrite(path string) (os.FileMode, error) {
	info, err := os.Lstat(path)
	switch {
	case err != nil:
		return newFileMode, nil // no file there yet, which is the ordinary first save
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			target = "somewhere this process cannot read"
		}
		return 0, fmt.Errorf("%s is a symbolic link to %s. Writing here would replace the link with a "+
			"regular file and leave %s holding the old values; edit the target directly", path, target,
			target)
	default:
		return info.Mode().Perm(), nil
	}
}

// writeAndSync writes the whole payload, sets the mode, and flushes to the device.
//
// The sync makes the rename meaningful: without it the rename can land before the contents, leaving
// a file whose name is new and whose bytes are absent.
func writeAndSync(tmp *os.File, raw []byte, mode os.FileMode) error {
	defer func() { _ = tmp.Close() }()

	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	return tmp.Close()
}

// syncDir flushes the directory entry so the rename itself survives a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // the destination's own directory
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", dir, err)
	}
	return nil
}

// keyOf splits a dotted key into its parser path.
func keyOf(key string) (parser.Key, error) {
	if key == "" {
		return nil, fmt.Errorf("empty key")
	}
	parts := strings.Split(strings.ToLower(key), ".")
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("key %q has an empty segment", key)
		}
	}
	return parser.Key(parts), nil
}

// quoteInt renders an integer the way TOML spells one.
func quoteInt(n int64) string { return strconv.FormatInt(n, 10) }
