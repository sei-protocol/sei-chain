package rlog

import (
	"fmt"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/tidwall/wal"
)

var (
	_ Reader = (*RLReader)(nil)
)

type RLReader struct {
	rlog   *wal.Log
	logger logger.Logger
	config Config
}

type ReaderConfig struct {
}

func NewReader(logger logger.Logger, rlog *wal.Log, config Config) (*RLReader, error) {
	return &RLReader{rlog: rlog, logger: logger, config: config}, nil
}

// Replay will read the replay log and process each log entry with the provided function
func (reader *RLReader) Replay(start uint64, end uint64, processFn func(index uint64, entry proto.ReplayLogEntry) error) error {
	for i := start; i <= end; i++ {
		var entry proto.ReplayLogEntry
		bz, err := reader.rlog.Read(i)
		if err != nil {
			return fmt.Errorf("read rlog failed, %w", err)
		}
		if err := entry.Unmarshal(bz); err != nil {
			return fmt.Errorf("unmarshal rlog failed, %w", err)
		}
		err = processFn(i, entry)
		if err != nil {
			return err
		}
	}
	return nil
}
