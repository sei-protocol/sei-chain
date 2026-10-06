//go:build mock_chain_validation

package statesync

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
)

func TestReactor_RefusesSnapshotAndChunkRequests(t *testing.T) {
	ctx := t.Context()

	var appQueried atomic.Bool
	conn := newTestStatesyncApp()
	conn.listSnapshots.Set(func(context.Context, *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
		appQueried.Store(true)
		return &abci.ResponseListSnapshots{Snapshots: []*abci.Snapshot{{Height: 1, Format: 1, Chunks: 1, Hash: []byte{1}}}}, nil
	})
	conn.loadSnapshotChunk.Set(func(context.Context, *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
		appQueried.Store(true)
		return &abci.ResponseLoadSnapshotChunk{Chunk: []byte{1, 2, 3}}, nil
	})

	rts := setup(t, conn, nil, false)
	n := utils.OrPanic1(rts.AddPeer(ctx, t))
	n.snapshotCh.Broadcast(wrap(&pb.SnapshotsRequest{}))
	n.chunkCh.Broadcast(wrap(&pb.ChunkRequest{Height: 1, Format: 1, Index: 0}))

	recvCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if m, err := n.snapshotCh.Recv(recvCtx); err == nil {
		t.Fatalf("reactor answered a snapshots request: %v", m.Message)
	}
	if m, err := n.chunkCh.Recv(recvCtx); err == nil {
		t.Fatalf("reactor answered a chunk request: %v", m.Message)
	}
	if appQueried.Load() {
		t.Fatal("reactor read snapshots from the app")
	}
}
