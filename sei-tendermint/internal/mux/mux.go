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
	Kinds map[StreamKind]*StreamKindConfig
}

type StreamKindConfig struct {
	MaxConnects uint64
	MaxAccepts uint64
}

type canUpdate[T comparable] struct {
	value T
	newValue T
}

func (v *canUpdate[T]) Pop() utils.Option[T] {
	if v.value==v.newValue {
		return utils.None[T]()
	}
	v.value = v.newValue
	return utils.Some(v.value)
}

type StreamState struct {
	id StreamID
	kind StreamKind

	sendMaxMsgSize uint64
	sendBegin uint64
	sendEnd uint64
	sendMsg utils.Option[[]byte]
	sendClosed canUpdate[bool]
	
	recvMaxMsgSize canUpdate[uint64]
	recvBegin uint64
	recvUsed uint64
	recvEnd canUpdate[uint64]
	recvMsgs [][]byte
	recvClosed bool
}

type Frame struct {
	Header
	Payload []byte
}

func (s *StreamState) PopFrame(maxPayload uint64) utils.Option[*Frame] {
	f := Frame{
		Header{
			RecvMaxMsgSize: s.recvMaxMsgSize.Pop(),
			RecvWindowEnd: s.recvEnd.Pop(),
		},
		nil,
	}
	// Sending message payload.
	if sendMsg,ok := s.sendMsg.Get(); ok {
		if uint64(len(sendMsg))>maxPayload {
			f.SendPayloadSize = utils.Some(uint64(len(sendMsg)))
			f.Payload = sendMsg
			f.SendMsgEnd = utils.Some(true)
			s.sendMsg = utils.None[[]byte]()
		} else {
			f.SendPayloadSize = utils.Some(maxPayload)
			f.Payload = sendMsg[:maxPayload]
			s.sendMsg = utils.Some(sendMsg[maxPayload:])
		}
	}
	if !s.sendMsg.IsPresent() {
		f.SendClosed = s.sendClosed.Pop()	
	}
	if f.Header!=(Header{}) {
		f.StreamID = s.id
		return utils.Some(&f)
	}
	return utils.None[*Frame]()
}

type Stream struct {
	state *utils.Watch[*StreamState]
	mux *Mux
}

func (s *Stream) Send(ctx context.Context, msg []byte) error {
	for state,ctrl := range s.state.Lock() {
		if uint64(len(msg))>state.sendMaxMsgSize {
			return fmt.Errorf("message too large")
		}
		if state.sendClosed.value { // not opened yet.
			return fmt.Errorf("closed")
		}
		if err:=ctrl.WaitUntil(ctx, func() bool {
			return state.sendClosed.newValue || (state.sendBegin<state.sendEnd && !state.sendMsg.IsPresent())
		}); err!=nil {
			return err
		}
		if state.sendClosed.newValue { // closed while flushing
			return fmt.Errorf("closed")
		}
		state.sendMsg = utils.Some(msg)
		state.sendBegin += 1
	}
	s.mux.dirty(s)
	return nil
}

func (s *Stream) SendClose() {
	for state := range s.state.Lock() {
		state.sendClosed.newValue = true
	}
}

func (s *Stream) Recv(ctx context.Context, freeBuffer bool) ([]byte,error) {
	for state,ctrl := range s.state.Lock() {
		if err:=ctrl.WaitUntil(ctx, func() bool {
			return state.recvClosed || state.recvBegin<state.recvUsed
		}); err!=nil {
			return nil,err
		}
		if state.recvBegin==state.recvUsed {
			return nil,fmt.Errorf("closed")
		}
		i := state.recvBegin%uint64(len(state.recvMsgs))
		msg := state.recvMsgs[i]
		state.recvMsgs[i] = nil
		state.recvBegin += 1
		if freeBuffer {
			state.recvEnd.newValue = state.recvBegin + uint64(len(state.recvMsgs))
			s.mux.dirty(s.stream)
		}
		return msg,nil
	}
	panic("unreachable")
}

/*
func (s *RecvState) CheckPayloadSize(payloadSize uint64) error {
	if s.windowUsed>=s.windowEnd {
		return fmt.Errorf("recv buffer overflow")
	}
	i := (s.windowUsed+1)%uint64(len(s.window))
	if s.maxMsgSize-uint64(len(s.window[i])) < payloadSize {
		return fmt.Errorf("recv buffer overflow")	
	}
	return nil
}

func (s *RecvState) PushPayload(payload []byte) {
	i := (s.windowUsed+1)%uint64(len(s.window))
	s.window[i] = append(s.window[i],payload...)
}

func (s *RecvState) PushMsgEnd() { s.windowUsed += 1 }
func (s *RecvState) PushResize(windowEnd uint64) { s.windowEnd = max(s.windowEnd,windowEnd) }
*/


type KindState struct {
	connectable chan *StreamState 
	acceptable chan *StreamState
}

type Mux struct {
	kinds map[StreamKind]*KindState
	dirty utils.Watch[[]StreamID]
}

func (m *Mux) NewMux(cfg *Config) *Mux {

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
					// Mark stream as remotely closed, clear the slot if needed.
				default: panic(fmt.Errorf("unknown frame type %T",frame))
			}
		}
	})
}

func (m *Mux) Connect(ctx context.Context, kind StreamKind, maxMsgSize uint64, window uint64) (StreamID,error) {
	s,ok := m.kinds[kind]
	if !ok { return 0,fmt.Errorf("kind %v not available",kind) }
	id,err := utils.Recv(ctx,s.connectable)
	if err!=nil { return 0,err }
	m.send(&FrameOpen{id,kind,maxMsgSize,window})
	// Wait until OPEN is received
	// if context canceled, send FrameClose{}
	// return stream
	return id,nil
}

func (m *Mux) Accept(ctx context.Context, kind StreamKind, maxMsgSize uint64, window uint64) (StreamID,error) {
	s,ok := m.kinds[kind]
	if !ok { return 0,fmt.Errorf("kind %v not available",kind) }
	id,err := utils.Recv(ctx,s.acceptable)
	if err!=nil { return 0,err }
	m.send(&Open{id,kind,maxMsgSize,window})	
	return id,nil
}

// To make the peer respect msg response size limit, encode the limit within the request.
func (m *Mux) Send(ctx context.Context, id StreamID, msg []byte) error {
	// TODO: access under lock.
	s,ok := m.streams[id]
	if !ok { return fmt.Errorf("stream %v not available",id) }
	if s.sendMaxMsgSize < uint64(len(msg)) {
		return fmt.Errorf("msg too large")
	}
	for sendMsg,ctrl := range s.sendMsg.Lock() {
		if err:=ctrl.WaitUntil(ctx,func() bool {
			return !sendMsg.IsPresent()
		}); err!=nil {
			return err
		}
		*sendMsg = utils.Some(msg)
	}
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
