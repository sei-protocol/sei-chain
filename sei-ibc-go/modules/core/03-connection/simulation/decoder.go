package simulation

import (
	"bytes"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/kv"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/types"
	host "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/24-host"
)

// NewDecodeStore returns a decoder function closure that unmarshals the KVPair's
// Value to the corresponding connection type.
func NewDecodeStore(cdc codec.BinaryCodec, kvA, kvB kv.Pair) (string, bool) {
	switch {
	case bytes.HasPrefix(kvA.Key, host.KeyClientStorePrefix) && bytes.HasSuffix(kvA.Key, []byte(host.KeyConnectionPrefix)):
		var clientConnectionsA, clientConnectionsB types.ClientPaths
		cdc.MustUnmarshal(kvA.Value, &clientConnectionsA)
		cdc.MustUnmarshal(kvB.Value, &clientConnectionsB)
		return fmt.Sprintf("ClientPaths A: %v\nClientPaths B: %v", clientConnectionsA, clientConnectionsB), true

	case bytes.HasPrefix(kvA.Key, []byte(host.KeyConnectionPrefix)):
		var connectionA, connectionB types.ConnectionEnd
		cdc.MustUnmarshal(kvA.Value, &connectionA)
		cdc.MustUnmarshal(kvB.Value, &connectionB)
		return fmt.Sprintf("ConnectionEnd A: %v\nConnectionEnd B: %v", connectionA, connectionB), true

	default:
		return "", false
	}
}
