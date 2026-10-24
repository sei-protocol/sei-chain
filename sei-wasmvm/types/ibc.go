package types

type IBCEndpoint struct {
	PortID    string `json:"port_id"`
	ChannelID string `json:"channel_id"`
}

type IBCChannel struct {
	Endpoint             IBCEndpoint `json:"endpoint"`
	CounterpartyEndpoint IBCEndpoint `json:"counterparty_endpoint"`
	Order                IBCOrder    `json:"order"`
	Version              string      `json:"version"`
	ConnectionID         string      `json:"connection_id"`
}

// TODO: test what the sdk Order.String() represents and how to parse back
// Proto files: https://github.com/cosmos/cosmos-sdk/blob/v0.40.0/proto/ibc/core/channel/v1/channel.proto#L69-L80
// Auto-gen code: https://github.com/cosmos/cosmos-sdk/blob/v0.40.0/x/ibc/core/04-channel/types/channel.pb.go#L70-L101
type IBCOrder = string

// These are the only two valid values for IbcOrder
const (
	Unordered = "ORDER_UNORDERED"
	Ordered   = "ORDER_ORDERED"
)

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
