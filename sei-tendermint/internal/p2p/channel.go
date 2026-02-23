package p2p

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type ChannelID = conn.ChannelID

type ChannelDescriptor[T proto.Message] = conn.ChannelDescriptorT[T]

// sendMsg is a message to be sent to a peer.
type sendMsg struct {
	Message   proto.Message // message payload
	ChannelID ChannelID     // channel id
}

// RecvMsg is a message received from a peer.
type RecvMsg[T proto.Message] struct {
	Message T            // message payload
	From    types.NodeID // sender
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
	desc      conn.ChannelDescriptor
	recvQueue *Queue[RecvMsg[proto.Message]] // inbound messages (peers to reactors)
}

type Channel[T proto.Message] struct {
	*channel
	router *Router
}

// NewChannel creates a new channel. It is primarily for internal and test
// use, reactors should use Router.OpenChannel().
func newChannel(desc conn.ChannelDescriptor) *channel {
	return &channel{
		desc: desc,
		// TODO(gprusak): get rid of this random cap*cap value once we understand
		// what the sizes per channel really should be.
		recvQueue: NewQueue[RecvMsg[proto.Message]](desc.RecvBufferCapacity * desc.RecvBufferCapacity),
	}
}

func (ch *Channel[T]) send(msg T, queues ...*Queue[sendMsg]) {
	ch.router.metrics.ChannelMsgs.With("ch_id", fmt.Sprint(ch.desc.ID), "direction", "out").Add(1.)
	m := sendMsg{msg, ch.desc.ID}
	size := proto.Size(msg)
	for _, q := range queues {
		if pruned, ok := q.Send(m, size, ch.desc.Priority).Get(); ok {
			ch.router.metrics.QueueDroppedMsgs.With("ch_id", fmt.Sprint(pruned.ChannelID), "direction", "out").Add(float64(1))
		}
	}
}

func (ch *Channel[T]) Send(msg T, to types.NodeID) {
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
func (ch *Channel[T]) Broadcast(msg T) {
	var queues []*Queue[sendMsg]
	for _, c := range ch.router.peerManager.Conns().All() {
		if _, ok := c.peerChannels[ch.desc.ID]; ok {
			queues = append(queues, c.sendQueue)
		}
	}
	ch.send(msg, queues...)
}

func (ch *Channel[T]) String() string {
	return fmt.Sprintf("p2p.Channel<%d:%s>", ch.desc.ID, ch.desc.Name)
}

func (ch *Channel[T]) ReceiveLen() int { return ch.recvQueue.Len() }

// Recv Receives the next message from the channel.
func (ch *Channel[T]) Recv(ctx context.Context) (RecvMsg[T], error) {
	recv, err := ch.recvQueue.Recv(ctx)
	if err != nil {
		return RecvMsg[T]{}, err
	}
	ch.router.metrics.ChannelMsgs.With("ch_id", fmt.Sprint(ch.desc.ID), "direction", "in").Add(1.)
	return RecvMsg[T]{Message: recv.Message.(T), From: recv.From}, nil
}
