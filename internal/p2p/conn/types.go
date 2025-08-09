package conn

import "google.golang.org/protobuf/proto"

type ChannelID uint16

type ChannelDescriptor struct {
	ID          ChannelID
	Priority    int
	MessageType proto.Message
	// TODO: Remove once p2p refactor is complete.
	SendQueueCapacity   int
	RecvMessageCapacity int
	// RecvBufferCapacity defines the max buffer size of inbound messages for a given p2p Channel queue.
	RecvBufferCapacity int
	// Human readable name of the channel, used in logging and diagnostics.
	Name string
}
