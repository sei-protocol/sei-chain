// mux package provides a TCP connection multiplexer - it allows to run
// multiple reliable independent bidirectional streams over a single TCP connection:
// The data is sent in frames of bounded size in round robin fashion over all the streams
// (fairness). There is no head-of-line blocking: a sender is not allowed to send bytes,
// until peer allows it - a TCP-like buffer window is maintained: peer declares the
// maximal size of message it is willing to consume, and the number of messages it currently
// can buffer locally.
//
// Each mux Stream has its own Kind number. Kind numbers are supposed to identify the Stream-level communication
// protocol (for example, if you implement an RPC server on top of this multiplexer, each RPC will have its own Kind number).
package mux

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/tendermint/tendermint/internal/mux/pb"
	"github.com/tendermint/tendermint/internal/p2p/conn"
	"github.com/tendermint/tendermint/internal/protoutils"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"google.golang.org/protobuf/proto"
	"io"
)

const handshakeMaxSize = 10 * 1024 // 10kB

type Config struct {
	// Maximal number of bytes in a frame (excluding header).
	FrameSize uint64
	// Limits on the number of concurrent streams of each kind.
	Kinds map[StreamKind]*StreamKindConfig
}

type StreamKindConfig struct {
	// Maximal number of concurrent outbound streams.
	MaxConnects uint64
	// Maximal number of concurrent inbound streams.
	MaxAccepts uint64
}

type handshake struct {
	Kinds map[StreamKind]*StreamKindConfig
}

var handshakeConv = protoutils.Conv[*handshake, *pb.Handshake]{
	Encode: func(h *handshake) *pb.Handshake {
		var kinds []*pb.StreamKindConfig
		for kind, c := range h.Kinds {
			kinds = append(kinds, &pb.StreamKindConfig{
				Kind:        uint64(kind),
				MaxConnects: c.MaxConnects,
				MaxAccepts:  c.MaxAccepts,
			})
		}
		return &pb.Handshake{Kinds: kinds}
	},
	Decode: func(x *pb.Handshake) (*handshake, error) {
		kinds := map[StreamKind]*StreamKindConfig{}
		for _, pc := range x.Kinds {
			kinds[StreamKind(pc.Kind)] = &StreamKindConfig{
				MaxConnects: pc.MaxConnects,
				MaxAccepts:  pc.MaxAccepts,
			}
		}
		return &handshake{Kinds: kinds}, nil
	},
}

type frame struct {
	Header  *pb.Header
	Payload []byte
}

type kindState struct {
	connectsQueue chan *streamState
	acceptsQueue  chan *streamState
}

type runnerInner struct {
	nextID     streamID
	streams    map[streamID]*streamState
	acceptsSem map[StreamKind]uint64
}

// State of the running multiplexer.
type runner struct {
	mux   *Mux
	inner utils.RWMutex[*runnerInner]
}

func newRunner(mux *Mux) *runner {
	return &runner{
		mux: mux,
		inner: utils.NewRWMutex(&runnerInner{
			nextID:     0,
			streams:    map[streamID]*streamState{},
			acceptsSem: map[StreamKind]uint64{},
		}),
	}
}

// getOrAccept() gets the current state of the stream with the given id (kind is ignored).
// If the stream does not exist yet, it tries to create it as an accept (inbound) stream.
// In that case the inbound stream limit for the given kind is checked.
func (r *runner) getOrAccept(id streamID, kind StreamKind) (*streamState, error) {
	for inner := range r.inner.RLock() {
		s, ok := inner.streams[id]
		if ok {
			return s, nil
		}
		if id.isConnect() {
			return nil, fmt.Errorf("peer tried to open stream with bad id")
		}
		if inner.acceptsSem[kind] == 0 {
			return nil, fmt.Errorf("too many concurrent accept streams")
		}
		inner.acceptsSem[kind] -= 1
		s = newStreamState(id, kind)
		inner.streams[id] = s
		return s, nil
	}
	panic("unreachable")
}

func (r *runner) tryPrune(id streamID) {
	for inner := range r.inner.Lock() {
		// Check if the Stream is fully closed.
		s, ok := inner.streams[id]
		if !ok {
			return
		}
		for c := range s.closed.RLock() {
			if !c.remote || !c.local {
				return
			}
		}
		// Delete Stream state.
		delete(inner.streams, id)
		// Free the Stream capacity.
		if id.isConnect() {
			// Non-blocking since we just closed a connect Stream.
			r.mux.kinds[s.kind].connectsQueue <- newStreamState(inner.nextID, s.kind)
			inner.nextID += 2
		} else {
			inner.acceptsSem[s.kind] += 1
		}
	}
}

