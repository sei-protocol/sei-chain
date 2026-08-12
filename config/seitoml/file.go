package seitoml

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
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
// At the top level beside VersionKey, not inside a section, because the mode selects which baselines
// apply and so cannot itself have a per-mode baseline. It is also the only durable record of an
// archive node: seid init writes config.toml's mode as "full" for one, since Tendermint has no
// archive mode, so nothing else on disk distinguishes the two.
const ModeKey = "node_mode"

// GeneratedByKey records the release that last produced or transformed the file.
//
// Provenance, never machinery. Nothing reads it to decide anything, which is what lets it be absent
// without consequence: a binary built outside the release process carries no version, and a file
// written by one simply omits the key. Anything branching on it would turn every development build
// into a node that cannot read its own configuration.
const GeneratedByKey = "generated_by"

// newFileMode is the permission a file created here gets.
//
// Narrow rather than the usual 0644, because a configuration may name a private endpoint or an
// authentication token. An existing file keeps the mode it already has, since widening one an
// operator deliberately narrowed is worse than a default nobody wanted.
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
	return &File{doc: doc}, nil
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

// New returns an empty document carrying this binary's schema version, the given node mode, and the
// release that produced it.
//
// The mode is required rather than optional. Every value a caller goes on to write resolves for one
// mode, and a file that does not say which cannot be compared against a binary's defaults or checked
// against the mode the node actually runs.
//
// generatedBy is recorded when the caller has one and omitted when it is empty, because a binary built
// outside the release process knows no version and an empty string says less than no key at all.
func New(mode, generatedBy string) (*File, error) {
	if mode == "" {
		return nil, fmt.Errorf("a sei.toml needs a node mode: every value in it resolves for one, and " +
			"a file that omits it cannot be compared against this binary's defaults")
	}
	f := &File{doc: &tomledit.Document{Global: &tomledit.Section{}}}
	if err := f.setVersion(SchemaVersion); err != nil {
		return nil, err
	}
	if err := f.Set(ModeKey, mode); err != nil {
		return nil, err
	}
	if err := f.SetGeneratedBy(generatedBy); err != nil {
		return nil, err
	}
	return f, nil
}

// SetGeneratedBy records the release producing the file, and removes the key when given nothing.
func (f *File) SetGeneratedBy(release string) error {
	if release == "" {
		_, err := f.Unset(GeneratedByKey)
		return err
	}
	return f.Set(GeneratedByKey, release)
}

// GeneratedBy returns the release the file records, and whether it records one at all.
//
// No error for an absent key. Absence is ordinary, and a caller forced to handle it as a failure
// would be a caller whose behaviour depends on the field.
func (f *File) GeneratedBy() (string, bool) {
	e := f.doc.First(GeneratedByKey)
	if e == nil || e.KeyValue == nil {
		return "", false
	}
	v, err := goValue(e.Value)
	if err != nil {
		return "", false
	}
	release, ok := v.(string)
	return release, ok && release != ""
}

// Mode returns the node mode the file's values resolve for.
//
// An absent mode is an error rather than a guess. Guessing picks one binary's idea of a default and
// silently compares an archive node's file against a validator's baselines, which is the mistake
// this key exists to make impossible.
func (f *File) Mode() (string, error) {
	e := f.doc.First(ModeKey)
	if e == nil || e.KeyValue == nil {
		return "", fmt.Errorf("sei.toml has no %s. Every value in it resolves for one node mode, so "+
			"without it nothing can tell an archive node's file from a validator's", ModeKey)
	}
	v, err := goValue(e.Value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ModeKey, err)
	}
	mode, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s is %T (%v), want a mode name", ModeKey, v, v)
	}
	if mode == "" {
		return "", fmt.Errorf("%s is empty", ModeKey)
	}
	return mode, nil
}

// Version returns the schema version the file records.
//
// An absent or unparsable version is an error, never a zero. A migration chain reads this to decide
// which steps to run, so guessing here transforms a file whose shape nobody established.
func (f *File) Version() (int, error) {
	e := f.doc.First(VersionKey)
	if e == nil || e.KeyValue == nil {
		return 0, fmt.Errorf("sei.toml has no %s. Its shape cannot be established, so no migration "+
			"can safely run against it and no reader can know which keys it is expected to carry",
			VersionKey)
	}
	v, err := goValue(e.Value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", VersionKey, err)
	}
	n, ok := v.(int64)
	if !ok {
		return 0, fmt.Errorf("%s is %T (%v), want an integer", VersionKey, v, v)
	}
	return int(n), nil
}

// setVersion writes the schema version at the document's top level.
func (f *File) setVersion(n int) error {
	return f.Set(VersionKey, n)
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

	mode := newFileMode
	if info, err := os.Stat(path); err == nil {
		// An existing file keeps its own mode, so a save never widens what an operator narrowed.
		mode = info.Mode().Perm()
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
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	return syncDir(dir)
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
