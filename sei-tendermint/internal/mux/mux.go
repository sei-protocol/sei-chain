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
	FramePayload uint64
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

type SendState struct {
	opened bool // StreamState.RemoteOpen()
	maxMsgSize uint64
	bufBegin uint64
	begin uint64
	end uint64
	closed bool // Stream.Close()
}

type RecvState struct {
	opened bool // Stream.open()
	maxMsgSize uint64
	begin uint64
	used uint64
	end uint64
	msgs [][]byte
	closed bool // StreamState.RemoteClose()
}

type StreamState struct {
	id StreamID
	kind StreamKind
	send utils.Watch[*SendState]
	recv utils.Watch[*RecvState]
}

func newStreamState(id StreamID, kind StreamKind) *StreamState {
	return &StreamState {id: id, kind: kind, recv: utils.NewWatch(&RecvState{})}
}

func (s *StreamState) terminated() bool {
	for send := range s.send.Lock() {
		if !send.opened && !send.closed {
			return false
		}
	}
	for recv := range s.recv.Lock() {
		if !recv.opened && !recv.closed {
			return false
		}
	}
	return true
}

func (s *StreamState) RemoteOpen(maxMsgSize uint64) error {
	for send,ctrl := range s.send.Lock() {
		if send.opened {
			return fmt.Errorf("already opened")
		}
		send.opened = true
		send.maxMsgSize = maxMsgSize
		ctrl.Updated()
	}
	return nil
}

func (s *StreamState) RemoteClose() error {
	for recv,ctrl := range s.recv.Lock() {
		if !recv.opened || recv.closed {
			return fmt.Errorf("already closed")
		}
		recv.closed = true
		ctrl.Updated()
	}
	return nil
}

type Frame struct {
	Header
	Payload []byte
}

type Stream struct {
	state *StreamState
	queue *Queue
}

func (s *Stream) open(ctx context.Context, maxMsgSize uint64, window uint64) error {
	for recv := range s.state.recv.Lock() {
		if recv.opened {
			return fmt.Errorf("already opened")
		}
		recv.opened = true
		recv.maxMsgSize = maxMsgSize
		recv.end = window
		recv.msgs = make([][]byte,window)
		for queue,ctrl := range s.queue.Lock() {
			f,ok := queue[s.state.id]
			if !ok {
				if len(queue)==0 {
					ctrl.Updated()
				}
				f = &Frame{Header:Header{StreamID: s.state.id}}
				queue[s.state.id] = f
			}
			f.Kind = s.state.kind
			f.RecvMaxMsgSize = maxMsgSize
			f.RecvWindowEnd = window
		}
	}
	for send,ctrl := range s.state.send.Lock() {
		if err:=ctrl.WaitUntil(ctx, func() bool { return send.opened }); err!=nil {
			// WARNING: note that here we close, before the stream was opened
			// Connect got cancelled and the stream is orphaned.
			s.Close()
			return err
		}
	}
	return nil
}

func (s *Stream) Send(ctx context.Context, msg []byte) error {
	for send,ctrl := range s.state.send.Lock() {	
		// Wait until the local buffer is empty && remote buffer has capacity.
		if err:=ctrl.WaitUntil(ctx, func() bool { return send.closed || (send.bufBegin == send.begin && send.begin < send.end) }); err!=nil {
			return err
		}
		if send.closed {
			return fmt.Errorf("stream closed")
		}
		if uint64(len(msg))>send.maxMsgSize {
			return fmt.Errorf("message too large")
		}
		send.begin += 1
		// Push msg to the queue.
		for queue,ctrl := range s.queue.Lock() {
			f,ok := queue[s.state.id]
			if !ok {
				if len(queue)==0 {
					ctrl.Updated()
				}
				f = &Frame{Header:Header{StreamID: id}}
				queue[s.state.id] = f
			} 
			f.PayloadSize = uint64(len(msg))
			f.Payload = msg
			f.MsgEnd = true
		}
	}
	return nil
}

func (s *Stream) Close() {
	for send := range s.state.send.Lock() {
		if send.closed {
			return
		}
		send.closed = true
		for queue,ctrl := range s.queue.Lock() {
			f,ok := queue[s.state.id]
			if !ok {
				if len(queue)==0 { ctrl.Updated() }
				f = &Frame{Header:Header{StreamID: s.state.id}}
				queue[s.state.id] = f
			}
			f.SendClosed = true
		}
	}
}

