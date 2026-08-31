package seitoml

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creachadair/tomledit"
)

// The shapes below arise only from a document assembled in code rather than parsed from a file, so
// they are driven from inside the package.

// TestATopLevelKeyReachesADocumentWithNoGlobalSection covers a document built rather than parsed.
//
// Parsing always produces a global section, even for a file whose first line is a table heading, so
// this shape only arises from a document assembled in code. Writing the schema version into one has to
// create the space rather than panic.
func TestATopLevelKeyReachesADocumentWithNoGlobalSection(t *testing.T) {
	f := &File{doc: &tomledit.Document{}}
	if err := f.Set(ModeKey, "seed"); err != nil {
		t.Fatalf("Set on a document with no global section: %v", err)
	}
	mode, err := f.Mode()
	if err != nil || mode != "seed" {
		t.Errorf("Mode = (%q, %v), want seed", mode, err)
	}
}

// TestAReadReusesItsDecodeAndNeverAStaleOne drives the cache itself, which only this package can see.
//
// A read renders the document and decodes it, so a caller walking every key would pay that per key. Two
// properties make the saving safe, and neither is visible from outside: a read with no edit before it
// reuses the last decode, and no edit ever leaves a decode behind that describes the document as it was.
// The second is the one that would be a correctness bug rather than a lost saving.
func TestAReadReusesItsDecodeAndNeverAStaleOne(t *testing.T) {
	newFile := func(t *testing.T) *File {
		t.Helper()
		f, err := Parse(strings.NewReader("schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nn = 1\n"))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		return f
	}

	t.Run("a second read reuses the first", func(t *testing.T) {
		f := newFile(t)
		first, err := f.decoded()
		if err != nil {
			t.Fatalf("decoded: %v", err)
		}
		// Written into the map the first read returned. A second read that decoded again would hand back
		// a map without it.
		first["probe.sentinel"] = true
		second, err := f.decoded()
		if err != nil {
			t.Fatalf("decoded: %v", err)
		}
		if _, reused := second["probe.sentinel"]; !reused {
			t.Error("a read with no edit before it decoded the document again")
		}
	})

	for _, tc := range []struct {
		name string
		edit func(*testing.T, *File)
	}{
		{"Set replacing a value", func(t *testing.T, f *File) {
			if err := f.Set("probe.n", 2); err != nil {
				t.Fatalf("Set: %v", err)
			}
		}},
		{"Set adding a key", func(t *testing.T, f *File) {
			if err := f.Set("probe.m", 3); err != nil {
				t.Fatalf("Set: %v", err)
			}
		}},
		{"Set adding a section", func(t *testing.T, f *File) {
			if err := f.Set("p2p.laddr", "x"); err != nil {
				t.Fatalf("Set: %v", err)
			}
		}},
		{"Unset", func(t *testing.T, f *File) {
			if _, err := f.Unset("probe.n"); err != nil {
				t.Fatalf("Unset: %v", err)
			}
		}},
		{"a Set the decoder refused", func(t *testing.T, f *File) {
			// Refused after the write, so the document changed and changed back. A decode of either state
			// in between describes neither.
			if err := f.Set("probe.n.deeper", 4); err == nil {
				t.Fatal("writing a table over a value was accepted")
			}
		}},
	} {
		t.Run("after "+tc.name, func(t *testing.T) {
			f := newFile(t)
			if _, err := f.decoded(); err != nil {
				t.Fatalf("decoded: %v", err)
			}
			tc.edit(t, f)

			held := f.values
			if held == nil {
				return // nothing cached, so nothing can be stale
			}
			raw, err := f.Bytes()
			if err != nil {
				t.Fatalf("Bytes: %v", err)
			}
			fresh, err := decodeBytes(raw)
			if err != nil {
				t.Fatalf("decodeBytes: %v", err)
			}
			if !reflect.DeepEqual(held, fresh) {
				t.Errorf("%s left a decode describing another document:\n held %v\nfresh %v",
					tc.name, held, fresh)
			}
		})
	}
}

