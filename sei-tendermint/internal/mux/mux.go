package mux

import (
	"io"
	"fmt"
	"context"
	"net"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/internal/mux/pb"
	"google.golang.org/protobuf/proto"
)

type StreamID uint64
type StreamKind uint64

type Config struct {
	FrameSize uint64
}

// State of the stream. 
type StreamState struct {
	// Output buffer is a single message (or less?)
	sendBegin uint64
	sendEnd uint64
	sendMsg utils.Option[[]byte]

	// Cyclic buffer
	recvBegin uint64
	recvUsed uint64
	recvEnd uint64
	recvMsgs [][]byte 
	// begin <= used <= end <= begin+len(recv)
}

type KindState struct {
	connected int 
	accepted int 
	acceptable chan StreamID
}

type Mux struct {
	cfg *Config
	nextStreamID StreamID
	streams map[StreamID]*StreamState
	kinds map[StreamKind]*KindState
	dirty []StreamID
}

func (m *Mux) Run(ctx context.Context, conn net.Conn) error {
	// TODO: handshake exchange.

	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			// Sending task.
			for {
				// Wait for some stream to have data to send.
			}
			return nil
		})
		// Receiving task.
		for {
			// Frame size is hard capped here at 255B.
			// Any real frame will be capped to 44B (4 varints)
			// An average frame will be ~10B
			var frameSize [1]byte
			if _,err := conn.Read(frameSize[:]); err!=nil { return err }
			frameRaw := make([]byte, frameSize[0])
			if _,err:=io.ReadFull(conn,frameRaw[:]); err!=nil { return err }
			var frameProto pb.Frame
			if err:=proto.Unmarshal(frameRaw,&frameProto); err!=nil { return err }
			switch frame := DecodeFrame(&frameProto).(type) {
				case *FrameOpen:
					// check if there is slot for a new stream and that the stream is new
				case *FrameResize:
					// move the WindowEnd
				case *FrameMsg:
					// check if there is place for these bytes.
					// io.ReadFull(conn,buf[0:frame.PayloadSize])
					// end the message and notify if needed.
				case *FrameClose:
				default: panic(fmt.Errorf("unknown frame type %T",frame))
			}
		}
	})
}

func (m *Mux) Connect(ctx context.Context, kind StreamKind, window uint64) (StreamID,error) {
	// Wait until stream slot is available
	// Send OPEN
	// Wait until OPEN is received
	// return stream
}

func (m *Mux) Accept(ctx context.Context, kind StreamKind, window uint64) (StreamID,error) {
	// Wait until OPEN is received
	// Send OPEN
	// return stream
}

// To make the peer respect msg response size limit, encode the limit within the request.
func (m *Mux) Send(ctx context.Context, id StreamID, msg []byte) error {
	// Find the stream state
	// Push data into send buffer 
	return nil
}

func (m *Mux) Recv(ctx context.Context, freeBuf bool) ([]byte,error) {
	// Find the stream state
	// Fetch len(msg) from the recv buffer
	// Check against the limit.
	// Fetch msg from the recv buffer.
}

// Marks stream for closure. All data sent so far will be delivered.
// Send and Recv will fail afterward.
// Slot will be freed after CLOSE is actually sent and received.
func (m *Mux) Close(id StreamID) {

}