func (s *Stream) Recv(ctx context.Context, freeBuffer bool) ([]byte,error) {
	for recv,ctrl := range s.state.recv.Lock() {
		if err:=ctrl.WaitUntil(ctx, func() bool {
			return recv.closed || recv.begin<recv.used
		}); err!=nil {
			return nil,err
		}
		if recv.begin==recv.used {
			return nil,fmt.Errorf("closed")
		}
		i := recv.begin%uint64(len(recv.msgs))
		msg := recv.msgs[i]
		recv.msgs[i] = nil
		recv.begin += 1
		if freeBuffer {
			recv.end = recv.begin + uint64(len(recv.msgs))
			for queue,ctrl := range s.queue.Lock() {
				f,ok := queue[s.state.id]
				if !ok {
					if len(queue)==0 { ctrl.Updated() }
					f = &Frame{Header:Header{StreamID:s.state.id}}
					queue[s.state.id] = f
				}
				f.RecvWindowEnd = recv.end
			}
		}
		return msg,nil
	}
	panic("unreachable")
}

type KindState struct {
	connectsQueue chan *StreamState
	acceptsQueue chan *StreamState
}

type runnerInner struct {
	nextID StreamID
	streams map[StreamID]*StreamState
	acceptsSem map[StreamKind]int
}

type runner struct {
	mux *Mux
	inner utils.RWMutex[*runnerInner]	
}

func newRunner(mux *Mux) *runner {
	return &runner {
		mux: mux,
		inner: utils.NewRWMutex(&runnerInner {
			nextID: 0,
			streams: map[StreamID]*StreamState{},
			acceptsSem: map[StreamKind]int{},
		}),
	}
}

func (r *runner) get(id StreamID) (*StreamState,bool) {
	for inner := range r.inner.RLock() {
		s,ok := inner.streams[id]
		return s,ok
	}
	panic("unreachable")
}

func (r *runner) getOrAccept(id StreamID, kind StreamKind) (*StreamState,error) {
	s,ok := r.get(id)
	if ok { return s,nil }
	for inner := range r.inner.Lock() {
		if id.isConnect() {
			return nil,fmt.Errorf("peer tried to open stream with bad id")
		}
		if inner.acceptsSem[kind]==0 {
			return nil,fmt.Errorf("too many concurrent accept streams")
		}
		inner.acceptsSem[kind] -= 1
		s = newStreamState(id,kind)
		inner.streams[id] = s 
		return s,nil
	}
	panic("unreachable")
}

func (r *runner) tryPrune(id StreamID) {
	for inner := range r.inner.Lock() {
		s,ok := inner.streams[id]
		if !ok || !s.terminated() { return }
		delete(inner.streams,id)
		if id.isConnect() {
			// Non-blocking since we just closed a connect stream.
			r.mux.kinds[s.kind].connectsQueue <- newStreamState(inner.nextID,s.kind)
			inner.nextID += 2
		} else {
			inner.acceptsSem[s.kind] += 1
		}
	}
}

// runSend handles the frame queue.
// The frames from all streams are interleaved in a round robin fashion.
// Frames have bounded size to make sure that large messages do not slow down smaller ones.
// Stream priorities are not implemented (not needed).
func (r *runner) runSend(ctx context.Context, conn net.Conn) error {
	for {
		// Collect frames in round robin over streams.
		var frames []*Frame
		flush := false
		for queue,ctrl := range r.mux.queue.Lock() {
			if err := ctrl.WaitUntil(ctx,func() bool { return len(queue)>0 }); err!=nil {
				return err
			}
			for id,f := range queue {
				frames = append(frames,f.Pop())
				if f.Empty() {
					delete(queue,id)
				}
			}
			flush = len(queue)==0
		}
		// Send the frames
		for _,f := range frames {
			if f.SendMsgEnd {
				// Notify sender about local buffer capacity.
				for inner := range r.inner.RLock() {
					for send,ctrl := range inner.streams[f.StreamId].send.Lock() {
						send.bufBegin += 1
						ctrl.Updated()
					}
				}
			}
			if f.SendClosed {
				r.tryPrune(f.StreamID)	
			}
			headerRaw := proto.Marshal(EncodeHeader(f.Header))
			if _,err:=conn.Write([]byte{len(frameRaw)}); err!=nil { return err }
			if _,err:=conn.Write(headerRaw); err!=nil { return err }
			if _,err:=conn.Write(f.Payload); err!=nil { return err }
		}
		if flush {
			if err:=conn.Flush(); err!=nil { return err }
		}
	}
}

