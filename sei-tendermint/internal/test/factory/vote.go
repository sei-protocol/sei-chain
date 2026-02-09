package factory

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func MakeVote(
	ctx context.Context,
	val types.PrivValidator,
	chainID string,
	valIndex int32,
	height int64,
	round int32,
	step int,
	blockID types.BlockID,
	time time.Time,
) (*types.Vote, error) {
	pubKey, err := val.GetPubKey(ctx)
	if err != nil {
		return nil, err
	}

	v := &types.Vote{
		ValidatorAddress: pubKey.Address(),
		ValidatorIndex:   valIndex,
		Height:           height,
		Round:            round,
		Type:             tmproto.SignedMsgType(step), //nolint:gosec // step is a small enum value (prevote/precommit/commit); no overflow risk
		BlockID:          blockID,
		Timestamp:        time,
	}

	vpb := v.ToProto()
	if err := val.SignVote(ctx, chainID, vpb); err != nil {
		return nil, err
	}
	sig, err := crypto.SigFromBytes(vpb.Signature)
	if err != nil {
		return nil, fmt.Errorf("crypto.SigFromBytes(): %w", err)
	}
	v.Signature = utils.Some(sig)
	return v, nil
}
