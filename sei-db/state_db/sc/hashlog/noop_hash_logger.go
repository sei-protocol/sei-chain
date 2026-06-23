package hashlog

import "github.com/sei-protocol/sei-chain/sei-db/proto"

var _ HashLogger = (*noOpHashLogger)(nil)

// noOpHashLogger is a HashLogger that records nothing. Useful for tests and for nodes where hash logging is
// disabled, so callers don't need to nil-check the logger.
type noOpHashLogger struct{}

// NewNoOpHashLogger creates a HashLogger that does nothing.
func NewNoOpHashLogger() HashLogger {
	return &noOpHashLogger{}
}

func (n *noOpHashLogger) ReportChangeset(uint64, []*proto.NamedChangeSet) {
	// intentional no-op
}

func (n *noOpHashLogger) ReportHash(uint64, string, []byte) error {
	// intentional no-op
	return nil
}

func (n *noOpHashLogger) Close() error {
	// intentional no-op
	return nil
}
