package compression

import (
	"bytes"

	"github.com/golang/snappy"
)

func compressSnappy(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := snappy.NewBufferedWriter(&buf)
	_, err := writer.Write(b)
	if err != nil {
		return nil, err
	}
	writer.Close()

	return buf.Bytes(), nil
}

func decompressSnappy(b []byte) ([]byte, error) {
	r := snappy.NewReader(bytes.NewReader(b))

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
