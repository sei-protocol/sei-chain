package statesync

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// chunkReadBufferSize is the read buffer of a row chunk decoder.
const chunkReadBufferSize = 1 << 20

func recordEncodedLen(keyLen, valueLen int) int {
	return uvarintLen(uint64(keyLen)) + keyLen + uvarintLen(uint64(valueLen)) + valueLen //nolint:gosec // lengths come from byte slices.
}

func uvarintLen(v uint64) int {
	var buf [10]byte
	return binary.PutUvarint(buf[:], v)
}

func (w *rowChunkWriter) appendUvarint(v uint64) {
	var buf [10]byte
	n := binary.PutUvarint(buf[:], v)
	w.chunk = append(w.chunk, buf[:n]...)
}

// chunkReader reads the row records of one row chunk from the archive stream.
type chunkReader struct {
	r *bufio.Reader
	// remaining is the number of chunk bytes not yet read.
	remaining int64
}

func (c *chunkReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err != nil {
		return 0, err
	}
	c.remaining--
	return b, nil
}

func (c *chunkReader) readRecord() ([]byte, []byte, error) {
	key, err := c.readField("key", maxStateKeySize)
	if err != nil {
		return nil, nil, err
	}
	value, err := c.readField("value", maxStateValueSize)
	if err != nil {
		return nil, nil, err
	}
	return key, value, nil
}

// readField reads one length-prefixed field, checking its length before it allocates the field.
func (c *chunkReader) readField(name string, max int) ([]byte, error) {
	before := c.remaining
	n, err := binary.ReadUvarint(c)
	if err != nil {
		return nil, fmt.Errorf("read %s length: %w", name, err)
	}
	if read := before - c.remaining; read != int64(uvarintLen(n)) {
		return nil, fmt.Errorf("%s length %d is encoded in %d bytes, not the minimal %d", name, n, read, uvarintLen(n))
	}
	if n == 0 {
		return nil, fmt.Errorf("%s length is 0", name)
	}
	if n > uint64(max) { //nolint:gosec // max is a positive archive limit constant.
		return nil, fmt.Errorf("%s length %d is more than %d", name, n, max)
	}
	if n > uint64(c.remaining) { //nolint:gosec // remaining is not negative.
		return nil, fmt.Errorf("%s length %d exceeds the remaining chunk size %d", name, n, c.remaining)
	}
	field := make([]byte, n)
	if _, err := io.ReadFull(c.r, field); err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	c.remaining -= int64(n) //nolint:gosec // n is bounded by maxStateKeySize or maxStateValueSize.
	return field, nil
}
