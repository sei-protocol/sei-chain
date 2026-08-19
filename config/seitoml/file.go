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

// SchemaVersion is the schema this binary writes and reads.
//
// A counter rising by one per migration, and not a release version. Most releases change no schema, so a
// release version could not answer whether the schema moved between two of them without a
// release-to-schema table, which is this counter reintroduced as an indirection. Releases also do not
// form the total order a chain needs: a hotfix can ship after a later minor, so ordering steps by
// release would run them in an order nobody intended.
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
//
// A File is for one goroutine at a time. Reading is not a pure operation: every read decodes the
// document and holds the result, so two concurrent reads of a shared File race.
type File struct {
	doc *tomledit.Document
	// values caches the last decode, and is nil whenever the document has changed since.
	//
	// Reading asks the decoder rather than the editing parser, which means rendering the document, so a
	// caller walking every declared key would otherwise render and decode once per key. Building a file
	// would be quadratic in its size for the same reason, since every edit checks the result.
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
	// The counter is read here rather than left to whoever calls Version, so no verb can answer from a
	// file whose schema this binary does not understand. Asked at the door, every read below is reading a
	// file whose shape is established; asked only by Version, a caller that never calls it resolves
	// values from a file a newer release wrote and boots on a configuration neither release produced.
	if _, err := f.Version(); err != nil {
		return nil, err
	}
	return f, nil
}

// refuseUnsupportedShapes rejects TOML this format does not carry.
//
// TOML permits more shapes than a node's configuration uses, and each of these reaches an edit that has
// nowhere to land: a mixed-case key is read back under a different name, an inline table and a dotted key
// each name a table with no line of its own, and an array of tables gives no entry a line of its own.
// Refusing at the door is what keeps one spelling per table and one answer per key, and it leaves every
// verb below with a document it can round-trip.
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
			// Also refused by the decoder, and kept for the same reason as a duplicate key: this says
			// which heading and what an edit would reach.
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
		// The decoder below refuses this too. It stays because it names the key and says what an edit
		// would do to it, where the decoder names a line, and a duplicate key is the mistake an operator
		// is most likely to make by hand.
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
	// One name used for both a value and a table, a table defined twice, a key written twice: all of it
	// is the decoder's answer rather than a list kept here. A hand-written list missed an implicitly
	// created table, an empty section, and any collision a hyphen sorted between.
	return f.decodable()
}

// keyIsAddressable reports whether every segment of a key can be read back as written.
//
// A source enumerates lower-cased, so an upper-case segment is read under a name that is not the one
// in the file, and a segment carrying a dot or a space cannot be split back into the segments it came
// from.
func keyIsAddressable(key parser.Key) error {
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

// notBareKeyRune reports whether a character cannot appear in a bare TOML key.
//
// A key outside this set has to be quoted where it is written, and the two readers of this file spell a
// quoted key differently: the decoder hands back the name itself, while looking one up rebuilds the
// quoting. Values would then report a key Get answers absent for.
func notBareKeyRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
		return false
	default:
		return true
	}
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
	if n < 1 {
		return 0, fmt.Errorf("sei.toml is at %s %d, and the first schema this format had is 1. Its shape "+
			"cannot be established, so no migration can safely run against it", VersionKey, n)
	}
	if n > int64(SchemaVersion) {
		// The rollback case, and the reason the counter exists. A release migrates the file forward on
		// the node's own disk, so rolling the binary back does not roll the file back with it. Read
		// anyway, this binary would silently ignore every key the newer schema added or renamed and boot
		// on a configuration neither release produced.
		return 0, fmt.Errorf("sei.toml is at %s %d and this binary understands %d. It was written by a "+
			"newer release, so reading it would apply only the keys this binary still recognises",
			VersionKey, n, SchemaVersion)
	}
	// Narrowed only past both bounds, so the counter fits whatever width int has here. Comparing after
	// the cast let a counter too wide for int wrap into the accepted range, and the file then read as a
	// version it does not hold.
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
// The document is offered to the decoder first, so no file reaches disk that a node cannot read. A
// non-nil error means the values are not on disk.
//
// The rename makes it atomic, and the temporary file sits in the destination's own directory so the
// rename stays within one filesystem. A crash at any point leaves either the previous file or the
// new one, never a truncated file a node cannot parse.
func (f *File) Save(path string) error {
	raw, err := f.Bytes()
	if err != nil {
		return err
	}
	// The one function every write to disk passes through, so the check belongs here rather than at each
	// verb that edits. A verb added later cannot forget an invariant it does not have to remember.
	if _, err := decodeBytes(raw); err != nil {
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
	syncDir(dir)
	return nil
}

// modeToWrite returns the permission a save should use, and refuses a destination it must not replace.
//
// An existing file keeps its own mode, so a save never widens what an operator narrowed. Two
// destinations are refused instead, because a rename replaces either one rather than writing through
// it. A symbolic link would leave whatever it pointed at holding the old values, with nothing about the
// result saying the link is gone. Anything else that is not a regular file is a device node, a socket
// or a pipe, and replacing one destroys it and hands the configuration whatever permission it carried,
// which for a device node is world-writable.
func modeToWrite(path string) (os.FileMode, error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return newFileMode, nil // no file there yet, which is the ordinary first save
	case err != nil:
		// Separated from the absent case, which used to absorb it. A path this process cannot inspect
		// is not a first save, and calling it one writes at the default mode on a guess.
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

// writeAndSync writes the whole payload, sets the mode, and flushes to the device.
//
// The sync is what makes the rename meaningful: without it the rename can land before the contents,
// leaving a file whose name is new and whose bytes are absent, which is the one outcome a node cannot
// boot from.
//
// This is the reason the write is by hand rather than through creachadair/atomicfile, which this module
// already depends on and which sei-tendermint's confix uses for the same job. That package renames on
// Close and never syncs, and its temporary file is unexported, so the flush cannot be added from
// outside. Fewer lines are not worth the flush here.
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

// syncDir asks the filesystem to flush dir's entries, so a rename into it survives a power loss.
//
// It reports nothing, because nothing a caller does with the answer is right. Past the rename the new
// file is what a node reads, so a flush that did not complete is not a failed save. Retrying is worse
// than doing nothing: Linux reports a writeback error once per descriptor and does not write the pages
// again, so a second flush can succeed over data that never reached the device.
func syncDir(dir string) {
	d, err := os.Open(dir) //nolint:gosec // the destination's own directory
	if err != nil {
		// A directory this process cannot open for reading is not a save that failed. The rename has
		// already happened and a reader sees the new values.
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
	// Folded to lower case above and then held to the rule Parse applies, which is not quite that rule:
	// Parse refuses an upper-case segment because a file is read lower-cased and the written name would
	// not be the one read, where a caller naming a key has no written spelling to disagree with. Held
	// here rather than at each verb, so Set cannot write a key the next Parse refuses.
	if err := keyIsAddressable(out); err != nil {
		return nil, fmt.Errorf("key %q: %w", key, err)
	}
	return out, nil
}

// quoteInt renders an integer the way TOML spells one.
func quoteInt(n int64) string { return strconv.FormatInt(n, 10) }
