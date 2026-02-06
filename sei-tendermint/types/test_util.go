package types

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func MakeCommit(ctx context.Context, blockID BlockID, height int64, round int32,
	voteSet *VoteSet, validators []PrivValidator, now time.Time) (*Commit, error) {

	// all sign
	for i := 0; i < len(validators); i++ {
		pubKey, err := validators[i].GetPubKey(ctx)
		if err != nil {
			return nil, fmt.Errorf("can't get pubkey: %w", err)
		}
		vote := &Vote{
			ValidatorAddress: pubKey.Address(),
			ValidatorIndex:   int32(i),
			Height:           height,
			Round:            round,
			Type:             tmproto.PrecommitType,
			BlockID:          blockID,
			Timestamp:        now,
		}

		_, err = signAddVote(ctx, validators[i], vote, voteSet)
		if err != nil {
			return nil, err
		}
	}

	return voteSet.MakeCommit(), nil
}

func signAddVote(ctx context.Context, privVal PrivValidator, vote *Vote, voteSet *VoteSet) (signed bool, err error) {
	v := vote.ToProto()
	if err := privVal.SignVote(ctx, voteSet.ChainID(), v); err != nil {
		return false, err
	}
	sig, err := crypto.SigFromBytes(v.Signature)
	if err != nil {
		return false, fmt.Errorf("SigFromBytes(): %w", err)
	}
	vote.Signature = utils.Some(sig)
	return voteSet.AddVote(vote)
}
