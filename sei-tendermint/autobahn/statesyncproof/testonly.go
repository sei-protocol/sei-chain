package statesyncproof

import (
	"github.com/sei-protocol/sei-chain/giga/apphash"
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
	record := genAppHashData(rng, height)
	appHash := record.AppHash()
	proposal := types.NewAppProposal(qc.QC().View(), appHash[:])
	return Encode(types.NewRoadBlock(qc, blocks), data.TestAppQC(keys, proposal), record)
}

// genAppHashData returns an app hash record of road height with random hashes.
func genAppHashData(rng utils.Rng, height types.RoadHeight) *apphash.AppHashData {
	hash := func() [32]byte { return [32]byte(utils.GenBytes(rng, 32)) }
	return apphash.NewAppHashData(rng.Uint64(), uint64(height), hash(), hash(), hash(), hash(), hash())
}
