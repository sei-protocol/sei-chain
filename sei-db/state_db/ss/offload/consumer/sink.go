package consumer

import (
	"fmt"

	gogoproto "github.com/gogo/protobuf/proto"

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

func DecodeEntry(payload []byte) (*dbproto.ChangelogEntry, error) {
	entry := &dbproto.ChangelogEntry{}
	if err := gogoproto.Unmarshal(payload, entry); err != nil {
		return nil, fmt.Errorf("decode changelog entry: %w", err)
	}
	return entry, nil
}
