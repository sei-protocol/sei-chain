package blocksync

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/flowrate"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/seilog"
)

/*
eg, L = latency = 0.1s
	P = num peers = 10
	FN = num full nodes
	BS = 1kB block size
	CB = 1 Mbit/s = 128 kB/s
	CB/P = 12.8 kB
	B/S = CB/P/BS = 12.8 blocks/s

	12.8 * 0.1 = 1.28 blocks on conn
*/

var logger = seilog.NewLogger("tendermint", "internal", "blocksync")

const (
	requestInterval           = 100 * time.Millisecond
	maxTotalRequesters        = 50
	maxPeerErrBuffer          = 1000
	maxPendingRequests        = maxTotalRequesters
	maxPendingRequestsPerPeer = 20

	// Minimum recv rate to ensure we're receiving blocks from a peer fast
	// enough. If a peer is not sending us data at at least that rate, we
	// consider them to have timedout and we disconnect.
	//
	// Assuming a DSL connection (not a good choice) 128 Kbps (upload) ~ 15 KB/s,
	// sending data across atlantic ~ 7.5 KB/s.
	minRecvRate = 7680

	// Maximum difference between current and new block's height.
	maxDiffBetweenCurrentAndReceivedBlockHeight = 100

	peerTimeout = 2 * time.Second
)

// Interface abstracting p2p.Router for tests.
type router interface {
	IsBlockSyncPeer(types.NodeID) bool
	Evict(id types.NodeID, err error)
	Connected(types.NodeID) bool
}

/*
	Peers self report their heights when we join the block pool.
	Starting from our latest pool.height, we request blocks
	in sequence from peers that reported higher heights than ours.
	Every so often we ask peers what height they're on so we can keep going.

	Requests are continuously made for blocks of higher heights until
	the limit is reached. If most of the requests have no available peers, and we
	are not at peer limits, we can probably switch to consensus reactor
*/

// BlockRequest stores a block request identified by the block Height and the
// PeerID responsible for delivering the block.
type BlockRequest struct {
	Height int64
	PeerID types.NodeID
}

// BlockPool keeps track of the block sync peers, block requests and block responses.
type BlockPool struct {
	lastAdvance time.Time

	mtx sync.RWMutex
	// block requests
	requesters map[int64]*bpRequester
	height     int64 // the lowest key in requesters.
	// peers
	peers         map[types.NodeID]*bpPeer
	router        router
	maxPeerHeight int64 // the biggest reported height among current peers

	// atomic
	numPending atomic.Int32 // number of requests pending assignment or block response

	requestsCh chan BlockRequest
	errorsCh   chan peerError
	reportErr  func(peerError)

	startHeight               int64
	lastHundredBlockTimeStamp time.Time
	lastSyncRate              float64
}

// NewBlockPool returns a new BlockPool with the height equal to start. Block
// requests and peer errors are published on the pool-owned request and error
// channels exposed via Requests and Errors.
func NewBlockPool(start int64, router router) *BlockPool {
	return newBlockPool(start, router, nil)
}

func newBlockPool(start int64, router router, reportErr func(peerError)) *BlockPool {
	bp := &BlockPool{
		peers:        make(map[types.NodeID]*bpPeer),
		requesters:   make(map[int64]*bpRequester),
		height:       start,
		startHeight:  start,
		requestsCh:   make(chan BlockRequest, maxTotalRequesters),
		errorsCh:     make(chan peerError, maxPeerErrBuffer), // NOTE: capacity should exceed peer count.
		lastSyncRate: 0,
		router:       router,
		reportErr:    reportErr,
	}
	if bp.reportErr == nil {
		bp.reportErr = func(pe peerError) {
			select {
			case bp.errorsCh <- pe:
			default:
			}
		}
	}
	return bp
}

