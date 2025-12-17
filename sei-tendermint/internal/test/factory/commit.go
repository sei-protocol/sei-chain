package factory

import (
	"context"
	"fmt"
	"time"

	"github.com/tendermint/tendermint/crypto"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
)

func MakeCommit(ctx context.Context, blockID types.BlockID, height int64, round int32, voteSet *types.VoteSet, validators []types.PrivValidator, now time.Time) (*types.Commit, error) {
	// all sign
	for i := range validators {
		pubKey, err := validators[i].GetPubKey(ctx)
		if err != nil {
			return nil, err
		}
		vote := &types.Vote{
			ValidatorAddress: pubKey.Address(),
			ValidatorIndex:   int32(i),
			Height:           height,
			Round:            round,
			Type:             tmproto.PrecommitType,
			BlockID:          blockID,
			Timestamp:        now,
		}

		v := vote.ToProto()

		if err := validators[i].SignVote(ctx, voteSet.ChainID(), v); err != nil {
			return nil, err
		}
		sig, err := crypto.SigFromBytes(v.Signature)
		if err != nil {
			return nil, fmt.Errorf("crypto.SigFromBytes(): %w", err)
		}
		vote.Signature = sig
		if _, err := voteSet.AddVote(vote); err != nil {
			return nil, err
		}
	}

	return voteSet.MakeCommit(), nil
}
