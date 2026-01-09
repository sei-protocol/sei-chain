package mux

import (
	"fmt"
	"github.com/tendermint/tendermint/libs/utils"
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

type streamState struct {
	id     streamID
	kind   StreamKind
	send   utils.Watch[*sendState]
	recv   utils.Watch[*recvState]
	closed utils.RWMutex[*closeState]
}

func newStreamState(id streamID, kind StreamKind) *streamState {
	return &streamState{
		id:     id,
		kind:   kind,
		send:   utils.NewWatch(&sendState{}),
		recv:   utils.NewWatch(&recvState{}),
		closed: utils.NewRWMutex(&closeState{}),
	}
}

func (s *streamState) RemoteOpen(maxMsgSize uint64) error {
	for send, ctrl := range s.send.Lock() {
		if send.remoteOpened {
			return fmt.Errorf("already opened")
		}
		send.remoteOpened = true
		send.maxMsgSize = maxMsgSize
		ctrl.Updated()
	}
	return nil
}

func (s *streamState) RemoteClose() error {
	for c := range s.closed.Lock() {
		if c.remote {
			return fmt.Errorf("already closed")
		}
		c.remote = true
	}
	// Both send and recv are affected.
	for _, ctrl := range s.send.Lock() {
		ctrl.Updated()
	}
	for _, ctrl := range s.recv.Lock() {
		ctrl.Updated()
	}
	return nil
}

func (s *streamState) RemotePayloadSize(payloadSize uint64) error {
	// check if there is place for the payload.
	for recv := range s.recv.Lock() {
		if recv.used == recv.end {
			return fmt.Errorf("buffer full")
		}
		i := int(recv.used) % len(recv.msgs)
		if recv.maxMsgSize-uint64(len(recv.msgs[i])) < payloadSize {
			return fmt.Errorf("msg too large")
		}
	}
	return nil
}

func (s *streamState) RemotePayload(payload []byte) {
	for recv := range s.recv.Lock() {
		i := int(recv.used) % len(recv.msgs)
		recv.msgs[i] = append(recv.msgs[i], payload...)
	}
}

func (s *streamState) RemoteMsgEnd() error {
	for recv, ctrl := range s.recv.Lock() {
		if recv.used == recv.end {
			return fmt.Errorf("buffer full")
		}
		recv.used += 1
		ctrl.Updated()
	}
	return nil
}
