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
	qc, blocks := data.TestCommitQC(rng, epoch, keys, utils.None[*types.CommitQC]())
	proposal := types.NewAppProposal(qc.QC().View(), utils.GenBytes(rng, 32))
	return Encode(types.NewRoadBlock(qc, blocks), data.TestAppQC(keys, proposal))
}
