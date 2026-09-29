package seitoml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/scanner"
)

// SchemaVersion is the schema this binary writes and reads. It rises by one per migration and is not a
// release version.
const SchemaVersion = 1

// VersionKey is the top-level key recording which schema the file follows.
const VersionKey = "schema_version"

// ModeKey is the top-level key recording which node mode the file's values resolve for. It is the only
// on-disk record of an archive node, since config.toml records one as "full".
const ModeKey = "node_mode"

// newFileMode is the permission of a newly created file. A save onto an existing file keeps its mode.
const newFileMode os.FileMode = 0o600

// File is a parsed sei.toml that survives editing with its comments and layout intact. It is for one
// goroutine at a time: reads populate a cache.
type File struct {
	doc *tomledit.Document
	// values caches the last decode, and is nil whenever the document has changed since.
	values map[string]any
}

// changed drops the decode a read would otherwise reuse. Every edit calls it before mutating.
func (f *File) changed() { f.values = nil }

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
	// Checked here so no caller can use a file whose schema or mode is invalid.
	if _, err := f.Version(); err != nil {
		return nil, err
	}
	if _, err := f.Mode(); err != nil {
		return nil, err
	}
	return f, nil
}

// refuseUnsupportedShapes rejects TOML this package cannot edit in place or write back.
func (f *File) refuseUnsupportedShapes() error {
	headings := map[string]bool{}
	// Sections holds the named tables; the global section is separate.
	for _, s := range f.doc.Sections {
		if s.IsArray {
			return fmt.Errorf("[[%s]] is an array of tables, which this file does not carry; every key "+
				"holds one value, so a repeated section has no reading", s.Name)
		}
		if err := keyIsAddressable(s.Name); err != nil {
			return fmt.Errorf("table [%s]: %w", shortKey(s.Name), err)
		}
		name := s.Name.String()
		if headings[name] {
			// The decoder refuses this too; this error names the heading.
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
		if len(e.Name) > 1 {
			bad = fmt.Errorf("%s is written as a dotted key, which this file does not carry. Every "+
				"segment before the last names a table with no line of its own, so a key added to one "+
				"of those tables has nowhere to go; write [%s] as a section instead, and put %s in it",
				full, full[:len(full)-1], full[len(full)-1])
			return false
		}
		// The decoder refuses this too; this error names the key.
		key := full.String()
		if written[key] {
			bad = fmt.Errorf("%s is written more than once, and an edit reaches only the first, so a "+
				"value written into this file would not be the one read back", key)
			return false
		}
		written[key] = true
		return true
	})
	if bad != nil {
		return bad
	}
	// Every other collision is the decoder's to find.
	return f.decodable()
}

// keyIsAddressable reports whether every segment of a key is a lower-case bare key, within maxKeyDepth.
func keyIsAddressable(key parser.Key) error {
	if len(key) > maxKeyDepth {
		return fmt.Errorf("%s is %d segments deep and this file is read to %d. A setting here is a "+
			"section and a key inside it, so nothing legitimate reaches that depth", shortKey(key),
			len(key), maxKeyDepth)
	}
	for _, segment := range key {
		if segment == "" {
			return fmt.Errorf("%s has an empty segment, which names nothing", key)
		}
		if segment != strings.ToLower(segment) {
			return fmt.Errorf("%q is not lower case, and this file's keys are read lower-cased, so it "+
				"would be read under a name that is not the one written here", segment)
		}
		if bad := strings.IndexFunc(segment, notBareKeyRune); bad >= 0 {
			return fmt.Errorf("%q carries %q, so it is not a bare key. A bare key holds lower-case "+
				"letters, digits, underscores and hyphens; anything else has to be quoted in the file "+
				"and a dotted spelling of it does not split back into the segments it came from",
				segment, segment[bad:bad+1])
		}
	}
	return nil
}

// notBareKeyRune reports whether a character cannot appear in a lower-case bare TOML key.
func notBareKeyRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
		return false
	default:
		return true
	}
}

// valueIsAddressable rejects an inline table or a date/time, at the top level of a value or inside an
// array.
func valueIsAddressable(key parser.Key, v parser.Value) error {
	return valueIsAddressableWithin(key, v, 0)
}

