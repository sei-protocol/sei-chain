package mux

import (
	"fmt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type streamID uint64
type StreamKind uint64

func (id streamID) isConnect() bool { return id&1 == 0 }

// The least significant bit of streamID decides whether the stream is
// outbound (connect) or inbound (accept). When receiving streamID from peer
// we need to convert it to local streamID.
func streamIDFromRemote(x uint64) streamID { return streamID(x ^ 1) }

type closeState struct {
	local  bool
	remote bool
}

type sendState struct {
	remoteOpened bool
	maxMsgSize   uint64
	bufBegin     uint64
	begin        uint64
	end          uint64
}

type recvState struct {
	opened     bool
	maxMsgSize uint64
	begin      uint64
	used       uint64
	end        uint64
	msgs       [][]byte
}

type streamStateInner struct {
	send   sendState
	recv   recvState
	closed closeState
}

type streamState struct {
	id    streamID
	kind  StreamKind
	inner utils.Watch[*streamStateInner]
}

func newStreamState(id streamID, kind StreamKind) *streamState {
	return &streamState{
		id:    id,
		kind:  kind,
		inner: utils.NewWatch(&streamStateInner{}),
	}
}

func (s *streamState) RemoteOpen(maxMsgSize uint64) error {
	for inner, ctrl := range s.inner.Lock() {
		if inner.send.remoteOpened {
			return fmt.Errorf("already opened")
		}
		// Do not allow remote open before we connect.
		if s.id.isConnect() && !inner.recv.opened {
			return errUnknownStream
		}
		inner.send.remoteOpened = true
		inner.send.maxMsgSize = maxMsgSize
		ctrl.Updated()
	}
	return nil
}

func (s *streamState) RemoteClose() error {
	for inner, ctrl := range s.inner.Lock() {
		if inner.closed.remote {
			return fmt.Errorf("already closed")
		}
		inner.closed.remote = true
		ctrl.Updated()
	}
	return nil
}

func (s *streamState) RemoteWindowEnd(windowEnd uint64) {
	for inner, ctrl := range s.inner.Lock() {
		if inner.send.end < windowEnd {
			inner.send.end = windowEnd
			ctrl.Updated()
		}
	}
}

// RemotePayloadSize checks if there is place for the payload.
func (s *streamState) RemotePayloadSize(payloadSize uint64) error {
	for inner := range s.inner.Lock() {
		if inner.recv.used == inner.recv.end {
			return errTooManyMsgs
		}
		i := int(inner.recv.used) % len(inner.recv.msgs)
		if inner.recv.maxMsgSize-uint64(len(inner.recv.msgs[i])) < payloadSize {
			return errTooLargeMsg
		}
	}
	return nil
}

func (s *streamState) RemotePayload(payload []byte) {
	for inner := range s.inner.Lock() {
		i := int(inner.recv.used) % len(inner.recv.msgs)
		inner.recv.msgs[i] = append(inner.recv.msgs[i], payload...)
	}
}

func (s *streamState) RemoteMsgEnd() error {
	for inner, ctrl := range s.inner.Lock() {
		if inner.recv.used == inner.recv.end {
			return fmt.Errorf("buffer full")
		}
		inner.recv.used += 1
		ctrl.Updated()
	}
	return nil
}
