package avail

import (
	"github.com/rs/zerolog/log"

	"github.com/tendermint/tendermint/internal/autobahn/types"
)

type appVotes struct {
	byKey  map[types.PublicKey]*types.Signed[*types.AppVote]
	byHash map[types.Hash[*types.AppVote]][]*types.Signed[*types.AppVote]
}

func newAppVotes() appVotes {
	return appVotes{
		byKey:  map[types.PublicKey]*types.Signed[*types.AppVote]{},
		byHash: map[types.Hash[*types.AppVote]][]*types.Signed[*types.AppVote]{},
	}
}

// Returns qc if a new qc has been reached.
func (av appVotes) pushVote(c *types.Committee, vote *types.Signed[*types.AppVote]) (*types.AppQC, bool) {
	k := vote.Key()
	if _, ok := av.byKey[k]; ok {
		return nil, false
	}
	av.byKey[k] = vote
	av.byHash[vote.Hash()] = append(av.byHash[vote.Hash()], vote)
	if len(av.byKey) == c.AppQuorum() && len(av.byHash) > 1 {
		log.Error().Uint64("n", uint64(vote.Msg().Proposal().GlobalNumber())).Msg("appHash mismatch")
	}
	if vs := av.byHash[vote.Hash()]; len(vs) == c.AppQuorum() {
		return types.NewAppQC(vs), true
	}
	return nil, false
}