// runSend handles the frame queue.
// The frames from all streams are interleaved in a round robin fashion.
// frames have bounded size to make sure that large messages do not slow down smaller ones.
// Stream priorities are not implemented (not needed).
// WARNING: it respects ctx only partially, because conn does not.
func (r *runner) runSend(ctx context.Context, conn conn.Conn) error {
	for {
		// Collect frames in round robin over streams.
		var frames []*frame
		flush := false
		for queue, ctrl := range r.mux.queue.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool { return len(queue) > 0 }); err != nil {
				return err
			}
			frames = make([]*frame, 0, len(queue))
			for id := range queue {
				frames = append(frames, queue.Pop(id, r.mux.cfg.FrameSize))
			}
			flush = len(queue) == 0
		}
		// Send the frames
		for _, f := range frames {
			id := streamID(f.Header.Id)
			if f.Header.GetMsgEnd() {
				// Notify sender about local buffer capacity.
				for inner := range r.inner.RLock() {
					for send, ctrl := range inner.streams[id].send.Lock() {
						send.bufBegin += 1
						ctrl.Updated()
					}
				}
			}
			if f.Header.GetClose() {
				r.tryPrune(id)
			}
			headerRaw, err := proto.Marshal(f.Header)
			if err != nil {
				panic(err)
			}
			if _, err := conn.Write([]byte{byte(len(headerRaw))}); err != nil {
				return err
			}
			if _, err := conn.Write(headerRaw); err != nil {
				return err
			}
			if _, err := conn.Write(f.Payload); err != nil {
				return err
			}
		}
		if flush {
			if err:=conn.Flush(); err!=nil { return err }
		}
	}
}

// runRecv receives and processes the incoming frames sequentially.
func (r *runner) runRecv(conn conn.Conn) error {
	for {
		// frame size is hard capped here at 255B.
		// Currently we have 7 varint fields (up to 77B)
		var headerSize [1]byte
		if _, err := conn.Read(headerSize[:]); err != nil {
			return err
		}
		headerRaw := make([]byte, headerSize[0])
		if _, err := io.ReadFull(conn, headerRaw[:]); err != nil {
			return err
		}
		var h pb.Header
		if err := proto.Unmarshal(headerRaw, &h); err != nil {
			return err
		}
		id := streamIDFromRemote(h.Id)
		kind := StreamKind(h.GetKind())

		s, err := r.getOrAccept(id, kind)
		if err != nil {
			return err
		}
		for c := range s.closed.RLock() {
			if c.remote {
				return fmt.Errorf("frame after CLOSE frame")
			}
		}
		// Process the frame content in order: OPEN, RESIZE, MSG, CLOSE
		if mms := h.MaxMsgSize; mms != nil {
			if err := s.RemoteOpen(*mms); err != nil {
				return err
			}
			if !s.id.isConnect() {
				r.mux.kinds[kind].acceptsQueue <- s
			}
		}
		if we := h.WindowEnd; we != nil {
			for send, ctrl := range s.send.Lock() {
				if send.end < *we {
					send.end = *we
					ctrl.Updated()
				}
			}
		}
		if ps := h.GetPayloadSize(); ps > 0 {
			if err := s.RemotePayloadSize(ps); err != nil {
				return err
			}
			// Read the payload.
			payload := make([]byte, ps)
			if _, err := io.ReadFull(conn, payload[:]); err != nil {
				return err
			}
			s.RemotePayload(payload)
		}
		if h.GetMsgEnd() {
			if err := s.RemoteMsgEnd(); err != nil {
				return err
			}
		}
		if h.GetClose() {
			if err := s.RemoteClose(); err != nil {
				return err
			}
			r.tryPrune(s.id)
		}
	}
}

// Run runs the multiplexer for the given connection.
// It closes the connection before return.
func (m *Mux) Run(ctx context.Context, conn conn.Conn) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		// Close on cancel.
		s.Spawn(func() error {
			defer conn.Close()
			<-ctx.Done()
			return nil
		})

		// Handshake exchange.
		handshake, err := scope.Run1(ctx, func(ctx context.Context, s scope.Scope) (*handshake, error) {
			s.Spawn(func() error {
				handshakeRaw, err := proto.Marshal(handshakeConv.Encode(&handshake{Kinds: m.cfg.Kinds}))
				if err != nil {
					panic(err)
				}
				sizeRaw := binary.LittleEndian.AppendUint32(nil, uint32(len(handshakeRaw)))
				if _, err := conn.Write(sizeRaw); err != nil {
					return err
				}
				if _, err := conn.Write(handshakeRaw); err != nil {
					return err
				}
				return conn.Flush()
			})
			var sizeRaw [4]byte
			if _, err := io.ReadFull(conn, sizeRaw[:]); err != nil {
				return nil, err
			}
			size := binary.LittleEndian.Uint32(sizeRaw[:])
			if size > handshakeMaxSize {
				return nil, fmt.Errorf("handshake too large")
			}
			handshakeRaw := make([]byte, size)
			if _, err := io.ReadFull(conn, handshakeRaw[:]); err != nil {
				return nil, err
			}
			var handshakeProto pb.Handshake
			if err := proto.Unmarshal(handshakeRaw, &handshakeProto); err != nil {
				return nil, err
			}
			return handshakeConv.Decode(&handshakeProto)
		})
		if err != nil {
			return err
		}

		// Initialize runner with handshake data.
		r := newRunner(m)
		for inner := range r.inner.Lock() {
			for kind, cfg := range m.cfg.Kinds {
				remCfg,ok := handshake.Kinds[kind]
				if !ok { remCfg = &StreamKindConfig{} } 
				inner.acceptsSem[kind] = min(cfg.MaxAccepts, remCfg.MaxConnects)
				for range min(cfg.MaxConnects, remCfg.MaxAccepts) {
					m.kinds[kind].connectsQueue <- newStreamState(inner.nextID, kind)
					inner.nextID += 2
				}
			}
		}
		// Run the tasks.
		s.Spawn(func() error { return r.runSend(ctx, conn) })
		s.Spawn(func() error { return r.runRecv(conn) })
		return nil
	})
}

