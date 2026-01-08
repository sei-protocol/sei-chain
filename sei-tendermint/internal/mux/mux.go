package mux

import (
	"io"
	"fmt"
	"context"
	"net"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/internal/mux/pb"
	"github.com/tendermint/tendermint/internal/protoutils"
	"google.golang.org/protobuf/proto"
)

type StreamID uint64
type StreamKind uint64

func (id StreamID) isConnect() bool { return id&1==0 }

func StreamIDFromRemote(x uint64) StreamID { return StreamID(x^1) }

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

type SendState struct {
	remoteOpened bool
	maxMsgSize uint64
	bufBegin uint64
	begin uint64
	end uint64
	closed bool
	remoteClosed bool
}

type RecvState struct {
	opened bool
	maxMsgSize uint64
	begin uint64
	used uint64
	end uint64
	msgs [][]byte
	closed bool
	remoteClosed bool
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
		if !send.remoteOpened && !send.closed {
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
		if send.opened { return fmt.Errorf("already opened") }
		send.opened = true
		send.maxMsgSize = maxMsgSize
		ctrl.Updated()
	}
	return nil
}

func (s *StreamState) RemoteClose() error {
	for recv,ctrl := range s.recv.Lock() {
		if recv.closed { return fmt.Errorf("already closed") }
		recv.closed = true
		ctrl.Updated()
	}
	return nil
}

func (s *StreamState) RemoteMsgEnd() error {
	for recv,ctrl := range s.recv.Lock() {
		if !recv.opened || recv.closed { return fmt.Errorf("closed") }
		if recv.used==recv.end { return fmt.Errorf("buffer full") }
		recv.used += 1
		ctrl.Updated()
	}
	return nil
}

type Frame struct {
	Header *pb.Header
	Payload []byte
}

type Stream struct {
	state *StreamState
	queue *utils.Watch[Queue]
}

// Opens the recv end of the stream. Permits the peer to send "window" messages, up to maxMsgSize bytes each.
// Up to maxMsgSize*window bytes will be cached locally during the life of this stream.
// Whenever you call Recv, you specify whether window should grow (i.e. whether to report that the messages
// have been consumed and he can send more).
func (s *Stream) open(ctx context.Context, maxMsgSize uint64, window uint64) error {
	for recv := range s.state.recv.Lock() {
		if recv.opened { return fmt.Errorf("already opened") }
		recv.opened = true
		recv.maxMsgSize = maxMsgSize
		recv.end = window
		recv.msgs = make([][]byte,window)
		for queue,ctrl := range s.queue.Lock() {
			if len(queue)==0 { ctrl.Updated() }
			f := queue.Get(s.state.id)
			f.Header.Kind = utils.Alloc(uint64(s.state.kind))
			f.Header.MaxMsgSize = utils.Alloc(maxMsgSize)
			f.Header.WindowEnd = utils.Alloc(window)
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

// Send sends a message to peer. Blocks until:
// * peer has permitted to send them a message (i.e. there is space in their local buffer)
// * the previous message has been sent by the multiplexer (at most 1 message per stream is cached at all times)
// Returns an error if Close() was called already.
// Returns an error if the message is too large (exceeds maxMsgSize declared by the peer). 
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
			if len(queue)==0 { ctrl.Updated() }
			f := queue.Get(s.state.id)
			f.Payload = msg
			f.Header.PayloadSize = utils.Alloc(uint64(len(msg)))
			f.Header.MsgEnd = utils.Alloc(true)
		}
	}
	return nil
}

// Close sends a final CLOSE flag to the peer.
// All subsequent Send calls will fail.
// Recv calls will no longer be able to free buffer space.
func (s *Stream) Close() {
	for send := range s.state.send.Lock() {
		if send.closed {
			return
		}
		send.closed = true
		for queue,ctrl := range s.queue.Lock() {
			if len(queue)==0 { ctrl.Updated() }
			f := queue.Get(s.state.id)
			f.Header.Close = utils.Alloc(true)
		}
	}
}

