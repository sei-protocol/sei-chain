package core

import (
	"context"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

// LagStatus returns Tendermint lag status, if lag is over a certain threshold
func (env *Environment) LagStatus(ctx context.Context) (*coretypes.ResultLagStatus, error) {
	currentHeight := env.BlockStore.Height()
	maxPeerBlockHeight := env.BlockSyncReactor.GetMaxPeerBlockHeight()
	lag := int64(0)

	// Calculate lag
	if maxPeerBlockHeight > currentHeight {
		lag = maxPeerBlockHeight - currentHeight
	}

	result := &coretypes.ResultLagStatus{
		CurrentHeight: currentHeight,
		MaxPeerHeight: maxPeerBlockHeight,
		Lag:           lag,
	}

	// Return a response with error code to differentiate the lagging status by http response code
	if lag > env.Config.LagThreshold {
		return result, coretypes.ErrLagIsTooHigh
	}

	return result, nil
}
