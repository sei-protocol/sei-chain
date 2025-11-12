package p2p

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/proto"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/types"
)

// sendMsg is a message to be sent to a peer.
type sendMsg struct {
	Message   proto.Message // WRAPPED message payload
	ChannelID ChannelID     // channel id
}

// RecvMsg is a message received from a peer.
type RecvMsg struct {
	Message proto.Message // UNWRAPPED message payload
	From    types.NodeID  // sender
}

// Wrapper is a Protobuf message that can contain a variety of inner messages
// (e.g. via oneof fields). If a Channel's message type implements Wrapper, the
// Router will automatically wrap outbound messages and unwrap inbound messages,
// such that reactors do not have to do this themselves.
type Wrapper interface {
	proto.Message

	// Wrap will take a message and wrap it in this one if possible.
	Wrap(proto.Message) error

	// Unwrap will unwrap the inner message contained in this message.
	Unwrap() (proto.Message, error)
}

// PeerError is a peer error reported via Channel.Error.
//
// FIXME: This currently just disconnects the peer, which is too simplistic.
// For example, some errors should be logged, some should cause disconnects,
// and some should ban the peer.
//
// FIXME: This should probably be replaced by a more general PeerBehavior
// concept that can mark good and bad behavior and contributes to peer scoring.
// It should possibly also allow reactors to request explicit actions, e.g.
// disconnection or banning, in addition to doing this based on aggregates.
type PeerError struct {
	NodeID types.NodeID
	Err    error
	Fatal  bool
}

func (pe PeerError) Error() string { return fmt.Sprintf("peer=%q: %s", pe.NodeID, pe.Err.Error()) }
func (pe PeerError) Unwrap() error { return pe.Err }

// channel is a bidirectional channel to exchange Protobuf messages with peers.
type channel struct {
	desc      ChannelDescriptor
	recvQueue *Queue[RecvMsg] // inbound messages (peers to reactors)
}

type Channel struct {
	*channel
	router *Router
}

// NewChannel creates a new channel. It is primarily for internal and test
// use, reactors should use Router.OpenChannel().
func newChannel(desc ChannelDescriptor) *channel {
	return &channel{
		desc: desc,
		// TODO(gprusak): get rid of this random cap*cap value once we understand
		// what the sizes per channel really should be.
		recvQueue: NewQueue[RecvMsg](desc.RecvBufferCapacity * desc.RecvBufferCapacity),
	}
}

func (ch *Channel) send(msg proto.Message, queues ...*Queue[sendMsg]) {
	// wrap the message if needed
	if wrapper, ok := ch.desc.MessageType.(Wrapper); ok {
		wrapper := utils.ProtoClone(wrapper)
		if err := wrapper.Wrap(msg); err != nil {
			ch.router.logger.Error("failed to wrap message", "channel", ch.desc.ID, "err", err)
			return
		}
		msg = wrapper
	}
	m := sendMsg{msg, ch.desc.ID}
	size := proto.Size(msg)
	for _, q := range queues {
		if pruned, ok := q.Send(m, size, ch.desc.Priority).Get(); ok {
			ch.router.metrics.QueueDroppedMsgs.With("ch_id", fmt.Sprint(pruned.ChannelID), "direction", "out").Add(float64(1))
		}
	}
}

func (ch *Channel) Send(msg proto.Message, to types.NodeID) {
	c, ok := ch.router.peerManager.Conns().Get(to)
	if !ok {
		ch.router.logger.Debug("dropping message for unconnected peer", "peer", to, "channel", ch.desc.ID)
		return
	}
	if _, contains := c.peerChannels[ch.desc.ID]; !contains {
		// reactor tried to send a message across a channel that the
		// peer doesn't have available. This is a known issue due to
		// how peer subscriptions work:
		// https://github.com/tendermint/tendermint/issues/6598
		return
	}
	ch.send(msg, c.sendQueue)
}

// Broadcasts msg to all peers on the channel.
func (ch *Channel) Broadcast(msg proto.Message) {
	var queues []*Queue[sendMsg]
	for _, c := range ch.router.peerManager.Conns().All() {
		if _, ok := c.peerChannels[ch.desc.ID]; ok {
			queues = append(queues, c.sendQueue)
		}
	}
	ch.send(msg, queues...)
}

func (ch *Channel) String() string {
	return fmt.Sprintf("p2p.Channel<%d:%s>", ch.desc.ID, ch.desc.Name)
}

func (ch *Channel) ReceiveLen() int { return ch.recvQueue.Len() }

// Recv Receives the next message from the channel.
func (ch *Channel) Recv(ctx context.Context) (RecvMsg, error) {
	return ch.recvQueue.Recv(ctx)
}
