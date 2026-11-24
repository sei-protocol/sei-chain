package statesyncproof

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// DecodeAndVerify decodes the state sync proof file data of road and verifies it against the epoch of
// road in registry. It returns the proof and the app hash that its AppQC signs.
func DecodeAndVerify(registry *types.EpochRegistry, data []byte, road types.RoadHeight) (*Proof, [32]byte, error) {
	ep, err := epochOfRoad(registry, road)
	if err != nil {
		return nil, [32]byte{}, err
	}
	proof, err := Decode(data, road)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("decode the state sync proof file of road %d: %w", road, err)
	}
	if err := proof.Verify(ep); err != nil {
		return nil, [32]byte{}, fmt.Errorf("verify the state sync proof file of road %d against epoch %d: %w", road, ep.EpochIndex(), err)
	}
	appHash := proof.AppQC().Proposal().AppHash()
	if len(appHash) != 32 {
		return nil, [32]byte{}, fmt.Errorf("the AppQC of road %d signs an app hash of %d bytes, want 32", road, len(appHash))
	}
	return proof, [32]byte(appHash), nil
}

// errNoEpoch is returned for a road that no epoch of the epoch registry holds.
var errNoEpoch = errors.New("the epoch registry holds no epoch of road")

// epochOfRoad returns the epoch of registry that holds road.
func epochOfRoad(registry *types.EpochRegistry, road types.RoadHeight) (*types.Epoch, error) {
	for _, ep := range registry.Epochs() {
		if ep.RoadRange().Has(road) {
			return ep, nil
		}
	}
	return nil, fmt.Errorf("%w %d", errNoEpoch, road)
}
