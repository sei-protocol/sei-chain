package execution

import (
	"fmt"
	"maps"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Committees is the committee weights after each block the app has committed.
// It keeps an entry only where the weights change.
type Committees struct {
	// Committed is the latest block the app has committed.
	Committed atypes.RoadIndex
	// changes holds each set of weights and the first block after which it
	// holds, in road order.
	changes []committeeChange
}

type committeeChange struct {
	from    atypes.RoadIndex
	weights map[atypes.PublicKey]uint64
}

// After returns the committee weights after block n, which must not be above
// Committed.
func (c Committees) After(n atypes.RoadIndex) map[atypes.PublicKey]uint64 {
	for i := len(c.changes) - 1; i > 0; i-- {
		if c.changes[i].from <= n {
			return c.changes[i].weights
		}
	}
	return c.changes[0].weights
}

// record returns c with block n committed and weights holding after it.
func (c Committees) record(n atypes.RoadIndex, weights map[atypes.PublicKey]uint64) Committees {
	c.Committed = n
	if len(c.changes) == 0 || !maps.Equal(c.changes[len(c.changes)-1].weights, weights) {
		c.changes = append(c.changes, committeeChange{from: n, weights: weights})
	}
	return c
}

// committeeWeights maps a bonded validator set to Autobahn voting power.
func committeeWeights(vals []abci.ValidatorUpdate) (map[atypes.PublicKey]uint64, error) {
	weights := make(map[atypes.PublicKey]uint64, len(vals))
	for _, v := range vals {
		if v.Power <= 0 {
			continue
		}
		pk, err := crypto.PubKeyFromProto(v.PubKey)
		if err != nil {
			return nil, fmt.Errorf("PubKeyFromProto: %w", err)
		}
		apk, err := atypes.PublicKeyFromBytes(pk.Bytes())
		if err != nil {
			return nil, fmt.Errorf("PublicKeyFromBytes: %w", err)
		}
		if _, dup := weights[apk]; dup {
			return nil, fmt.Errorf("duplicate public key %s", apk)
		}
		power, ok := utils.SafeCast[uint64](v.Power)
		if !ok {
			return nil, fmt.Errorf("validator power %d does not fit uint64", v.Power)
		}
		weights[apk] = power
	}
	return weights, nil
}
