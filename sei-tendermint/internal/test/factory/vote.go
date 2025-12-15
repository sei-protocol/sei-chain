package factory

import (
	"context"
	"time"
	"fmt"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
	"github.com/tendermint/tendermint/crypto"
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
		Type:             tmproto.SignedMsgType(step),
		BlockID:          blockID,
		Timestamp:        time,
	}

	vpb := v.ToProto()
	if err := val.SignVote(ctx, chainID, vpb); err != nil {
		return nil, err
	}
	sig,err := crypto.SigFromBytes(vpb.Signature)
	if err!=nil { return nil, fmt.Errorf("crypto.SigFromBytes(): %w",err) }
	v.Signature = sig
	return v, nil
}
