package statesyncproof

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// EncodeForTesting returns a state sync proof file of road height, signed by a random committee
// that rng generates.
// Intended for use in tests only.
func EncodeForTesting(rng utils.Rng, height types.RoadHeight) ([]byte, error) {
	committee, keys := types.GenCommittee(rng, 4)
	epoch := types.NewEpoch(types.GenEpochIndex(rng), types.RoadRange{First: height, Next: height + 10}, committee)
	return EncodeForEpochForTesting(rng, epoch, keys, []byte{1, 2, 3})
}

// EncodeForEpochForTesting returns a state sync proof file of the first road of ep whose AppQC
// signs appHash. keys are the keys of the committee of ep, so the proof verifies against ep.
// Intended for use in tests only.
func EncodeForEpochForTesting(rng utils.Rng, ep *types.Epoch, keys []types.SecretKey, appHash types.AppHash) ([]byte, error) {
	qc, blocks := data.TestCommitQC(rng, ep, keys, utils.None[*types.CommitQC]())
	proposal := types.NewAppProposal(qc.QC().View(), appHash)
	return Encode(types.NewRoadBlock(qc, blocks), data.TestAppQC(keys, proposal))
}