// run owns the pool's requester-management loop and its cleanup. Starting the
// task marks the pool active; exiting the task stops all outstanding requester
// work and marks the pool inactive.
func (pool *BlockPool) run(ctx context.Context) error {
	pool.lastAdvance = time.Now()
	pool.lastHundredBlockTimeStamp = pool.lastAdvance

	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for ctx.Err() == nil {
			if r, ok := pool.makeNextRequester(); ok {
				s.Spawn(func() error { return r.run(ctx) })
				continue
			}
			if err := utils.Sleep(ctx, requestInterval); err != nil {
				return err
			}
			pool.removeTimedoutPeers()
		}
		return ctx.Err()
	})
}

func (pool *BlockPool) Requests() <-chan BlockRequest {
	return pool.requestsCh
}

func (pool *BlockPool) Errors() <-chan peerError {
	return pool.errorsCh
}

func (pool *BlockPool) removeTimedoutPeers() {
	var errsToSend []peerError
	defer func() {
		for _, pe := range errsToSend {
			pool.sendError(pe.err, pe.peerID)
		}
	}()
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	for _, peer := range pool.peers {
		// check if peer timed out
		if !peer.didTimeout && peer.numPending > 0 {
			curRate := peer.recvMonitor.CurrentTransferRate()
			// curRate can be 0 on start
			if curRate != 0 && curRate < minRecvRate {
				err := errors.New("peer is not sending us data fast enough")
				errsToSend = append(errsToSend, peerError{err, peer.id})
				logger.Error("SendTimeout", "peer", peer.id,
					"reason", err,
					"curRate-kbps", curRate/1024,
					"minRate-kbps", minRecvRate/1024)
				peer.didTimeout = true
			}
		}

		if peer.didTimeout {
			pool.removePeer(peer.id, true)
		}
	}
}

// GetStatus returns pool's height, numPending requests and the number of
// requesters.
func (pool *BlockPool) GetStatus() (height int64, numPending int32, lenRequesters int) {
	pool.mtx.RLock()
	defer pool.mtx.RUnlock()

	return pool.height, pool.numPending.Load(), len(pool.requesters)
}

// IsCaughtUp returns true if this node is caught up, false - otherwise.
func (pool *BlockPool) IsCaughtUp() bool {
	pool.mtx.RLock()
	defer pool.mtx.RUnlock()

	// Need at least 2 peers to be considered caught up.
	if len(pool.peers) <= 1 {
		return false
	}

	// To sync block H we require block H+1 to verify the LastCommit, so we are
	// caught up once we have reached the current maximum peer height minus one.
	return pool.height >= (pool.maxPeerHeight - 1)
}

// PeekTwoBlocks returns blocks at pool.height and pool.height+1. We need to
// see the second block's Commit to validate the first block. So we peek two
// blocks at a time. We return an extended commit, containing vote extensions
// and their associated signatures, as this is critical to consensus in ABCI++
// as we switch from block sync to consensus mode.
//
// The caller will verify the commit.
func (pool *BlockPool) PeekTwoBlocks() (first, second *types.Block) {
	pool.mtx.RLock()
	defer pool.mtx.RUnlock()

	if r, ok := pool.requesters[pool.height]; ok {
		first = r.getBlock().Or(nil)
	}
	if r, ok := pool.requesters[pool.height+1]; ok {
		second = r.getBlock().Or(nil)
	}
	return
}

// PopRequest pops the first block at pool.height.
// It must have been validated by the second Commit from PeekTwoBlocks.
func (pool *BlockPool) PopRequest() {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	if r, ok := pool.requesters[pool.height]; ok {
		for inner, ctrl := range r.inner.Lock() {
			inner.done = true
			ctrl.Updated()
		}
		delete(pool.requesters, pool.height)
		pool.height++
		pool.lastAdvance = time.Now()

		// the lastSyncRate will be updated every 100 blocks, it uses the adaptive filter
		// to smooth the block sync rate and the unit represents the number of blocks per second.
		if (pool.height-pool.startHeight)%100 == 0 {
			newSyncRate := 100 / time.Since(pool.lastHundredBlockTimeStamp).Seconds()
			if pool.lastSyncRate == 0 {
				pool.lastSyncRate = newSyncRate
			} else {
				pool.lastSyncRate = 0.9*pool.lastSyncRate + 0.1*newSyncRate
			}
			pool.lastHundredBlockTimeStamp = time.Now()
		}

	} else {
		panic(fmt.Sprintf("Expected requester to pop, got nothing at height %v", pool.height))
	}
}

