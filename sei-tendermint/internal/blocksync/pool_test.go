package blocksync

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/internal/p2p"
	dbm "github.com/tendermint/tm-db"

	"github.com/stretchr/testify/assert"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
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
	_ = input.pool.AddBlock(input.request.PeerID, block, 123)
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
	for range numPeers {
		bytes := make([]byte, 20)
		if _, err := rand.Read(bytes); err != nil {
			panic(err)
		}
		peerID := types.NodeID(hex.EncodeToString(bytes))
		peers[peerID] = testPeer{peerID, minHeight, maxHeight, make(chan inputData, 10)}
	}
	return peers
}

func makeRouter(peers map[types.NodeID]testPeer) *p2p.Router {
	selfKey := ed25519.GenPrivKeyFromSecret([]byte{0xf9, 0x1b, 0x08, 0xaa, 0x38, 0xee, 0x34, 0xdd})
	router, _ := p2p.NewRouter(log.NewNopLogger(), p2p.NopMetrics(), selfKey, nil/*TODO(gprusak)*/, dbm.NewMemDB(), &p2p.RouterOptions{
		MaxConnected:        utils.Some(1),
	})
	for nodeId := range peers {
		err := router.AddAddrs(utils.Slice(p2p.NodeAddress{NodeID: nodeId, Hostname: "a.com", Port: 1234}))
		if err != nil {
			panic(err)
		}
		//router.MarkReadyConnected(nodeId)
	}

	return router
}

func TestBlockPoolBasic(t *testing.T) {
	ctx := t.Context()

	start := int64(42)
	peers := makePeers(10, start, 1000)
	errorsCh := make(chan peerError, 1000)
	requestsCh := make(chan BlockRequest, 1000)
	pool := NewBlockPool(log.NewNopLogger(), start, requestsCh, errorsCh, makeRouter(peers))

	if err := pool.Start(ctx); err != nil {
		t.Error(err)
	}

	t.Cleanup(func() { pool.Wait() })

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
			first, second := pool.PeekTwoBlocks()
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
	ctx := t.Context()

	start := int64(42)
	peers := makePeers(10, start, 1000)
	errorsCh := make(chan peerError, 1000)
	requestsCh := make(chan BlockRequest, 1000)
	pool := NewBlockPool(log.NewNopLogger(), start, requestsCh, errorsCh, makeRouter(peers))
	err := pool.Start(ctx)
	if err != nil {
		t.Error(err)
	}

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
			first, second := pool.PeekTwoBlocks()
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
	ctx := t.Context()

	peers := make(testPeers, 10)
	for i := 0; i < 10; i++ {
		var peerID types.NodeID
		if i+1 == 10 {
			peerID = types.NodeID(strings.Repeat(fmt.Sprintf("%d", i+1), 20))
		} else {
			peerID = types.NodeID(strings.Repeat(fmt.Sprintf("%d", i+1), 40))
		}
		height := int64(i + 1)
		peers[peerID] = testPeer{peerID, 0, height, make(chan inputData)}
	}
	requestsCh := make(chan BlockRequest)
	errorsCh := make(chan peerError)

	pool := NewBlockPool(log.NewNopLogger(), 1, requestsCh, errorsCh, makeRouter(peers))
	err := pool.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Wait() })

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

	peers[peerIdA] = testPeer{peerIdA, 0, 1, make(chan inputData)}
	peers[peerIdB] = testPeer{peerIdA, 0, 1, make(chan inputData)}
	peers[peerIdC] = testPeer{peerIdA, 0, 1, make(chan inputData)}

	requestsCh := make(chan BlockRequest)
	errorsCh := make(chan peerError)
	pool := NewBlockPool(log.NewNopLogger(), 1, requestsCh, errorsCh, makeRouter(peers))
	// add peers
	for peerID, peer := range peers {
		pool.SetPeerRange(peerID, peer.base, peer.height)
	}
	// Peers should be sorted by score via peerManager
	assert.Equal(t, []types.NodeID{peerIdC, peerIdA, peerIdB}, pool.getSortedPeers(pool.peers))
}

func TestBlockPoolMaliciousNodeMaxInt64(t *testing.T) {
	const initialHeight = 7
	goodNodeId := types.NodeID(strings.Repeat("a", 40))
	badNodeId := types.NodeID(strings.Repeat("b", 40))
	peers := testPeers{
		goodNodeId: testPeer{goodNodeId, 1, initialHeight, make(chan inputData)},
		badNodeId:  testPeer{badNodeId, 1, math.MaxInt64, make(chan inputData)},
	}
	errorsCh := make(chan peerError, 3)
	requestsCh := make(chan BlockRequest)

	pool := NewBlockPool(log.NewNopLogger(), 1, requestsCh, errorsCh, makeRouter(peers))
	// add peers
	for peerID, peer := range peers {
		pool.SetPeerRange(peerID, peer.base, peer.height)
	}
	require.Equal(t, int64(math.MaxInt64), pool.maxPeerHeight)
	// now the bad peer withdraws its malicious height
	pool.SetPeerRange(badNodeId, 1, initialHeight)
	require.Equal(t, int64(initialHeight), pool.maxPeerHeight)
}
