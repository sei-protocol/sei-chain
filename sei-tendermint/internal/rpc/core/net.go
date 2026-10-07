package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

// NetInfo returns network info. NPeers and PeerConnections cover every
// connected peer; Peers lists the connected peers with a known address.
// More: https://docs.tendermint.com/master/rpc/#/Info/net_info
func (env *Environment) NetInfo(ctx context.Context) (*coretypes.ResultNetInfo, error) {
	infos := env.Router.ConnInfos()
	peers := make([]coretypes.Peer, 0, len(infos))
	peerConnections := make([]coretypes.PeerConnection, 0, len(infos))
	for _, info := range infos {
		peerConnections = append(peerConnections, coretypes.PeerConnection{
			ID:    info.ID,
			State: "ready,connected",
			Score: 100,
		})
		addr, ok := info.DialedAddr.Get()
		if !ok {
			addr, ok = info.SelfDeclaredAddr.Get()
		}
		if ok {
			peers = append(peers, coretypes.Peer{ID: info.ID, URL: addr.String()})
		}
	}

	return &coretypes.ResultNetInfo{
		Listening:       env.IsListening,
		Listeners:       env.Listeners,
		NPeers:          len(peerConnections),
		Peers:           peers,
		PeerConnections: peerConnections,
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