// RedoRequest invalidates the block at pool.height,
// Remove the peer and redo request from others.
// Returns the ID of the removed peer.
func (pool *BlockPool) RedoRequest(height int64) utils.Option[types.NodeID] {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	request := pool.requesters[height]
	peerID := request.getPeerID()
	if id, ok := peerID.Get(); ok {
		pool.removePeer(id, false)
		// Redo all requesters associated with this peer.
		for _, r := range pool.requesters {
			r.reset(id, true)
		}
	}
	return peerID
}

// AddBlock validates that the block comes from the peer it was expected from
// and calls the requester to store it.
//
// This requires an extended commit at the same height as the supplied block -
// the block contains the last commit, but we need the latest commit in case we
// need to switch over from block sync to consensus at this height. If the
// height of the extended commit and the height of the block do not match, we
// do not add the block and return an error.
// TODO: ensure that blocks come in order for each peer.
func (pool *BlockPool) AddBlock(peerID types.NodeID, block *types.Block, blockSize int) error {
	var (
		pendingErr    error
		pendingPeerID types.NodeID
	)
	defer func() {
		if pendingErr != nil {
			pool.sendError(pendingErr, pendingPeerID)
		}
	}()
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	requester := pool.requesters[block.Height]
	if requester == nil {
		diff := pool.height - block.Height
		if diff < 0 {
			diff *= -1
		}
		if diff > maxDiffBetweenCurrentAndReceivedBlockHeight {
			pendingErr = errors.New("peer sent us a block we didn't expect with a height too far ahead/behind")
			pendingPeerID = peerID
		}
		return fmt.Errorf("peer sent us a block we didn't expect (peer: %s, current height: %d, block height: %d)", peerID, pool.height, block.Height)
	}

	setBlockResult := requester.setBlock(block, peerID)
	if setBlockResult == 0 {
		pool.numPending.Add(-1)
		peer := pool.peers[peerID]
		if peer != nil {
			peer.decrPending(blockSize)
		}
		return nil
	}

	pendingErr = errors.New("requester is different or block already exists")
	pendingPeerID = peerID
	return fmt.Errorf("%w (peer: %s, requester: %v, block height: %d)", pendingErr, peerID, requester.getPeerID(), block.Height)
}

// MaxPeerHeight returns the highest reported height.
func (pool *BlockPool) MaxPeerHeight() int64 {
	pool.mtx.RLock()
	defer pool.mtx.RUnlock()
	return pool.maxPeerHeight
}

// LastAdvance returns the time when the last block was processed (or start
// time if no blocks were processed).
func (pool *BlockPool) LastAdvance() time.Time {
	pool.mtx.RLock()
	defer pool.mtx.RUnlock()
	return pool.lastAdvance
}

// SetPeerRange sets the peer's alleged blockchain base and height.
func (pool *BlockPool) SetPeerRange(peerID types.NodeID, base int64, height int64) {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	if !pool.router.IsBlockSyncPeer(peerID) {
		return
	}

	peer := pool.peers[peerID]
	if peer != nil {
		if base < peer.base || height < peer.height {
			// RemovePeer will redo all requesters associated with this peer.
			pool.removePeer(peerID, true)
			pool.router.Evict(peerID, fmt.Errorf(
				"peer is reporting (base=%v,height=%v), which is lower than previously reported (base=%v,height=%v)",
				base, height, peer.base, peer.height,
			))
			return
		}
		peer.base = base
		peer.height = height
	} else {
		peer = &bpPeer{
			pool:       pool,
			id:         peerID,
			base:       base,
			height:     height,
			numPending: 0,
			startAt:    time.Now(),
		}
		logger.Info("Adding peer to blocksync pool", "peer", peerID)
		pool.peers[peerID] = peer
	}

	if height > pool.maxPeerHeight {
		pool.maxPeerHeight = height
	}
}

