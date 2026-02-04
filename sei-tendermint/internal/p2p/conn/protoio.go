package conn

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
)

var errMsgTooLarge = errors.New("message too large")

// Writes size-prefixed proto message.
func WriteSizedMsg(ctx context.Context, conn Conn, msg []byte) error {
	var sizeVar [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(sizeVar[:], uint64(len(msg)))
	if err := conn.Write(ctx, sizeVar[:n]); err != nil {
		return err
	}
	if err := conn.Write(ctx, msg); err != nil {
		return err
	}
	return nil
}

// Reads size-prefixed proto message.
// It is slow, because size is encoded as varint, and therefore needs to be read byte at a time.
func ReadSizedMsg(ctx context.Context, conn Conn, maxSize uint64) ([]byte, error) {
	var sizeVar [binary.MaxVarintLen64]byte
	for i := range sizeVar {
		if err := conn.Read(ctx, sizeVar[i:i+1]); err != nil {
			return nil, err
		}
		if sizeVar[i] < 0x80 {
			break
		}
	}
	size, n := binary.Uvarint(sizeVar[:])
	if n <= 0 {
		return nil, errors.New("invalid size")
	}
	if size > maxSize {
		return nil, fmt.Errorf("%w: got %v, want <=%v", errMsgTooLarge, size, maxSize)
	}
	msg := make([]byte, size)
	if err := conn.Read(ctx, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// Unmarshals length prefixed message.
// Length is encoded as varint.
func UnmarshalSizedProto(data []byte, msg proto.Message) error {
	size, n := binary.Uvarint(data)
	if n <= 0 || uint64(len(data)) < uint64(n)+size {
		return errors.New("invalid size")
	}
	return proto.Unmarshal(data[n:n+int(size)], msg)
}