// queue is a queue of frames to send, consumed by runSend.
type queue map[streamID]*frame

// Get returns the frame corresponding to the given stream id.
// If it doesn't exist, it initializes the frame first.
func (q queue) Get(id streamID) *frame {
	f, ok := q[id]
	if ok {
		return f
	}
	q[id] = &frame{Header: &pb.Header{Id: uint64(id)}}
	return q[id]
}

// Pop removes a frame of the given stream from the queue.
// Panics if there is no frame for this id.
// If a frame is too large (payload larger than maxPayload) it splits
// the frame into 2 smaller ones and returns the first one.
func (q queue) Pop(id streamID, maxPayload uint64) *frame {
	f, ok := q[id]
	if !ok {
		panic(fmt.Errorf("missing frame"))
	}
	if uint64(len(f.Payload)) <= maxPayload {
		delete(q, id)
		return f
	}
	// Split the frame into first and second.
	first := &frame{
		Header: &pb.Header{
			Id:          f.Header.Id,
			Kind:        f.Header.Kind,
			MaxMsgSize:  f.Header.MaxMsgSize,
			WindowEnd:   f.Header.WindowEnd,
			PayloadSize: &maxPayload,
			// Close and MsgEnd fields are left in the second frame.
		},
		Payload: f.Payload[:maxPayload],
	}
	// Clear the fields from the first frame.
	f.Header.Kind = nil
	f.Header.MaxMsgSize = nil
	f.Header.WindowEnd = nil
	f.Payload = f.Payload[maxPayload:]
	f.Header.PayloadSize = utils.Alloc(uint64(len(f.Payload)))
	return first
}

type Mux struct {
	cfg   *Config
	kinds map[StreamKind]*kindState
	queue *utils.Watch[queue]
}

// NewMux constructs a new multipexer.
// Remember to spawn Mux.Run() afterwards.
func (m *Mux) NewMux(cfg *Config) *Mux {
	kinds := map[StreamKind]*kindState{}
	for kind, c := range cfg.Kinds {
		kinds[kind] = &kindState{
			acceptsQueue:  make(chan *streamState, c.MaxAccepts),
			connectsQueue: make(chan *streamState, c.MaxConnects),
		}
	}
	queue := utils.NewWatch(queue{})
	return &Mux{cfg: cfg, kinds: kinds, queue: &queue}
}

// Connect establishes a new stream of the given kind.
// Blocks until the number of concurrent connects falls below the allowed limit.
// Then it waits until peer accepts the connection.
// Remember to Close() the stream after use.
func (m *Mux) Connect(ctx context.Context, kind StreamKind, maxMsgSize uint64, window uint64) (*Stream, error) {
	ks, ok := m.kinds[kind]
	if !ok {
		return nil, fmt.Errorf("kind %v not available", kind)
	}
	state, err := utils.Recv(ctx, ks.connectsQueue)
	if err != nil {
		return nil, err
	}
	s := &Stream{state, m.queue}
	if err := s.open(ctx, maxMsgSize, window); err != nil {
		return nil, err
	}
	return s, nil
}

// Accept accepts an incoming stream of the given kind.
// Blocks until peer opens a connect stream.
// Remember to Close() the stream after use.
func (m *Mux) Accept(ctx context.Context, kind StreamKind, maxMsgSize uint64, window uint64) (*Stream, error) {
	ks, ok := m.kinds[kind]
	if !ok {
		return nil, fmt.Errorf("kind %v not available", kind)
	}
	state, err := utils.Recv(ctx, ks.acceptsQueue)
	if err != nil {
		return nil, err
	}
	s := &Stream{state, m.queue}
	if err := s.open(ctx, maxMsgSize, window); err != nil {
		return nil, err
	}
	return s, nil
}
