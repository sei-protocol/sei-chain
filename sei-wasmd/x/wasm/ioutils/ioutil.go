package ioutils

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/ioutil"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

// Uncompress returns gzip uncompressed content if input was gzip, or original src otherwise
func Uncompress(src []byte, limit uint64) ([]byte, error) {
	switch n := uint64(len(src)); {
	case n < 3:
		return src, nil
	case n > limit:
		return nil, types.ErrLimit
	}
	if !bytes.Equal(gzipIdent, src[0:3]) {
		return src, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	zr.Multistream(false)
	defer zr.Close()
	return ioutil.ReadAll(LimitReader(zr, int64(limit)))
}

// LimitReader returns a Reader that reads from r
// but stops with types.ErrLimit after n bytes.
// The underlying implementation is a *io.LimitedReader.
func LimitReader(r io.Reader, n int64) io.Reader {
	return &LimitedReader{r: &io.LimitedReader{R: r, N: n}}
}

type LimitedReader struct {
	r *io.LimitedReader
}

func (l *LimitedReader) Read(p []byte) (n int, err error) {
	if l.r.N <= 0 {
		return 0, types.ErrLimit
	}
	return l.r.Read(p)
}
