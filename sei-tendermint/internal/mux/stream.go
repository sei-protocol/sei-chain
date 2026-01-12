package mux

import (
	"context"
	"errors"
	"fmt"
	"github.com/tendermint/tendermint/libs/utils"
)

var errClosed = errors.New("closed")

type Stream struct {
	state *streamState
	queue *utils.Watch[queue]
}

func (s *Stream) maxSendMsgSize() uint64 {
	for send := range s.state.send.Lock() {
		return send.maxMsgSize
	}
	panic("unreachable")
}

// open() opens the recv end of the Stream. Permits the peer to send "window" messages, up to maxMsgSize bytes each.
// Up to maxMsgSize*window bytes will be cached locally during the life of this Stream.
// Whenever you call Recv, you specify whether window should grow (i.e. whether to report that the messages
// have been consumed and he can send more).
func (s *Stream) open(ctx context.Context, maxMsgSize uint64, window uint64) error {
	for recv := range s.state.recv.Lock() {
		if recv.opened {
			return fmt.Errorf("already opened")
		}
		recv.opened = true
		recv.maxMsgSize = maxMsgSize
		recv.end = window
		recv.msgs = make([][]byte, window)
		for queue, ctrl := range s.queue.Lock() {
			if len(queue) == 0 {
				ctrl.Updated()
			}
			f := queue.Get(s.state.id)
			f.Header.Kind = utils.Alloc(uint64(s.state.kind))
			f.Header.MaxMsgSize = utils.Alloc(maxMsgSize)
			f.Header.WindowEnd = utils.Alloc(window)
		}
	}
	for send, ctrl := range s.state.send.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool { return send.remoteOpened }); err != nil {
			s.Close()
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
	for send, ctrl := range s.state.send.Lock() {
		// Wait until the local buffer is empty && remote buffer has capacity.
		if err := ctrl.WaitUntil(ctx, func() bool {
			for c := range s.state.closed.RLock() {
				// Will we never be able to send...
				never := c.local || (c.remote && send.begin == send.end)
				// ...or we can send now.
				return never || (send.bufBegin == send.begin && send.begin < send.end)
			}
			panic("unreachable")
		}); err != nil {
			return err
		}
		for c := range s.state.closed.RLock() {
			if c.local || (c.remote && send.begin == send.end) {
				return errClosed
			}
		}
		// We check msg size AFTER waiting because maxMsgSize could be set AFTER we wait.
		if uint64(len(msg)) > send.maxMsgSize {
			return fmt.Errorf("message too large")
		}
		send.begin += 1
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

// Close sends a final CLOSE flag to the peer.
// All subsequent Send calls will fail.
// Recv calls will no longer be able to free buffer space.
func (s *Stream) Close() {
	for c := range s.state.closed.Lock() {
		if c.local {
			return
		}
		c.local = true
		for queue, ctrl := range s.queue.Lock() {
			if len(queue) == 0 {
				ctrl.Updated()
			}
			f := queue.Get(s.state.id)
			f.Header.Close = utils.Alloc(true)
		}
	}
	// Send is affected.
	for _, ctrl := range s.state.send.Lock() {
		ctrl.Updated()
	}
}

// Recv receives a message from peer. Blocks until message is available OR
// until peer has closed their end of the Stream.
// If freeBuffer is set, it permits the peer to send more messages (since local buffer was freed).
func (s *Stream) Recv(ctx context.Context, freeBuffer bool) ([]byte, error) {
	for recv, ctrl := range s.state.recv.Lock() {
		if err := ctrl.WaitUntil(ctx, func() bool {
			// A message is available...
			if recv.begin < recv.used {
				return true
			}
			// ...or peer closed the Stream.
			for c := range s.state.closed.RLock() {
				return c.remote
			}
			panic("unreachable")
		}); err != nil {
			return nil, err
		}
		if recv.begin == recv.used {
			return nil, errClosed
		}
		i := recv.begin % uint64(len(recv.msgs))
		msg := recv.msgs[i]
		recv.msgs[i] = nil
		recv.begin += 1
		// Free buffer if requested AND the Stream was not closed locally.
		if freeBuffer {
			for c := range s.state.closed.RLock() {
				if c.local {
					break
				}
				recv.end = recv.begin + uint64(len(recv.msgs))
				for queue, ctrl := range s.queue.Lock() {
					if len(queue) == 0 {
						ctrl.Updated()
					}
					f := queue.Get(s.state.id)
					f.Header.WindowEnd = utils.Alloc(recv.end)
				}
			}
		}
		return msg, nil
	}
	panic("unreachable")
}
