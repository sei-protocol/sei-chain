package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"maps"

	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// NetInfo returns network info.
// More: https://docs.tendermint.com/master/rpc/#/Info/net_info
func (env *Environment) NetInfo(ctx context.Context) (*coretypes.ResultNetInfo, error) {
	peers := map[types.NodeID]coretypes.Peer{}
	for _, addr := range env.PeerManager.AllAddrs() {
		if _,ok := peers[addr.NodeID]; ok { continue }
		peers[addr.NodeID] = coretypes.Peer{ID:  addr.NodeID, URL: addr.String()}	
	}
	peerConnections := map[types.NodeID]coretypes.PeerConnection{}
	for _, info := range env.PeerManager.ConnInfos() {
		if _,ok := peerConnections[info.ID]; ok { continue }
		peerConnections[info.ID] = coretypes.PeerConnection{
			ID:    info.ID,
			State: "ready,connected",
			Score: 100,
		}
	}

	return &coretypes.ResultNetInfo{
		Listening:       env.IsListening,
		Listeners:       env.Listeners,
		NPeers:          len(peers),
		Peers:           slices.Collect(maps.Values(peers)),
		PeerConnections: slices.Collect(maps.Values(peerConnections)),
	}, nil
}

// Genesis returns genesis file.
// More: https://docs.tendermint.com/master/rpc/#/Info/genesis
func (env *Environment) Genesis(ctx context.Context) (*coretypes.ResultGenesis, error) {
	if len(env.genChunks) > 1 {
		return nil, errors.New("genesis response is large, please use the genesis_chunked API instead")
	}

	return &coretypes.ResultGenesis{Genesis: env.GenDoc}, nil
}

func (env *Environment) GenesisChunked(ctx context.Context, req *coretypes.RequestGenesisChunked) (*coretypes.ResultGenesisChunk, error) {
	if env.genChunks == nil {
		return nil, fmt.Errorf("service configuration error, genesis chunks are not initialized")
	}

	if len(env.genChunks) == 0 {
		return nil, fmt.Errorf("service configuration error, there are no chunks")
	}

	id := int(req.Chunk)

	if id > len(env.genChunks)-1 {
		return nil, fmt.Errorf("there are %d chunks, %d is invalid", len(env.genChunks)-1, id)
	}

	return &coretypes.ResultGenesisChunk{
		TotalChunks: len(env.genChunks),
		ChunkNumber: id,
		Data:        env.genChunks[id],
	}, nil
}
