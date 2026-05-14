package blocksync

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/stretchr/testify/assert"
)

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

type fakeRouter struct {
	peers  map[types.NodeID]testPeer
	errors map[types.NodeID]error
}

func (r *fakeRouter) IsBlockSyncPeer(id types.NodeID) bool {
	_, ok := r.peers[id]
	return ok
}

func (r *fakeRouter) Evict(id types.NodeID, err error) {
	r.errors[id] = err
}

func (r *fakeRouter) Connected(id types.NodeID) bool {
	if _, ok := r.errors[id]; ok {
		return false
	}
	_, ok := r.peers[id]
	return ok
}

func makeRouter(peers map[types.NodeID]testPeer) *fakeRouter {
	return &fakeRouter{
		peers:  peers,
		errors: map[types.NodeID]error{},
	}
}

func TestBlockPoolBasic(t *testing.T) {
	ctx := t.Context()

	start := int64(42)
	peers := makePeers(10, start, 1000)
	errorsCh := make(chan peerError, 1000)
	requestsCh := make(chan BlockRequest, 1000)
	pool := NewBlockPool(start, requestsCh, errorsCh, makeRouter(peers))

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
	pool := NewBlockPool(start, requestsCh, errorsCh, makeRouter(peers))
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
	<-errorsCh
}

func TestBlockPoolRemovePeer(t *testing.T) {
	ctx := t.Context()

	peers := make(testPeers, 10)
	for i := range 10 {
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

	pool := NewBlockPool(1, requestsCh, errorsCh, makeRouter(peers))
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

	pool := NewBlockPool(1, requestsCh, errorsCh, makeRouter(peers))
	// add peers
	for peerID, peer := range peers {
		pool.SetPeerRange(peerID, peer.base, peer.height)
	}
	require.Equal(t, int64(math.MaxInt64), pool.maxPeerHeight)
	// now the bad peer withdraws its malicious height
	pool.SetPeerRange(badNodeId, 1, initialHeight)
	require.Equal(t, int64(initialHeight), pool.maxPeerHeight)
}

func TestBlockPoolRejectsWrongPeerWithoutDiscardingGoodBlock(t *testing.T) {
	t.Run("good then bad", func(t *testing.T) {
		testBlockPoolRejectsWrongPeerWithoutDiscardingGoodBlock(t, true)
	})

	t.Run("bad then good", func(t *testing.T) {
		testBlockPoolRejectsWrongPeerWithoutDiscardingGoodBlock(t, false)
	})
}

func testBlockPoolRejectsWrongPeerWithoutDiscardingGoodBlock(t *testing.T, goodFirst bool) {
	ctx := t.Context()

	goodPeerID := types.NodeID(strings.Repeat("a", 40))
	badPeerID := types.NodeID(strings.Repeat("b", 40))
	peers := testPeers{
		goodPeerID: {goodPeerID, 1, 2, make(chan inputData)},
	}
	requestsCh := make(chan BlockRequest, 2)
	errorsCh := make(chan peerError, 2)
	pool := NewBlockPool(1, requestsCh, errorsCh, makeRouter(peers))

	require.NoError(t, pool.Start(ctx))
	t.Cleanup(func() { pool.Wait() })

	pool.SetPeerRange(goodPeerID, 1, 2)

	requests := map[int64]BlockRequest{}
	for range 2 {
		request := <-requestsCh
		requests[request.Height] = request
	}

	firstRequest, ok := requests[1]
	require.True(t, ok)
	secondRequest, ok := requests[2]
	require.True(t, ok)
	require.Equal(t, goodPeerID, firstRequest.PeerID)
	require.Equal(t, goodPeerID, secondRequest.PeerID)

	goodBlock := &types.Block{Header: types.Header{Height: 1}}
	badBlock := &types.Block{Header: types.Header{Height: 1}}

	if goodFirst {
		require.NoError(t, pool.AddBlock(goodPeerID, goodBlock, 123))
		require.Error(t, pool.AddBlock(badPeerID, badBlock, 123))
	} else {
		require.Error(t, pool.AddBlock(badPeerID, badBlock, 123))
		require.NoError(t, pool.AddBlock(goodPeerID, goodBlock, 123))
	}

	secondBlock := &types.Block{Header: types.Header{Height: 2}}
	require.NoError(t, pool.AddBlock(goodPeerID, secondBlock, 123))

	first, second := pool.PeekTwoBlocks()
	require.Equal(t, goodBlock, first)
	require.Equal(t, secondBlock, second)
}

// TestBlockPoolAddBlockReleasesLockBeforeSend asserts that AddBlock does
// not hold pool.mtx across a send on errorsCh.
func TestBlockPoolAddBlockReleasesLockBeforeSend(t *testing.T) {
	ctx := t.Context()

	peerID := types.NodeID(strings.Repeat("a", 40))
	peers := testPeers{peerID: {peerID, 1, 100, make(chan inputData)}}

	errorsCh := make(chan peerError, 1)
	requestsCh := make(chan BlockRequest, 1000)
	pool := NewBlockPool(1, requestsCh, errorsCh, makeRouter(peers))
	require.NoError(t, pool.Start(ctx))
	t.Cleanup(func() { pool.Wait() })

	pool.SetPeerRange(peerID, 1, 100)

	errorsCh <- peerError{errors.New("filler"), peerID}

	farHeight := int64(1 + maxDiffBetweenCurrentAndReceivedBlockHeight + 1000)
	farBlock := &types.Block{Header: types.Header{Height: farHeight}}

	addBlockDone := make(chan struct{})
	go func() {
		_ = pool.AddBlock(peerID, farBlock, 123)
		close(addBlockDone)
	}()

	time.Sleep(200 * time.Millisecond)

	probeDone := make(chan struct{})
	go func() {
		_ = pool.MaxPeerHeight()
		close(probeDone)
	}()

	select {
	case <-probeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("pool.mtx not released before AddBlock sent on errorsCh")
	}

	<-errorsCh

	select {
	case <-addBlockDone:
	case <-time.After(2 * time.Second):
		t.Fatal("AddBlock did not complete after errorsCh was drained")
	}

	select {
	case <-errorsCh:
	default:
	}
}
