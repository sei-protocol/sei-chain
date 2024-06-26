package compression

import (
	"bytes"

	"compress/zlib"
)

func compressZLib(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	_, err := writer.Write(b)
	if err != nil {
		return nil, err
	}
	writer.Close()

	return buf.Bytes(), nil
}

func decompressZLib(b []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