// RemovePeer removes the peer with peerID from the pool. If there's no peer
// with peerID, function is a no-op.
func (pool *BlockPool) RemovePeer(peerID types.NodeID) {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()
	pool.removePeer(peerID, true)
}

func (pool *BlockPool) removePeer(peerID types.NodeID, redo bool) {
	if redo {
		for _, requester := range pool.requesters {
			requester.reset(peerID, false)
		}
	}

	peer, ok := pool.peers[peerID]
	if ok {
		if peer.timeout != nil {
			peer.timeout.Stop()
		}

		delete(pool.peers, peerID)

		// Find a new peer with the biggest height and update maxPeerHeight if the
		// peer's height was the biggest.
		if peer.height == pool.maxPeerHeight {
			pool.updateMaxPeerHeight()
		}
	}
}

// If no peers are left, maxPeerHeight is set to 0.
func (pool *BlockPool) updateMaxPeerHeight() {
	var max int64
	for _, peer := range pool.peers {
		if peer.height > max {
			max = peer.height
		}
	}
	pool.maxPeerHeight = max
}

// Pick an available peer with the given height available.
// If no peers are available, returns nil.
func (pool *BlockPool) pickIncrAvailablePeer(height int64) *bpPeer {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	var goodPeers []types.NodeID
	// Remove peers with 0 score and shuffle list
	for nodeId := range pool.peers {
		peer := pool.peers[nodeId]
		if peer.didTimeout {
			pool.removePeer(peer.id, true)
			continue
		}
		if peer.numPending >= maxPendingRequestsPerPeer {
			continue
		}
		if height < peer.base || height > peer.height {
			continue
		}
		// We only want to work with peers that are ready & connected (not dialing)
		if pool.router.Connected(nodeId) {
			goodPeers = append(goodPeers, nodeId)
		}
	}

	// randomly pick one with weak entropy.
	if len(goodPeers) > 0 {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		index := rng.Intn(len(goodPeers))
		if index >= len(goodPeers) {
			index = len(goodPeers) - 1
		}
		peer := pool.peers[goodPeers[index]]
		peer.incrPending()
		return peer
	}
	return nil
}

func (pool *BlockPool) makeNextRequester() (*bpRequester, bool) {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()
	nextHeight := pool.height + pool.requestersLen()
	if pool.requestersLen() >= maxTotalRequesters || pool.numPending.Load() >= maxPendingRequests || nextHeight > pool.maxPeerHeight {
		return nil, false
	}

	r := newBPRequester(pool, nextHeight)

	pool.requesters[nextHeight] = r
	pool.numPending.Add(1)
	return r, true
}

func (pool *BlockPool) requestersLen() int64 {
	return int64(len(pool.requesters))
}

func (pool *BlockPool) sendError(err error, peerID types.NodeID) {
	pool.reportErr(peerError{err, peerID})
}

func (pool *BlockPool) targetSyncBlocks() int64 {
	pool.mtx.RLock()
	defer pool.mtx.RUnlock()

	return pool.maxPeerHeight - pool.startHeight + 1
}

func (pool *BlockPool) getLastSyncRate() float64 {
	pool.mtx.RLock()
	defer pool.mtx.RUnlock()

	return pool.lastSyncRate
}

//-------------------------------------

type bpPeer struct {
	didTimeout  bool
	numPending  int32
	height      int64
	base        int64
	pool        *BlockPool
	id          types.NodeID
	recvMonitor *flowrate.Monitor

	timeout *time.Timer
	startAt time.Time
}

func (peer *bpPeer) resetMonitor() {
	peer.recvMonitor = flowrate.New(peer.startAt, time.Second, time.Second*40)
	initialValue := float64(minRecvRate) * math.E
	peer.recvMonitor.SetREMA(initialValue)
}

func (peer *bpPeer) resetTimeout() {
	if peer.timeout == nil {
		peer.timeout = time.AfterFunc(peerTimeout, peer.onTimeout)
	} else {
		peer.timeout.Stop()
		peer.timeout.Reset(peerTimeout)
	}
}