func (r *runner) runRecv(conn net.Conn) error {
	for {
		// Frame size is hard capped here at 255B.
		// Currently we have 7 varint fields (up to 77B)
		var frameSize [1]byte
		if _,err := conn.Read(frameSize[:]); err!=nil { return err }
		frameRaw := make([]byte, frameSize[0])
		if _,err:=io.ReadFull(conn,frameRaw[:]); err!=nil { return err }
		var frameProto pb.Frame
		if err:=proto.Unmarshal(frameRaw,&frameProto); err!=nil { return err }
		frame := DecodeFrame(&frameProto)
		frame.StreamID = frame.StreamID.RemoteToLocal()
		s,err := r.getOrAccept(frame.StreamID,frame.GetStreamKind())	
		if frame.RecvMaxMsgSize!=nil {
			if err:=s.RemoteOpen(frame.GetRecvMaxMsgSize()); err!=nil {
				return err
			}
			if s.id.IsConnect() {
				r.tryPrune(s.id) // tryPrune the stream in case it was orphaned.
			} else {
				r.mux.kinds[frame.StreamKind].acceptsQueue <- s
			}
		}
		if f.RecvWindowEnd!=nil {
			for send,ctrl := range s.send.Lock() {
				if send.End<*f.RecvWindowEnd {
					send.End = *f.RecvWindowEnd
					ctrl.Updated()
				}
			}
		}
		if f.PayloadSize!=nil {
			// check if there is place for the payload.
			for recv := range s.recv.Lock() {
				if recv.closed || !recv.opened { return fmt.Errorf("closed") }
				if recv.used==recv.end {
					return fmt.Errorf("buffer full")
				}
				i := int(recv.used)%len(recv.msgs)
				if recv.maxMsgSize-len(recv.msgs[i]) < f.PayloadSize {
					return fmt.Errorf("msg too large")
				}
			}
			// Read the payload.
			payload := make([]byte,*f.PayloadSize)
			if _,err:=io.ReadFull(conn,payload[:]); err!=nil { return err }
			for recv := range s.recv.Lock() {
				i := int(recv.used)%len(recv.msgs)
				recv.msgs[i] = append(recv.msgs[i],payload...)
			}
		}
		if f.MsgEnd {
			for recv,ctrl := range s.recv.Lock() {
				if recv.closed || !recv.opened { return fmt.Errorf("closed") }
				if recv.used==recv.end {
					return fmt.Errorf("buffer full")
				}
				recv.used += 1
				ctrl.Updated()
			}
		}
		if f.SendClosed {
			for recv,ctrl := range s.recv.Lock() {
				if err:=recv.RemoteClose(); err!=nil {
					return err
				}
				for ss := range ss.Lock() {
					r.tryPrune(s.id)	
				}
				ctrl.Updated()
			}
		}
	}
}

func (m *Mux) Run(ctx context.Context, conn net.Conn) error {
	// TODO: handshake exchange.
	r := newRunner(m)
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return r.runSend(ctx,conn) })
		s.Spawn(func() error { return r.runRecv(ctx,conn) })	
		return nil
	})
}

type Queue = utils.Watch[map[StreamID]*Frame]

type Mux struct {
	cfg *Config
	kinds map[StreamKind]*KindState
	queue *Queue 
}

func (m *Mux) NewMux(cfg *Config) *Mux {
	kinds := map[StreamKind]*KindState{}
	for kind,c := range cfg.Kinds {
		kinds[kind] = &KindState {
			acceptsQueue: make(chan *StreamState, c.MaxAccepts),
			connectsQueue: make(chan *StreamState, c.MaxConnects),
		}
	}
	queue := utils.NewWatch(map[StreamID]*Frame{})
	return &Mux {cfg: cfg, kinds: kinds, queue: &queue}
}

func (m *Mux) Connect(ctx context.Context, kind StreamKind, maxMsgSize uint64, window uint64) (*Stream,error) {
	ks,ok := m.kinds[kind]
	if !ok { return nil,fmt.Errorf("kind %v not available",kind) }
	state,err := utils.Recv(ctx,ks.connectsQueue)
	if err!=nil { return nil,err }
	s := &Stream{state,m.queue}
	if err := s.open(ctx,maxMsgSize,window); err!=nil { return nil,err }
	return s,nil
}

func (m *Mux) Accept(ctx context.Context, kind StreamKind, maxMsgSize uint64, window uint64) (*Stream,error) {
	ks,ok := m.kinds[kind]
	if !ok { return nil,fmt.Errorf("kind %v not available",kind) }
	state,err := utils.Recv(ctx,ks.acceptsQueue)
	if err!=nil { return nil,err }
	s := &Stream{state,m.queue}
	if err := s.open(ctx,maxMsgSize,window); err!=nil { return nil,err }
	return s,nil
}
