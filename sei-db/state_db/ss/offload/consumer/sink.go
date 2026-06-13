package consumer

import (
	"context"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Topic/Partition/Offset are kept alongside Entry so sinks can be idempotent
// across replayed Kafka messages.
type Record struct {
	Topic     string
	Partition int
	Offset    int64
	Entry     *dbproto.ChangelogEntry
}

type Sink interface {
	Write(ctx context.Context, rec Record) error
	LastVersion(ctx context.Context) (int64, error)
	Close() error
}

type BatchSink interface {
	WriteBatch(ctx context.Context, records []Record) error
}