func (peer *bpPeer) incrPending() {
	if peer.numPending == 0 {
		peer.resetMonitor()
		peer.resetTimeout()
	}
	peer.numPending++
}

func (peer *bpPeer) decrPending(recvSize int) {
	peer.numPending--
	if peer.numPending == 0 {
		peer.timeout.Stop()
	} else {
		peer.recvMonitor.Update(recvSize)
		peer.resetTimeout()
	}
}

func (peer *bpPeer) onTimeout() {
	peer.pool.mtx.Lock()
	peer.didTimeout = true
	peer.pool.mtx.Unlock()

	err := errors.New("peer did not send us anything")
	logger.Error("SendTimeout", "id", peer.id, "reason", err, "timeout", peerTimeout)
	peer.pool.sendError(err, peer.id)
}

//-------------------------------------

type bpRequesterInner struct {
	peerID utils.Option[types.NodeID]
	block  utils.Option[*types.Block]
	done   bool
}

type bpRequester struct {
	pool   *BlockPool
	height int64
	inner  utils.Watch[*bpRequesterInner]
}

func newBPRequester(pool *BlockPool, height int64) *bpRequester {
	return &bpRequester{
		pool:   pool,
		height: height,
		inner:  utils.NewWatch(&bpRequesterInner{}),
	}
}

// Responsible for making more requests as necessary
// Returns only when a block is found (e.g. AddBlock() is called)
func (bpr *bpRequester) run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Wait until reset.
		for inner, ctrl := range bpr.inner.Lock() {
			for {
				if inner.done {
					return nil
				}
				if !inner.peerID.IsPresent() {
					break
				}
				if err := ctrl.Wait(ctx); err != nil {
					return err
				}
			}
		}
		// Pick a peer to send request to.
		var peer *bpPeer
		for {
			if peer = bpr.pool.pickIncrAvailablePeer(bpr.height); peer != nil {
				break
			}
			if err := utils.Sleep(ctx, requestInterval); err != nil {
				return err
			}
		}
		for inner := range bpr.inner.Lock() {
			inner.peerID = utils.Some(peer.id)
		}
		// Send request.
		if err := utils.Send(ctx, bpr.pool.requestsCh, BlockRequest{bpr.height, peer.id}); err != nil {
			return err
		}
		// Wait for response with timeout
		if err := utils.WithTimeout(ctx, peerTimeout, func(ctx context.Context) error {
			for inner, ctrl := range bpr.inner.Lock() {
				return ctrl.WaitUntil(ctx, func() bool { return inner.block.IsPresent() })
			}
			panic("unreachable")
		}); err != nil {
			bpr.reset(peer.id, false)
		}
	}
}

// Returns 0 if block doesn't already exist.
// Returns -1 if peer doesn't match.
// Return 1 if block exist and peer matches.
func (bpr *bpRequester) setBlock(block *types.Block, peerID types.NodeID) int {
	for inner, ctrl := range bpr.inner.Lock() {
		if inner.peerID != utils.Some(peerID) {
			return -1
		}
		if inner.block.IsPresent() {
			return 1
		}
		inner.block = utils.Some(block)
		ctrl.Updated()
	}
	return 0
}

func (bpr *bpRequester) getBlock() utils.Option[*types.Block] {
	for inner := range bpr.inner.Lock() {
		return inner.block
	}
	panic("unreachable")
}

func (bpr *bpRequester) getPeerID() utils.Option[types.NodeID] {
	for inner := range bpr.inner.Lock() {
		return inner.peerID
	}
	panic("unreachable")
}

func (bpr *bpRequester) reset(peerID types.NodeID, force bool) bool {
	for inner, ctrl := range bpr.inner.Lock() {
		if inner.peerID != utils.Some(peerID) || (inner.block.IsPresent() && !force) {
			return false
		}
		if inner.block.IsPresent() {
			bpr.pool.numPending.Add(1)
		}
		inner.peerID = utils.None[types.NodeID]()
		inner.block = utils.None[*types.Block]()
		ctrl.Updated()
	}
	return true
}
