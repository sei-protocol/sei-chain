package mux

import (
	"context"
	"errors"
	"fmt"
	"github.com/tendermint/tendermint/libs/utils"
)

var errRemoteClosed = errors.New("remote closed")
var errClosed = errors.New("closed")

type Stream struct {
	state *streamState
	queue *utils.Watch[queue]
}

func (s *Stream) maxSendMsgSize() uint64 {
	for inner := range s.state.inner.Lock() {
		return inner.send.maxMsgSize
	}
	panic("unreachable")
}

// open() opens the recv end of the Stream. Permits the peer to send "window" messages, up to maxMsgSize bytes each.
// Up to maxMsgSize*window bytes will be cached locally during the life of this Stream.
// Whenever you call Recv, you specify whether window should grow (i.e. whether to report that the messages
// have been consumed and he can send more).
func (s *Stream) open(ctx context.Context, maxMsgSize uint64, window uint64) error {
	for inner, ctrl := range s.state.inner.Lock() {
		if inner.recv.opened {
			return fmt.Errorf("already opened")
		}
		inner.recv.opened = true
		inner.recv.maxMsgSize = maxMsgSize
		inner.recv.end = window
		inner.recv.msgs = make([][]byte, window)
		for queue, ctrl := range s.queue.Lock() {
			if len(queue) == 0 {
				ctrl.Updated()
			}
			f := queue.Get(s.state.id)
			f.Header.Kind = utils.Alloc(uint64(s.state.kind))
			f.Header.MaxMsgSize = utils.Alloc(maxMsgSize)
			f.Header.WindowEnd = utils.Alloc(window)
		}
		if err := ctrl.WaitUntil(ctx, func() bool { return inner.send.remoteOpened }); err != nil {
			s.close(inner)
			ctrl.Updated()
			return err
		}
	}
	return nil
}

// Send sends a message to peer. Blocks until:
// * peer has permitted to send them a message (i.e. there is space in their local buffer)
// * the previous message has been sent by the multiplexer (at most 1 message per Stream is cached at all times)
// Returns an error if Close() was called already.
// Returns an error if the message is too large (exceeds maxMsgSize declared by the peer).
func (s *Stream) Send(ctx context.Context, msg []byte) error {
	for inner, ctrl := range s.state.inner.Lock() {
		// Wait until the local buffer is empty && remote buffer has capacity.
		if err := ctrl.WaitUntil(ctx, func() bool {
			// Will we never be able to send...
			never := inner.closed.local || (inner.closed.remote && inner.send.begin == inner.send.end)
			// ...or we can send now.
			return never || (inner.send.bufBegin == inner.send.begin && inner.send.begin < inner.send.end)
		}); err != nil {
			return err
		}
		if inner.closed.local {
			return errClosed
		}
		if inner.send.begin == inner.send.end {
			return errRemoteClosed
		}
		// We check msg size AFTER waiting because maxMsgSize could be set AFTER we wait.
		if uint64(len(msg)) > inner.send.maxMsgSize {
			return errTooLargeMsg
		}
		inner.send.begin += 1
		// Push msg to the queue.
		for queue, ctrl := range s.queue.Lock() {
			if len(queue) == 0 {
				ctrl.Updated()
			}
			f := queue.Get(s.state.id)
			f.Payload = msg
			f.Header.PayloadSize = utils.Alloc(uint64(len(msg)))
			f.Header.MsgEnd = utils.Alloc(true)
		}
	}
	return nil
}

func (s *Stream) close(inner *streamStateInner) {
	if inner.closed.local {
		return
	}
	inner.closed.local = true
	for queue, ctrl := range s.queue.Lock() {
		if len(queue) == 0 {
			ctrl.Updated()
		}
		f := queue.Get(s.state.id)
		f.Header.Close = utils.Alloc(true)
	}
}

// Close sends a final CLOSE flag to the peer.
// All subsequent Send calls will fail.
// Recv calls will no longer be able to free buffer space.
// NOTE: we may consider separating Close into SendClose and RecvClose,
// to make send and recv parts of the stream entirely independent.
func (s *Stream) Close() {
	for inner, ctrl := range s.state.inner.Lock() {
		s.close(inner)
		ctrl.Updated()
	}
}

// Recv receives a message from peer. Blocks until message is available OR
// until peer has closed their end of the Stream.
// If freeBuffer is set, it permits the peer to send more messages (since local buffer was freed).
func (s *Stream) Recv(ctx context.Context, freeBuffer bool) ([]byte, error) {
	for inner, ctrl := range s.state.inner.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			// A message is available or peer closed the Stream.
			return inner.recv.begin < inner.recv.used || inner.closed.remote
		}); err != nil {
			return nil, err
		}
		if inner.recv.begin == inner.recv.used {
			return nil, errRemoteClosed
		}
		i := inner.recv.begin % uint64(len(inner.recv.msgs))
		msg := inner.recv.msgs[i]
		inner.recv.msgs[i] = nil
		inner.recv.begin += 1
		// Free buffer if requested AND the stream was not closed locally.
		if freeBuffer && !inner.closed.local {
			inner.recv.end = inner.recv.begin + uint64(len(inner.recv.msgs))
			for queue, ctrl := range s.queue.Lock() {
				if len(queue) == 0 {
					ctrl.Updated()
				}
				f := queue.Get(s.state.id)
				f.Header.WindowEnd = utils.Alloc(inner.recv.end)
			}
		}
		return msg, nil
	}
	panic("unreachable")
}
