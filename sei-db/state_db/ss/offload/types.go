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

// ReplayRequest scopes a history replay/backfill request.
type ReplayRequest struct {
	FromVersion int64
	ToVersion   int64
}

// Publisher publishes committed changelog entries to an external transport.
type Publisher interface {
	Publish(ctx context.Context, entry *proto.ChangelogEntry) (Ack, error)
}

// Replayer streams changelog entries back for recovery or backfill.
type Replayer interface {
	Replay(ctx context.Context, req ReplayRequest, handler func(*proto.ChangelogEntry) error) error
}

// Stream is the full transport contract used by the state store offload hook.
type Stream interface {
	Publisher
	Replayer
}
