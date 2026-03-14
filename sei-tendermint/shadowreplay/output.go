package shadowreplay

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const blocksPerFile = 10_000

// OutputWriter manages NDJSON output to rotating gzipped files and optional
// per-divergence files.
type OutputWriter struct {
	dir     string
	stdout  io.Writer
	curFile *os.File
	curGzip *gzip.Writer
	curEnc  *json.Encoder
	curBase int64
}

// NewOutputWriter creates an output writer. If dir is empty, output goes only
// to stdout. If stdout is nil, os.Stdout is used.
func NewOutputWriter(dir string, stdout io.Writer) (*OutputWriter, error) {
	if stdout == nil {
		stdout = os.Stdout
	}
	w := &OutputWriter{dir: dir, stdout: stdout}
	if dir != "" {
		for _, sub := range []string{"blocks", "divergences"} {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
				return nil, fmt.Errorf("creating output dir %s: %w", sub, err)
			}
		}
	}
	return w, nil
}

// WriteBlock writes a BlockComparison record to the output stream(s).
func (w *OutputWriter) WriteBlock(comp *BlockComparison) error {
	if w.dir != "" {
		if err := w.writeToFile(comp); err != nil {
			return err
		}

		if len(comp.Divergences) > 0 {
			if err := w.writeDivergence(comp); err != nil {
				return err
			}
		}
	}

	return json.NewEncoder(w.stdout).Encode(comp)
}

func (w *OutputWriter) writeToFile(comp *BlockComparison) error {
	windowBase := (comp.Height / blocksPerFile) * blocksPerFile

	if w.curFile == nil || windowBase != w.curBase {
		if err := w.rotateFile(); err != nil {
			return err
		}

		name := fmt.Sprintf("%d-%d.ndjson.gz", windowBase, windowBase+blocksPerFile)
		path := filepath.Join(w.dir, "blocks", name)

		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("opening block file %s: %w", name, err)
		}

		w.curFile = f
		w.curGzip = gzip.NewWriter(f)
		w.curEnc = json.NewEncoder(w.curGzip)
		w.curBase = windowBase
	}

	return w.curEnc.Encode(comp)
}

func (w *OutputWriter) writeDivergence(comp *BlockComparison) error {
	name := fmt.Sprintf("%d.json", comp.Height)
	path := filepath.Join(w.dir, "divergences", name)

	data, err := json.MarshalIndent(comp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling divergence at %d: %w", comp.Height, err)
	}

	return os.WriteFile(path, data, 0o644)
}

func (w *OutputWriter) rotateFile() error {
	if w.curGzip != nil {
		if err := w.curGzip.Close(); err != nil {
			return err
		}
	}
	if w.curFile != nil {
		if err := w.curFile.Close(); err != nil {
			return err
		}
	}
	w.curFile = nil
	w.curGzip = nil
	w.curEnc = nil
	return nil
}

// Close flushes and closes any open file handles.
func (w *OutputWriter) Close() error {
	return w.rotateFile()
}