// Recv receives a message from peer. Blocks until message is available OR
// until peer has closed their end of the stream.
// If freeBuffer is set, it permits the peer to send more messages (since local buffer was freed).
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
				if len(queue)==0 { ctrl.Updated() }
				f := queue.Get(s.state.id)
				f.Header.WindowEnd = utils.Alloc(recv.end)
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
			frames := make([]*Frame,0,len(queue))
			for id := range queue {
				frames = append(frames,queue.Pop(id,r.mux.cfg.FrameSize))
			}
			flush = len(queue)==0
		}
		// Send the frames
		for _,f := range frames {
			id := StreamID(f.Header.Id)
			if f.Header.GetMsgEnd() {
				// Notify sender about local buffer capacity.
				for inner := range r.inner.RLock() {
					for send,ctrl := range inner.streams[id].send.Lock() {
						send.bufBegin += 1
						ctrl.Updated()
					}
				}
			}
			if f.Header.GetClose() {
				r.tryPrune(id)	
			}
			headerRaw,err := proto.Marshal(f.Header)
			if err!=nil { panic(err) }
			if _,err:=conn.Write([]byte{byte(len(headerRaw))}); err!=nil { return err }
			if _,err:=conn.Write(headerRaw); err!=nil { return err }
			if _,err:=conn.Write(f.Payload); err!=nil { return err }
		}
		if flush {
			// TODO: flush
			//if err:=conn.Flush(); err!=nil { return err }
		}
	}
}

func (r *runner) runRecv(ctx context.Context, conn net.Conn) error {
	for {
		// Frame size is hard capped here at 255B.
		// Currently we have 7 varint fields (up to 77B)
		var headerSize [1]byte
		if _,err := conn.Read(headerSize[:]); err!=nil { return err }
		headerRaw := make([]byte, headerSize[0])
		if _,err:=io.ReadFull(conn,headerRaw[:]); err!=nil { return err }
		var h pb.Header
		if err:=proto.Unmarshal(headerRaw,&h); err!=nil { return err }
		id := StreamIDFromRemote(h.Id)
		kind := StreamKind(h.GetKind())
		s,err := r.getOrAccept(id,kind)	
		if err!=nil { return err }
		if mms := h.MaxMsgSize; mms!=nil {
			if err:=s.RemoteOpen(*mms); err!=nil { return err }
			r.tryPrune(s.id) // tryPrune the stream in case it was orphaned.
			if !s.id.isConnect() {
				r.mux.kinds[kind].acceptsQueue <- s
			}
		}
		if we:=h.WindowEnd; we!=nil {
			for send,ctrl := range s.send.Lock() {
				if send.end<*we {
					send.end = *we
					ctrl.Updated()
				}
			}
		}
		if ps:=h.PayloadSize; ps!=nil {
			// check if there is place for the payload.
			for recv := range s.recv.Lock() {
				if recv.closed || !recv.opened { return fmt.Errorf("closed") }
				if recv.used==recv.end {
					return fmt.Errorf("buffer full")
				}
				i := int(recv.used)%len(recv.msgs)
				if recv.maxMsgSize-uint64(len(recv.msgs[i])) < *ps {
					return fmt.Errorf("msg too large")
				}
			}
			// Read the payload.
			payload := make([]byte,*h.PayloadSize)
			if _,err:=io.ReadFull(conn,payload[:]); err!=nil { return err }
			for recv := range s.recv.Lock() {
				i := int(recv.used)%len(recv.msgs)
				recv.msgs[i] = append(recv.msgs[i],payload...)
			}
		}
		if h.GetMsgEnd() {
			if err:=s.RemoteMsgEnd(); err!=nil { return err }	
		}
		if h.GetClose() {
			if err:=s.RemoteClose(); err!=nil { return err }	
			r.tryPrune(s.id)	
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

type Queue map[StreamID]*Frame

func (q Queue) Get(id StreamID) *Frame {
	f,ok := q[id]	
	if ok { return f }
	q[id] = &Frame{Header:&pb.Header{Id:uint64(id)}}
	return q[id]
}

func (q Queue) Pop(id StreamID, maxPayload uint64) *Frame {
	f,ok := q[id]
	if !ok { panic(fmt.Errorf("missing frame")) }
	if uint64(len(f.Payload)) <= maxPayload {
		delete(q,id)
		return f
	}
	out := &Frame {
		Header: protoutils.Clone(f.Header),
		Payload: f.Payload[:maxPayload],
	}
	out.Header.MsgEnd = nil
	out.Header.Close = nil
	f.Header.Kind = nil
	f.Header.MaxMsgSize = nil
	f.Payload = f.Payload[maxPayload:]
	return out
}

type Mux struct {
	cfg *Config
	kinds map[StreamKind]*KindState
	queue *utils.Watch[Queue]
}

func (m *Mux) NewMux(cfg *Config) *Mux {
	kinds := map[StreamKind]*KindState{}
	for kind,c := range cfg.Kinds {
		kinds[kind] = &KindState {
			acceptsQueue: make(chan *StreamState, c.MaxAccepts),
			connectsQueue: make(chan *StreamState, c.MaxConnects),
		}
	}
	queue := utils.NewWatch(Queue{})
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
