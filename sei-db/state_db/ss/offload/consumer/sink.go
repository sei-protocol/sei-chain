package consumer

import (
	"context"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
)

// Topic/Partition/Offset are kept alongside Entry so the sink can write
// idempotently across replays.
type Record struct {
	Topic     string
	Partition int
	Offset    int64
	Entry     *dbproto.ChangelogEntry
}

// Sink implementations are called sequentially from a single reader loop
// and must tolerate replayed records.
type Sink interface {
	Write(ctx context.Context, rec Record) error
	LastVersion(ctx context.Context) (int64, error)
	Close() error
}

type BatchSink interface {
	WriteBatch(ctx context.Context, records []Record) error
}
