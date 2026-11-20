package p2p

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// VerifyStateSyncProof decodes the state sync proof file data of road and verifies it against the
// epoch of road in registry. It returns the proof and the app hash that its AppQC signs.
func VerifyStateSyncProof(registry *atypes.EpochRegistry, data []byte, road atypes.RoadHeight) (*statesyncproof.Proof, [32]byte, error) {
	ep, err := epochOfRoad(registry, road)
	if err != nil {
		return nil, [32]byte{}, err
	}
	proof, err := statesyncproof.Decode(data, road)
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

// epochOfRoad returns the epoch of registry that holds road.
func epochOfRoad(registry *atypes.EpochRegistry, road atypes.RoadHeight) (*atypes.Epoch, error) {
	for _, ep := range registry.Epochs() {
		if ep.RoadRange().Has(road) {
			return ep, nil
		}
	}
	return nil, fmt.Errorf("the epoch registry holds no epoch of road %d", road)
}
