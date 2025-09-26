package ioutils

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

func TestUncompress(t *testing.T) {
	wasmRaw, err := ioutil.ReadFile("../keeper/testdata/hackatom.wasm")
	require.NoError(t, err)

	wasmGzipped, err := ioutil.ReadFile("../keeper/testdata/hackatom.wasm.gzip")
	require.NoError(t, err)

	const maxSize = 400_000

	specs := map[string]struct {
		src       []byte
		expError  error
		expResult []byte
	}{
		"handle wasm uncompressed": {
			src:       wasmRaw,
			expResult: wasmRaw,
		},
		"handle wasm compressed": {
			src:       wasmGzipped,
			expResult: wasmRaw,
		},
		"handle nil slice": {
			src:       nil,
			expResult: nil,
		},
		"handle short unidentified": {
			src:       []byte{0x1, 0x2},
			expResult: []byte{0x1, 0x2},
		},
		"handle input slice exceeding limit": {
			src:      []byte(strings.Repeat("a", maxSize+1)),
			expError: types.ErrLimit,
		},
		"handle input slice at limit": {
			src:       []byte(strings.Repeat("a", maxSize)),
			expResult: []byte(strings.Repeat("a", maxSize)),
		},
		"handle gzip identifier only": {
			src:      gzipIdent,
			expError: io.ErrUnexpectedEOF,
		},
		"handle broken gzip": {
			src:      append(gzipIdent, byte(0x1)),
			expError: io.ErrUnexpectedEOF,
		},
		"handle incomplete gzip": {
			src:      wasmGzipped[:len(wasmGzipped)-5],
			expError: io.ErrUnexpectedEOF,
		},
		"handle limit gzip output": {
			src:       asGzip(bytes.Repeat([]byte{0x1}, maxSize)),
			expResult: bytes.Repeat([]byte{0x1}, maxSize),
		},
		"handle big gzip output": {
			src:      asGzip(bytes.Repeat([]byte{0x1}, maxSize+1)),
			expError: types.ErrLimit,
		},
		"handle other big gzip output": {
			src:      asGzip(bytes.Repeat([]byte{0x1}, 2*maxSize)),
			expError: types.ErrLimit,
		},
	}
	for msg, spec := range specs {
		t.Run(msg, func(t *testing.T) {
			r, err := Uncompress(spec.src, maxSize)
			require.True(t, errors.Is(spec.expError, err), "exp %v got %+v", spec.expError, err)
			if spec.expError != nil {
				return
			}
			assert.Equal(t, spec.expResult, r)
		})
	}
}

func asGzip(src []byte) []byte {
	var buf bytes.Buffer
	zipper := gzip.NewWriter(&buf)
	if _, err := io.Copy(zipper, bytes.NewReader(src)); err != nil {
		panic(err)
	}
	if err := zipper.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
