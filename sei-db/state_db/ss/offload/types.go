package offload

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Ack is the generic acknowledgement returned by a history offload transport.
// Kafka-like systems can map Cursor to an offset, while other queue systems can
// populate only MessageID.
type Ack struct {
	Accepted  bool
	Durable   bool
	MessageID string
	Cursor    string
}

// Publisher publishes committed changelog entries to an external transport.
type Publisher interface {
	Publish(ctx context.Context, entry *proto.ChangelogEntry) (Ack, error)
}

// Stream is the transport contract used by the benchmark offload path.
type Stream interface {
	Publisher
}