// valueIsAddressableWithin is valueIsAddressable at an array nesting depth, refusing one past
// maxArrayDepth before the decode.
func valueIsAddressableWithin(key parser.Key, v parser.Value, depth int) error {
	if depth > maxArrayDepth {
		return fmt.Errorf("%s nests arrays %d deep and this file is read to %d. No setting here is a "+
			"list of lists, so nothing legitimate reaches that depth", key, depth, maxArrayDepth)
	}
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
			if err := valueIsAddressableWithin(key, element, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// The bounds this file is read within, far above anything legitimate configuration reaches. The byte
// bound applies before parsing; the depth bounds apply between parse and decode.
const (
	// maxFileBytes bounds the bytes Load will read. A file stating every declared key is a few tens
	// of kilobytes.
	maxFileBytes = 1 << 20
	// maxKeyDepth bounds the segments in one key. A setting is a section and a key inside it.
	maxKeyDepth = 8
	// maxArrayDepth bounds nesting inside a value. No setting here is a list of lists.
	maxArrayDepth = 8
)

// shortKey renders a key for a message, truncated past maxKeyDepth segments.
func shortKey(key parser.Key) string {
	if len(key) <= maxKeyDepth {
		return key.String()
	}
	return fmt.Sprintf("%s and %d more segments", key[:maxKeyDepth].String(), len(key)-maxKeyDepth)
}

// whatALinkToNothingIs returns an error if path is a dangling symlink, and nil otherwise, so a broken
// link is not mistaken for an absent file.
func whatALinkToNothingIs(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return nil
	}
	return fmt.Errorf("%s is a link to something that is not there", path)
}

// Load reads the document at path. A missing file reports an error matching fs.ErrNotExist.
func Load(path string) (*File, error) {
	// Checked before opening, since opening a FIFO blocks. Stat follows symlinks, which a mounted
	// ConfigMap uses.
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if dangling := whatALinkToNothingIs(path); dangling != nil {
			return nil, dangling
		}
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is a %s rather than a regular file, and this file is read as one",
			path, info.Mode().Type())
	}

	fh, err := os.Open(path) //nolint:gosec // the caller's configured path is the subject
	if err != nil {
		// A mounted file's target can be swapped out between the Stat and the open.
		if errors.Is(err, fs.ErrNotExist) {
			if dangling := whatALinkToNothingIs(path); dangling != nil {
				return nil, dangling
			}
		}
		return nil, err
	}
	defer func() { _ = fh.Close() }()

	// Bound the bytes actually read rather than trusting the Stat size.
	raw, err := io.ReadAll(io.LimitReader(fh, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxFileBytes {
		return nil, fmt.Errorf("%s holds more than %d bytes, which is what this file is read up to. A "+
			"file stating every key this binary declares is a small fraction of that, so one this large "+
			"is not that file", path, maxFileBytes)
	}
	f, err := Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// New returns an empty document carrying this binary's schema version and the given, required, node mode.
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

// Mode returns the node mode the file's values resolve for. An absent or empty mode is an error.
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

// Version returns the schema version the file records. An absent version, or one outside
// 1..SchemaVersion, is an error.
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
	if n < 1 {
		return 0, fmt.Errorf("sei.toml is at %s %d, and the first schema this format had is 1. Its shape "+
			"cannot be established, so no migration can safely run against it", VersionKey, n)
	}
	if n > int64(SchemaVersion) {
		// A binary rolled back past a migration.
		return 0, fmt.Errorf("sei.toml is at %s %d and this binary understands %d. It was written by a "+
			"newer release, so reading it would apply only the keys this binary still recognises",
			VersionKey, n, SchemaVersion)
	}
	// Narrowed after the bounds check so a wide value cannot wrap into range.
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

// Save atomically writes the document to path after checking that Parse accepts it. A destination that
// is a symlink or not a regular file is refused. An existing file keeps its permission; a new one gets
// newFileMode. A non-nil error means nothing was written.
func (f *File) Save(path string) error { return f.save(path, false) }

// SaveNew is Save for a path that must not exist. If anything is at path when the file is installed, it
// returns an error wrapping fs.ErrExist and leaves that entry untouched.
func (f *File) SaveNew(path string) error { return f.save(path, true) }

func (f *File) save(path string, mustBeNew bool) error {
	raw, err := f.Bytes()
	if err != nil {
		return err
	}
	if _, err := Parse(bytes.NewReader(raw)); err != nil {
		return err
	}

	mode := newFileMode
	if !mustBeNew {
		if mode, err = modeToWrite(path); err != nil {
			return err
		}
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary file beside %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Fails harmlessly once the file has been renamed; after a link it removes the extra name.
		_ = os.Remove(tmpName)
	}()

	if err := writeAndSync(tmp, raw, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := install(tmpName, path, mustBeNew); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	syncDir(dir)
	return nil
}

// install moves the written temporary file to path. A link, unlike a rename, fails on an existing entry,
// so a file that must be new cannot replace one created after any earlier check.
func install(tmpName, path string, mustBeNew bool) error {
	if mustBeNew {
		return os.Link(tmpName, path)
	}
	return os.Rename(tmpName, path)
}

// modeToWrite returns the permission a save should use: the existing file's, or newFileMode. It refuses
// a symlink or non-regular destination, which a rename would replace rather than write through.
func modeToWrite(path string) (os.FileMode, error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return newFileMode, nil // no file there yet, which is the ordinary first save
	case err != nil:
		return 0, fmt.Errorf("inspect %s: %w", path, err)
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			target = "somewhere this process cannot read"
		}
		return 0, fmt.Errorf("%s is a symbolic link to %s. Writing here would replace the link with a "+
			"regular file and leave %s holding the old values; edit the target directly", path, target,
			target)
	case !info.Mode().IsRegular():
		return 0, fmt.Errorf("%s is a %s, not a regular file. A save renames over it, which would "+
			"destroy it and write the configuration at its permission (%#o)", path,
			info.Mode().Type(), info.Mode().Perm())
	default:
		return info.Mode().Perm(), nil
	}
}

// writeAndSync writes the whole payload, sets the mode, and fsyncs before the caller renames.
// creachadair/atomicfile is not used because it does not sync.
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

// syncDir flushes dir's entries so a rename into it survives a power loss. It is best effort: the rename
// has already happened, and retrying a failed fsync is unreliable on Linux.
func syncDir(dir string) {
	d, err := os.Open(dir) //nolint:gosec // the destination's own directory
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
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
	out := parser.Key(parts)
	// So Set cannot write a key the next Parse refuses.
	if err := keyIsAddressable(out); err != nil {
		return nil, fmt.Errorf("key %q: %w", key, err)
	}
	return out, nil
}

// quoteInt renders an integer the way TOML spells one.
func quoteInt(n int64) string { return strconv.FormatInt(n, 10) }
