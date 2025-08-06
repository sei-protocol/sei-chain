package blocksync

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tendermint/tendermint/internal/libs/flowrate"
	"github.com/tendermint/tendermint/internal/p2p"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/service"
	"github.com/tendermint/tendermint/types"
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

	// Used to indicate the reason of the redo
	PeerRemoved RetryReason = "PeerRemoved"
	BadBlock    RetryReason = "BadBlock"
)

var peerTimeout = 2 * time.Second // not const so we can override with tests

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
	service.BaseService
	logger log.Logger

	lastAdvance time.Time

	mtx sync.RWMutex
	// block requests
	requesters map[int64]*bpRequester
	height     int64 // the lowest key in requesters.
	// peers
	peers         map[types.NodeID]*bpPeer
	peerManager   *p2p.PeerManager
	maxPeerHeight int64 // the biggest reported height

	// atomic
	numPending int32 // number of requests pending assignment or block response

	requestsCh chan<- BlockRequest
	errorsCh   chan<- peerError

	startHeight               int64
	lastHundredBlockTimeStamp time.Time
	lastSyncRate              float64
	cancels                   []context.CancelFunc
}

// NewBlockPool returns a new BlockPool with the height equal to start. Block
// requests and errors will be sent to requestsCh and errorsCh accordingly.
func NewBlockPool(
	logger log.Logger,
	start int64,
	requestsCh chan<- BlockRequest,
	errorsCh chan<- peerError,
	peerManager *p2p.PeerManager,
) *BlockPool {
	bp := &BlockPool{
		logger:       logger,
		peers:        make(map[types.NodeID]*bpPeer),
		requesters:   make(map[int64]*bpRequester),
		height:       start,
		startHeight:  start,
		numPending:   0,
		requestsCh:   requestsCh,
		errorsCh:     errorsCh,
		lastSyncRate: 0,
		peerManager:  peerManager,
	}
	bp.BaseService = *service.NewBaseService(logger, "BlockPool", bp)
	return bp
}

// OnStart implements service.Service by spawning requesters routine and recording
// pool's start time.
func (pool *BlockPool) OnStart(ctx context.Context) error {
	pool.lastAdvance = time.Now()
	pool.lastHundredBlockTimeStamp = pool.lastAdvance
	go pool.makeRequestersRoutine(ctx)

	return nil
}

func (pool *BlockPool) OnStop() {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	// cancel all running requesters if any
	for _, cancel := range pool.cancels {
		cancel()
	}
	pool.cancels = []context.CancelFunc{}
}

// spawns requesters as needed
func (pool *BlockPool) makeRequestersRoutine(ctx context.Context) {
	for pool.IsRunning() {
		if ctx.Err() != nil {
			return
		}

		_, numPending, lenRequesters := pool.GetStatus()
		if numPending >= maxPendingRequests || lenRequesters >= maxTotalRequesters {
			// This is preferable to using a timer because the request interval
			// is so small. Larger request intervals may necessitate using a
			// timer/ticker.
			time.Sleep(requestInterval)
			pool.removeTimedoutPeers()
			continue
		}

		// request for more blocks.
		pool.makeNextRequester(ctx)
	}
}

