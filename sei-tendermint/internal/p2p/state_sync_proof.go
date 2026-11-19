package p2p

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
)

// VerifyStateSyncProof decodes the state sync proof file data of road and verifies it against
// the epoch of road in registry. It returns the proof and the app hash that its AppQC signs.
func VerifyStateSyncProof(registry *epoch.Registry, data []byte, road atypes.RoadIndex) (*statesyncproof.Proof, [32]byte, error) {
	proof, err := statesyncproof.Decode(data, road)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("decode the state sync proof of road %d: %w", road, err)
	}
	ep, err := registry.EpochAt(road)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("the epoch registry has no epoch for road %d: %w", road, err)
	}
	if err := proof.Verify(ep); err != nil {
		return nil, [32]byte{}, fmt.Errorf("verify the state sync proof of road %d against epoch %d: %w", road, ep.EpochIndex(), err)
	}
	appHash := proof.AppQC().Proposal().AppHash()
	if len(appHash) != 32 {
		return nil, [32]byte{}, fmt.Errorf("the AppQC of road %d signs an app hash of %d bytes, want 32", road, len(appHash))
	}
	return proof, [32]byte(appHash), nil
}
