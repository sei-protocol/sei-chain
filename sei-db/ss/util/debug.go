package util

import (
	"encoding/binary"
	"io"
)

// Writes raw bytes to file
func writeByteSlice(w io.Writer, data []byte) error {
	length := uint32(len(data))
	if err := binary.Write(w, binary.LittleEndian, length); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}
