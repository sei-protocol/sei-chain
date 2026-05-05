package consumer

import (
	"context"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Record is one Kafka message handed to a Sink, carrying the decoded
// ChangelogEntry plus the Kafka coordinates needed for idempotent writes.
type Record struct {
	Topic     string
	Partition int
	Offset    int64
	Entry     *dbproto.ChangelogEntry
}

// Sink persists decoded changelog entries to a downstream store.
// Implementations must be safe to call sequentially from a single reader
// loop and should tolerate replayed records.
type Sink interface {
	Write(ctx context.Context, rec Record) error
	LastVersion(ctx context.Context) (int64, error)
	Close() error
}

type BatchSink interface {
	WriteBatch(ctx context.Context, records []Record) error
}
