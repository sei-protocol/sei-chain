package types

// IBCTimeoutBlock Height is a monotonically increasing data type
// that can be compared against another Height for the purposes of updating and
// freezing clients.
// Ordering is (revision_number, timeout_height)
type IBCTimeoutBlock struct {
	// the version that the client is currently on
	// (eg. after reseting the chain this could increment 1 as height drops to 0)
	Revision uint64 `json:"revision"`
	// block height after which the packet times out.
	// the height within the given revision
	Height uint64 `json:"height"`
}

func (t IBCTimeoutBlock) IsZero() bool {
	return t.Revision == 0 && t.Height == 0
}

// IBCTimeout is the timeout for an IBC packet. At least one of block and timestamp is required.
type IBCTimeout struct {
	Block *IBCTimeoutBlock `json:"block"`
	// Nanoseconds since UNIX epoch
	Timestamp uint64 `json:"timestamp,string,omitempty"`
}