func (pool *BlockPool) removeTimedoutPeers() {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	for _, peer := range pool.peers {
		// check if peer timed out
		if !peer.didTimeout && peer.numPending > 0 {
			curRate := peer.recvMonitor.CurrentTransferRate()
			// curRate can be 0 on start
			if curRate != 0 && curRate < minRecvRate {
				err := errors.New("peer is not sending us data fast enough")
				pool.sendError(err, peer.id)
				pool.logger.Error("SendTimeout", "peer", peer.id,
					"reason", err,
					"curRate", fmt.Sprintf("%d KB/s", curRate/1024),
					"minRate", fmt.Sprintf("%d KB/s", minRecvRate/1024))
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

	return pool.height, atomic.LoadInt32(&pool.numPending), len(pool.requesters)
}

// IsCaughtUp returns true if this node is caught up, false - otherwise.
func (pool *BlockPool) IsCaughtUp() bool {
	pool.mtx.RLock()
	defer pool.mtx.RUnlock()

	// Need at least 2 peers to be considered caught up.
	if len(pool.peers) <= 1 {
		return false
	}

	// NOTE: we use maxPeerHeight - 1 because to sync block H requires block H+1
	// to verify the LastCommit.
	return pool.height >= (pool.maxPeerHeight - 1)
}

// PeekTwoBlocks returns blocks at pool.height and pool.height+1. We need to
// see the second block's Commit to validate the first block. So we peek two
// blocks at a time. We return an extended commit, containing vote extensions
// and their associated signatures, as this is critical to consensus in ABCI++
// as we switch from block sync to consensus mode.
//
// The caller will verify the commit.
func (pool *BlockPool) PeekTwoBlocks() (first, second *types.Block, firstExtCommit *types.ExtendedCommit) {
	pool.mtx.RLock()
	defer pool.mtx.RUnlock()

	if r := pool.requesters[pool.height]; r != nil {
		first = r.getBlock()
		firstExtCommit = r.getExtendedCommit()
	}
	if r := pool.requesters[pool.height+1]; r != nil {
		second = r.getBlock()
	}
	return
}

// PopRequest pops the first block at pool.height.
// It must have been validated by the second Commit from PeekTwoBlocks.
// TODO(thane): (?) and its corresponding ExtendedCommit.
func (pool *BlockPool) PopRequest() {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	if r := pool.requesters[pool.height]; r != nil {
		r.Stop()
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
func (pool *BlockPool) RedoRequest(height int64) types.NodeID {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	request := pool.requesters[height]
	peerID := request.getPeerID()
	if peerID != types.NodeID("") {
		pool.removePeer(peerID, false)
	}
	// Redo all requesters associated with this peer.
	for _, requester := range pool.requesters {
		if requester.getPeerID() == peerID {
			requester.redo(peerID, BadBlock)
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
func (pool *BlockPool) AddBlock(peerID types.NodeID, block *types.Block, extCommit *types.ExtendedCommit, blockSize int) error {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	if extCommit != nil && block.Height != extCommit.Height {
		return fmt.Errorf("heights don't match, not adding block (block height: %d, commit height: %d)", block.Height, extCommit.Height)
	}

	requester := pool.requesters[block.Height]
	if requester == nil {
		diff := pool.height - block.Height
		if diff < 0 {
			diff *= -1
		}
		if diff > maxDiffBetweenCurrentAndReceivedBlockHeight {
			pool.sendError(errors.New("peer sent us a block we didn't expect with a height too far ahead/behind"), peerID)
		}
		return fmt.Errorf("peer sent us a block we didn't expect (peer: %s, current height: %d, block height: %d)", peerID, pool.height, block.Height)
	}

	setBlockResult := requester.setBlock(block, extCommit, peerID)
	if setBlockResult == 0 {
		atomic.AddInt32(&pool.numPending, -1)
		peer := pool.peers[peerID]
		if peer != nil {
			peer.decrPending(blockSize)
		}

		// Increment the number of consecutive successful block syncs for the peer
		pool.peerManager.IncrementBlockSyncs(peerID)
	} else {
		err := errors.New("requester is different or block already exists")
		pool.sendError(err, peerID)
		return fmt.Errorf("%w (peer: %s, requester: %s, block height: %d)", err, peerID, requester.getPeerID(), block.Height)
	}

	return nil
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

	blockSyncPeers := pool.peerManager.GetBlockSyncPeers()
	if len(blockSyncPeers) > 0 && !blockSyncPeers[peerID] {
		return
	}

	peer := pool.peers[peerID]
	if peer != nil {
		peer.base = base
		peer.height = height
	} else {
		peer = &bpPeer{
			pool:       pool,
			id:         peerID,
			base:       base,
			height:     height,
			numPending: 0,
			logger:     pool.logger.With("peer", peerID),
			startAt:    time.Now(),
		}
		pool.logger.Info(fmt.Sprintf("Adding peer %s to blocksync pool", peerID))
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
			if requester.getPeerID() == peerID {
				requester.redo(peerID, PeerRemoved)
			}
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

func (pool *BlockPool) getSortedPeers(peers map[types.NodeID]*bpPeer) []types.NodeID {
	// Generate a sorted list
	sortedPeers := make([]types.NodeID, 0, len(peers))

	for peer := range peers {
		sortedPeers = append(sortedPeers, peer)
	}

	// Sort from high to low score
	sort.Slice(sortedPeers, func(i, j int) bool {
		return pool.peerManager.Score(sortedPeers[i]) > pool.peerManager.Score(sortedPeers[j])
	})
	return sortedPeers
}

// Pick an available peer with the given height available.
// If no peers are available, returns nil.
func (pool *BlockPool) pickIncrAvailablePeer(height int64) *bpPeer {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	// Generate a sorted list
	sortedPeers := pool.getSortedPeers(pool.peers)
	var goodPeers []types.NodeID
	// Remove peers with 0 score and shuffle list
	for _, nodeId := range sortedPeers {
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
		if pool.peerManager.State(nodeId) == "ready,connected" {
			goodPeers = append(goodPeers, nodeId)
		}

		// Skip the ones with zero score to avoid connecting to bad peers
		if pool.peerManager.Score(nodeId) <= 0 {
			break
		}
	}

	// randomly pick one
	if len(goodPeers) > 0 {
		rand.Seed(time.Now().UnixNano())
		index := rand.Intn(len(goodPeers))
		if index >= len(goodPeers) {
			index = len(goodPeers) - 1
		}
		peer := pool.peers[goodPeers[index]]
		peer.incrPending()
		return peer
	}
	return nil
}

func (pool *BlockPool) makeNextRequester(ctx context.Context) {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	nextHeight := pool.height + pool.requestersLen()
	if nextHeight > pool.maxPeerHeight {
		return
	}

	request := newBPRequester(pool.logger, pool, nextHeight)

	pool.requesters[nextHeight] = request
	atomic.AddInt32(&pool.numPending, 1)

	ctx, cancel := context.WithCancel(ctx)
	pool.cancels = append(pool.cancels, cancel)
	err := request.Start(ctx)
	if err != nil {
		request.logger.Error("error starting request", "err", err)
	}
}

func (pool *BlockPool) requestersLen() int64 {
	return int64(len(pool.requesters))
}

func (pool *BlockPool) sendRequest(height int64, peerID types.NodeID) {
	if !pool.IsRunning() {
		return
	}
	pool.requestsCh <- BlockRequest{height, peerID}
}

func (pool *BlockPool) sendError(err error, peerID types.NodeID) {
	if !pool.IsRunning() {
		return
	}
	pool.errorsCh <- peerError{err, peerID}
}

// for debugging purposes
//
//nolint:unused
func (pool *BlockPool) debug() string {
	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	str := ""
	nextHeight := pool.height + pool.requestersLen()
	for h := pool.height; h < nextHeight; h++ {
		if pool.requesters[h] == nil {
			str += fmt.Sprintf("H(%v):X ", h)
		} else {
			str += fmt.Sprintf("H(%v):", h)
			str += fmt.Sprintf("B?(%v) ", pool.requesters[h].block != nil)
			str += fmt.Sprintf("C?(%v) ", pool.requesters[h].extCommit != nil)
		}
	}
	return str
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

	logger log.Logger
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
	defer peer.pool.mtx.Unlock()

	err := errors.New("peer did not send us anything")
	peer.pool.sendError(err, peer.id)
	peer.logger.Error("SendTimeout", "reason", err, "timeout", peerTimeout)
	peer.didTimeout = true
}

//-------------------------------------

type bpRequester struct {
	service.BaseService
	logger        log.Logger
	pool          *BlockPool
	height        int64
	gotBlockCh    chan struct{}
	redoCh        chan RedoOp // redo may send multitime, add peerId to identify repeat
	timeoutTicker *time.Ticker
	mtx           sync.Mutex
	peerID        types.NodeID
	block         *types.Block
	extCommit     *types.ExtendedCommit
}

type RetryReason string

type RedoOp struct {
	PeerId types.NodeID
	Reason RetryReason
}

func newBPRequester(logger log.Logger, pool *BlockPool, height int64) *bpRequester {
	bpr := &bpRequester{
		logger:        pool.logger,
		pool:          pool,
		height:        height,
		gotBlockCh:    make(chan struct{}, 1),
		redoCh:        make(chan RedoOp, 1),
		timeoutTicker: time.NewTicker(peerTimeout),
		peerID:        "",
		block:         nil,
	}
	bpr.BaseService = *service.NewBaseService(logger, "bpRequester", bpr)
	return bpr
}

func (bpr *bpRequester) OnStart(ctx context.Context) error {
	go bpr.requestRoutine(ctx)
	return nil
}

func (*bpRequester) OnStop() {}

// Returns 0 if block doesn't already exist.
// Returns -1 if block exist but peers doesn't match.
// Return 1 if block exist and peer matches.
func (bpr *bpRequester) setBlock(block *types.Block, extCommit *types.ExtendedCommit, peerID types.NodeID) int {
	bpr.mtx.Lock()
	defer bpr.mtx.Unlock()
	if bpr.block == nil {
		bpr.block = block
		if extCommit != nil {
			bpr.extCommit = extCommit
		}
		select {
		case bpr.gotBlockCh <- struct{}{}:
		default:
		}
		return 0
	} else if bpr.peerID == peerID {
		return 1
	}
	return -1
}

func (bpr *bpRequester) getBlock() *types.Block {
	bpr.mtx.Lock()
	defer bpr.mtx.Unlock()
	return bpr.block
}

func (bpr *bpRequester) getExtendedCommit() *types.ExtendedCommit {
	bpr.mtx.Lock()
	defer bpr.mtx.Unlock()
	return bpr.extCommit
}

func (bpr *bpRequester) getPeerID() types.NodeID {
	bpr.mtx.Lock()
	defer bpr.mtx.Unlock()
	return bpr.peerID
}

// This is called from the requestRoutine, upon redo().
func (bpr *bpRequester) reset(force bool) bool {
	bpr.mtx.Lock()
	defer bpr.mtx.Unlock()

	if bpr.block != nil && !force {
		// Do not reset if we already have a block
		return false
	}

	if bpr.block != nil {
		atomic.AddInt32(&bpr.pool.numPending, 1)
	}

	bpr.peerID = ""
	bpr.block = nil
	bpr.extCommit = nil
	return true
}

// Tells bpRequester to pick another peer and try again.
// NOTE: Nonblocking, and does nothing if another redo
// was already requested.
func (bpr *bpRequester) redo(peerID types.NodeID, retryReason RetryReason) {
	select {
	case bpr.redoCh <- RedoOp{
		PeerId: peerID,
		Reason: retryReason,
	}:
	default:
	}
}

// Responsible for making more requests as necessary
// Returns only when a block is found (e.g. AddBlock() is called)
func (bpr *bpRequester) requestRoutine(ctx context.Context) {
OUTER_LOOP:
	for {
		// Pick a peer to send request to.
		var peer *bpPeer
	PICK_PEER_LOOP:
		for {
			if !bpr.IsRunning() || !bpr.pool.IsRunning() || ctx.Err() != nil {
				bpr.timeoutTicker.Stop()
				return
			}
			if ctx.Err() != nil {
				return
			}

			peer = bpr.pool.pickIncrAvailablePeer(bpr.height)
			if peer == nil {
				// This is preferable to using a timer because the request
				// interval is so small. Larger request intervals may
				// necessitate using a timer/ticker.
				time.Sleep(requestInterval)
				continue PICK_PEER_LOOP
			}
			break PICK_PEER_LOOP
		}
		bpr.mtx.Lock()
		bpr.peerID = peer.id
		bpr.mtx.Unlock()

		// Send request and wait.
		bpr.pool.sendRequest(bpr.height, peer.id)
		bpr.timeoutTicker.Reset(peerTimeout)
	WAIT_LOOP:
		for {
			select {
			case <-ctx.Done():
				bpr.timeoutTicker.Stop()
				return
			case redoOp := <-bpr.redoCh:
				// if we don't have an existing block or this is a bad block
				// we should reset the previous block
				if bpr.reset(redoOp.Reason == BadBlock) {
					continue OUTER_LOOP
				}
				continue WAIT_LOOP
			case <-bpr.timeoutTicker.C:
				if bpr.reset(false) {
					continue OUTER_LOOP
				} else {
					continue WAIT_LOOP
				}
			case <-bpr.gotBlockCh:
				// We got a block!
				// Stop the goroutine to avoid leak
				bpr.timeoutTicker.Stop()
				bpr.Stop()
				return
			}
		}
	}
}
