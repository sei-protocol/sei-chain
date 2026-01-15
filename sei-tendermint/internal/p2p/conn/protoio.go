package conn 

import (
	"errors"
	"context"
	"encoding/binary"
	"github.com/gogo/protobuf/proto"
)

// Writes size-prefixed proto message.
func WriteSizedProto(ctx context.Context, conn Conn, msg proto.Message) error {
	data, err := proto.Marshal(msg)
	if err != nil { return err }
	var sizeVar [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(sizeVar[:],uint64(len(data)))
	if err := conn.Write(ctx,sizeVar[:n]); err!=nil { return err }
	if err := conn.Write(ctx,data); err!=nil { return err }
	return nil
}

// Reads size-prefixed proto message.
// It is slow, because size is encoded as varint, and therefore needs to be read byte at a time.
func ReadSizedProto(ctx context.Context, conn Conn, msg proto.Message, maxSize uint64) error {
	var sizeVar [binary.MaxVarintLen64]byte
	for i := range sizeVar {
		if err:=conn.Read(ctx,sizeVar[i:i+1]); err!=nil { return err }
		if sizeVar[i] < 0x80 { break }
	}
	size, n := binary.Uvarint(sizeVar[:])
	if n<=0 { return errors.New("invalid size") }
	if size>maxSize { return errors.New("message too large") }
	msgRaw := make([]byte, size)
	if err:=conn.Read(ctx,msgRaw); err!=nil { return err }
	return proto.Unmarshal(msgRaw, msg)
}

// Unmarshals length prefixed message.
// Length is encoded as varint.
func UnmarshalSizedProto(data []byte, msg proto.Message) error {
	size,n := binary.Uvarint(data)
	if n<=0 || uint64(len(data))<uint64(n)+size { return errors.New("invalid size") }
	return proto.Unmarshal(data[n:n+int(size)],msg)
}
