package blocksync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/p2p"
	dbm "github.com/tendermint/tm-db"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

func init() {
	peerTimeout = 2 * time.Second
}

type testPeer struct {
	id        types.NodeID
	base      int64
	height    int64
	inputChan chan inputData // make sure each peer's data is sequential
	score     p2p.PeerScore
}

type inputData struct {
	t       *testing.T
	pool    *BlockPool
	request BlockRequest
}

func (p testPeer) runInputRoutine() {
	go func() {
		for input := range p.inputChan {
			p.simulateInput(input)
		}
	}()
}

// Request desired, pretend like we got the block immediately.
func (p testPeer) simulateInput(input inputData) {
	block := &types.Block{Header: types.Header{Height: input.request.Height}}
	extCommit := &types.ExtendedCommit{
		Height: input.request.Height,
	}
	_ = input.pool.AddBlock(input.request.PeerID, block, extCommit, 123)
	// TODO: uncommenting this creates a race which is detected by:
	// https://github.com/golang/go/blob/2bd767b1022dd3254bcec469f0ee164024726486/src/testing/testing.go#L854-L856
	// see: https://github.com/tendermint/tendermint/issues/3390#issue-418379890
	// input.t.Logf("Added block from peer %v (height: %v)", input.request.PeerID, input.request.Height)
}

type testPeers map[types.NodeID]testPeer

func (ps testPeers) start() {
	for _, v := range ps {
		v.runInputRoutine()
	}
}

func (ps testPeers) stop() {
	for _, v := range ps {
		close(v.inputChan)
	}
}

func makePeers(numPeers int, minHeight, maxHeight int64) testPeers {
	peers := make(testPeers, numPeers)
	for i := 0; i < numPeers; i++ {
		bytes := make([]byte, 20)
		if _, err := rand.Read(bytes); err != nil {
			panic(err)
		}
		peerID := types.NodeID(hex.EncodeToString(bytes))
		peers[peerID] = testPeer{peerID, minHeight, maxHeight, make(chan inputData, 10), 1}
	}
	return peers
}

func makePeerManager(peers map[types.NodeID]testPeer) *p2p.PeerManager {
	selfKey := ed25519.GenPrivKeyFromSecret([]byte{0xf9, 0x1b, 0x08, 0xaa, 0x38, 0xee, 0x34, 0xdd})
	selfID := types.NodeIDFromPubKey(selfKey.PubKey())
	peerScores := make(map[types.NodeID]p2p.PeerScore)
	for nodeId, peer := range peers {
		peerScores[nodeId] = peer.score
	}
	peerManager, _ := p2p.NewPeerManager(log.NewNopLogger(), selfID, dbm.NewMemDB(), p2p.PeerManagerOptions{
		PeerScores:          peerScores,
		MaxConnected:        1,
		MaxConnectedUpgrade: 2,
	}, p2p.NopMetrics())
	for nodeId, _ := range peers {
		_, err := peerManager.Add(p2p.NodeAddress{Protocol: "memory", NodeID: nodeId})
		peerManager.MarkReadyConnected(nodeId)
		if err != nil {
			panic(err)
		}
	}

	return peerManager
}
func TestBlockPoolBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := int64(42)
	peers := makePeers(10, start, 1000)
	errorsCh := make(chan peerError, 1000)
	requestsCh := make(chan BlockRequest, 1000)
	pool := NewBlockPool(log.NewNopLogger(), start, requestsCh, errorsCh, makePeerManager(peers))

	if err := pool.Start(ctx); err != nil {
		t.Error(err)
	}

	t.Cleanup(func() { cancel(); pool.Wait() })

	peers.start()
	defer peers.stop()

	// Introduce each peer.
	go func() {
		for _, peer := range peers {
			pool.SetPeerRange(peer.id, peer.base, peer.height)
		}
	}()

	// Start a goroutine to pull blocks
	go func() {
		for {
			if !pool.IsRunning() {
				return
			}
			first, second, _ := pool.PeekTwoBlocks()
			if first != nil && second != nil {
				pool.PopRequest()
			} else {
				time.Sleep(1 * time.Second)
			}
		}
	}()

	// Pull from channels
	for {
		select {
		case err := <-errorsCh:
			t.Error(err)
		case request := <-requestsCh:
			if request.Height == 300 {
				return // Done!
			}

			peers[request.PeerID].inputChan <- inputData{t, pool, request}
		}
	}
}

func TestBlockPoolTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := int64(42)
	peers := makePeers(10, start, 1000)
	errorsCh := make(chan peerError, 1000)
	requestsCh := make(chan BlockRequest, 1000)
	pool := NewBlockPool(log.NewNopLogger(), start, requestsCh, errorsCh, makePeerManager(peers))
	err := pool.Start(ctx)
	if err != nil {
		t.Error(err)
	}
	t.Cleanup(func() {
		cancel()
	})

	// Introduce each peer.
	go func() {
		for _, peer := range peers {
			pool.SetPeerRange(peer.id, peer.base, peer.height)
		}
	}()

	// Start a goroutine to pull blocks
	go func() {
		for {
			if !pool.IsRunning() {
				return
			}
			first, second, _ := pool.PeekTwoBlocks()
			if first != nil && second != nil {
				pool.PopRequest()
			} else {
				time.Sleep(1 * time.Second)
			}
		}
	}()

	// Pull from channels
	for {
		select {
		case <-errorsCh:
			return
		// consider error to be always timeout here
		default:
		}
	}
}

func TestBlockPoolRemovePeer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	peers := make(testPeers, 10)
	for i := 0; i < 10; i++ {
		var peerID types.NodeID
		if i+1 == 10 {
			peerID = types.NodeID(strings.Repeat(fmt.Sprintf("%d", i+1), 20))
		} else {
			peerID = types.NodeID(strings.Repeat(fmt.Sprintf("%d", i+1), 40))
		}
		height := int64(i + 1)
		peers[peerID] = testPeer{peerID, 0, height, make(chan inputData), 1}
	}
	requestsCh := make(chan BlockRequest)
	errorsCh := make(chan peerError)

	pool := NewBlockPool(log.NewNopLogger(), 1, requestsCh, errorsCh, makePeerManager(peers))
	err := pool.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { cancel(); pool.Wait() })

	// add peers
	for peerID, peer := range peers {
		pool.SetPeerRange(peerID, peer.base, peer.height)
	}
	assert.EqualValues(t, 10, pool.MaxPeerHeight())

	// remove not-existing peer
	assert.NotPanics(t, func() { pool.RemovePeer(types.NodeID("Superman")) })

	// remove peer with biggest height
	pool.RemovePeer(types.NodeID(strings.Repeat("10", 20)))
	assert.EqualValues(t, 9, pool.MaxPeerHeight())

	// remove all peers
	for peerID := range peers {
		pool.RemovePeer(peerID)
	}

	assert.EqualValues(t, 0, pool.MaxPeerHeight())
}

func TestSortedPeers(t *testing.T) {
	peers := make(testPeers, 10)
	peerIdA := types.NodeID(strings.Repeat("a", 40))
	peerIdB := types.NodeID(strings.Repeat("b", 40))
	peerIdC := types.NodeID(strings.Repeat("c", 40))

	peers[peerIdA] = testPeer{peerIdA, 0, 1, make(chan inputData), 11}
	peers[peerIdB] = testPeer{peerIdA, 0, 1, make(chan inputData), 10}
	peers[peerIdC] = testPeer{peerIdA, 0, 1, make(chan inputData), 13}

	requestsCh := make(chan BlockRequest)
	errorsCh := make(chan peerError)
	pool := NewBlockPool(log.NewNopLogger(), 1, requestsCh, errorsCh, makePeerManager(peers))
	// add peers
	for peerID, peer := range peers {
		pool.SetPeerRange(peerID, peer.base, peer.height)
	}
	// Peers should be sorted by score via peerManager
	assert.Equal(t, []types.NodeID{peerIdC, peerIdA, peerIdB}, pool.getSortedPeers(pool.peers))
}