// TestAFileWhoseCostOutgrowsItsSizeIsRefusedBeforeItIsRead covers the one refusal that has to happen at
// the door.
//
// Nothing downstream of reading this file can refuse a boot, which is the promise the whole surface rests
// on. A file whose cost grows faster than the bytes describing it breaks that promise from outside: it is
// not refused, it exhausts the process, and a recover cannot catch a kernel kill. So the cost is bounded
// here, before the bytes are parsed, and the bound is a refusal an operator is told about.
//
// The three shapes are the ones that grow: many segments in one heading, arrays inside arrays, and a file
// that is simply enormous. Each is written far past its bound so a change that loosens one of them fails
// rather than merely slowing down.
func TestAFileWhoseCostOutgrowsItsSizeIsRefusedBeforeItIsRead(t *testing.T) {
	const header = "schema_version = 1\nnode_mode = \"validator\"\n"
	for _, tc := range []struct {
		name string
		body string
		says string
	}{
		{
			name: "one heading of many segments",
			body: "[" + strings.Repeat("a.", maxKeyDepth+4) + "a]\nx = 1\n",
			says: "segments deep",
		},
		{
			name: "arrays inside arrays",
			body: "x = " + strings.Repeat("[", maxArrayDepth+4) + strings.Repeat("]", maxArrayDepth+4) + "\n",
			says: "nests arrays",
		},
		{
			name: "more bytes than this file is read to",
			body: strings.Repeat("# padding\n", maxFileBytes/8),
			says: "read up to",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sei.toml")
			if err := os.WriteFile(path, []byte(header+tc.body), 0o600); err != nil {
				t.Fatalf("write the probe file: %v", err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatal("the file was accepted, so its cost reaches the node rather than being refused")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the refusal says %q and has to say %q, which is what tells an operator what "+
					"about their file was refused", err, tc.says)
			}
			// The message is the one place an over-large key is certain to be rendered, so rendering it
			// whole makes the refusal as large as the file it refused.
			if len(err.Error()) > 4096 {
				t.Errorf("the refusal is %d bytes long. It reaches a log line and an operator's terminal, "+
					"so a message that grows with the file is the same problem in another place",
					len(err.Error()))
			}
		})
	}
}

// TestADeepHeadingIsRefusedBeforeTheExpensiveStep pins the ordering the bounds depend on.
//
// The depth bound is load-bearing only because keyIsAddressable runs before the decode, where the cost that
// grows faster than the file actually lives. The test above proves the refusal happens and says the right
// thing; it would still pass with the check moved after the decode, which is the arrangement the bound
// exists to prevent.
//
// So this compares two files of the same size: one deep heading, and ordinary shallow keys. Refused at the
// door, the deep one costs about what reading the bytes costs, so the two are comparable. Decoded first, it
// costs orders of magnitude more.
//
// A ratio rather than a ceiling, because the race detector and the allocator's own overhead move the
// absolute numbers and apply equally to both sides.
func TestADeepHeadingIsRefusedBeforeTheExpensiveStep(t *testing.T) {
	const header = "schema_version = 1\nnode_mode = \"validator\"\n"
	const segments = 100_000

	deep := header + "[" + strings.Repeat("a.", segments) + "a]\nx = 1\n"
	// The same bytes spent on keys nothing objects to, as the comparison.
	var shallow strings.Builder
	shallow.WriteString(header)
	for i := 0; shallow.Len() < len(deep); i++ {
		fmt.Fprintf(&shallow, "k%06d = 1\n", i)
	}

	deepCost, err := allocatedReading(t, deep)
	if err == nil {
		t.Fatal("the deep heading was accepted, so its cost reaches the node")
	}
	shallowCost, _ := allocatedReading(t, shallow.String())

	// Generous: the refusal path should be within a small factor of simply reading the bytes. Decoding
	// first put this in the thousands.
	const factor = 20
	if shallowCost > 0 && deepCost > shallowCost*factor {
		t.Errorf("refusing a %d-byte deep heading allocated %d bytes against %d for the same bytes of "+
			"ordinary keys, over %dx. The refusal is no longer happening before the step whose cost grows "+
			"with depth", len(deep), deepCost, shallowCost, factor)
	}
}

