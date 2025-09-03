package p2p_test

import (
	"bytes"
	"context"
	"encoding/hex"

	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

// Transports are mainly tested by common tests in transport_test.go, we
// register a transport factory here to get included in those tests.
func init() {
	testTransports["memory"] = func() func(context.Context) p2p.Transport {
		network := p2p.NewMemoryNetwork(log.NewNopLogger(), 1)
		return func(ctx context.Context) p2p.Transport {
			i := byte(network.Size())
			nodeID, err := types.NewNodeID(hex.EncodeToString(bytes.Repeat([]byte{i<<4 + i}, 20)))
			if err != nil {
				panic(err)
			}
			t := network.CreateTransport(nodeID)
			go func() {
				if err := t.Run(ctx); err != nil {
					panic(err)
				}
			}()
			return t
		}
	}
}