// allocatedReading reports the bytes Load allocates for a body, and what it answered.
func allocatedReading(t *testing.T, body string) (uint64, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sei.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the probe file: %v", err)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := Load(path)
	runtime.ReadMemStats(&after)
	// Cumulative rather than resident, so a collection between the two reads cannot hide the work.
	return after.TotalAlloc - before.TotalAlloc, err
}

// TestSomethingThatIsNotARegularFileIsRefused covers the shape the size bound cannot describe.
//
// The bound is a number of bytes, and only a regular file has a size that means anything. A FIFO reports
// zero, so a check against the reported size passes and the read that follows then blocks with nothing to
// stop it. That is a node that hangs on start rather than one told its file was refused, which is the
// outcome this whole const block exists to avoid.
func TestSomethingThatIsNotARegularFileIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sei.toml")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("cannot create a FIFO here: %v", err)
	}

	// Nothing ever writes to it, so a read without a bound would not return.
	done := make(chan error, 1)
	go func() {
		_, err := Load(path)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a FIFO was accepted as this node's configuration file")
		}
		if !strings.Contains(err.Error(), "regular file") {
			t.Errorf("the refusal says %q, and it has to say the path is not a regular file", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO did not return, so the bound describes a read that is not the one " +
			"happening and a node would hang on start")
	}
}

// TestASymlinkedFileIsRead covers the shape a mounted configuration file has.
//
// A Kubernetes ConfigMap volume mounts each entry as a symlink, and any layout that keeps the real file
// elsewhere and links it into the node's config directory does the same. Judging the link itself rather than
// what it points at refuses every one of those, and the file is then silently not delivered.
func TestASymlinkedFileIsRead(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "..data-sei.toml")
	if err := os.WriteFile(real, []byte("schema_version = 1\nnode_mode = \"validator\"\n"), 0o600); err != nil {
		t.Fatalf("write the real file: %v", err)
	}
	link := filepath.Join(dir, "sei.toml")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	if _, err := Load(link); err != nil {
		t.Errorf("a symlinked configuration file was refused: %v", err)
	}
}

// TestASymlinkToSomethingThatIsNotAFileIsRefused keeps the change above from reopening the hang.
//
// Following the link is what makes a mounted file readable. It must not also make a link to a FIFO
// readable, because that is the case whose open never returns.
func TestASymlinkToSomethingThatIsNotAFileIsRefused(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("cannot create a FIFO here: %v", err)
	}
	link := filepath.Join(dir, "sei.toml")
	if err := os.Symlink(fifo, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	done := make(chan error, 1)
	go func() { _, err := Load(link); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a symlink to a FIFO was accepted as this node's configuration file")
		}
		if !strings.Contains(err.Error(), "regular file") {
			t.Errorf("the refusal says %q, and it has to say the path is not a regular file", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a symlink to a FIFO did not return, so a node would hang on start")
	}
}

// TestADanglingSymlinkIsNotAnAbsentFile covers the one answer a caller acts on by staying silent.
//
// An absent file is the ordinary state and reported quietly. A link to a file that is not there is somebody
// having placed it, so answering the same for both leaves them with no signal that what they wrote does
// nothing. A ConfigMap mid-update looks exactly like this.
func TestADanglingSymlinkIsNotAnAbsentFile(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "sei.toml")
	if err := os.Symlink(filepath.Join(dir, "gone.toml"), link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	_, err := Load(link)
	if err == nil {
		t.Fatal("a link to a file that is not there was accepted")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a link to a file that is not there answers the same as no file at all (%v), and a "+
			"caller acts on that by staying quiet", err)
	}
}

// TestAnAbsentFileStillAnswersAsAbsent keeps the change above from making every missing file loud.
func TestAnAbsentFileStillAnswersAsAbsent(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "sei.toml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a node with no sei.toml answers %v, and the one caller reads that as the ordinary "+
			"state rather than a mistake", err)
	}
}
