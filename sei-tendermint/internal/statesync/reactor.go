package statesync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	"github.com/sei-protocol/sei-chain/sei-tendermint/light"
	"github.com/sei-protocol/sei-chain/sei-tendermint/light/provider"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var (
	_ service.Service = (*Reactor)(nil)
)

type isPBMessage interface {
	*pb.SnapshotsRequest |
		*pb.SnapshotsResponse |
		*pb.ChunkRequest |
		*pb.ChunkResponse |
		*pb.LightBlockRequest |
		*pb.LightBlockResponse |
		*pb.ParamsRequest |
		*pb.ParamsResponse
}

func wrap[T isPBMessage](msg T) *pb.Message {
	switch msg := any(msg).(type) {
	case *pb.SnapshotsRequest:
		return &pb.Message{Sum: &pb.Message_SnapshotsRequest{SnapshotsRequest: msg}}
	case *pb.SnapshotsResponse:
		return &pb.Message{Sum: &pb.Message_SnapshotsResponse{SnapshotsResponse: msg}}
	case *pb.ChunkRequest:
		return &pb.Message{Sum: &pb.Message_ChunkRequest{ChunkRequest: msg}}
	case *pb.ChunkResponse:
		return &pb.Message{Sum: &pb.Message_ChunkResponse{ChunkResponse: msg}}
	case *pb.LightBlockRequest:
		return &pb.Message{Sum: &pb.Message_LightBlockRequest{LightBlockRequest: msg}}
	case *pb.LightBlockResponse:
		return &pb.Message{Sum: &pb.Message_LightBlockResponse{LightBlockResponse: msg}}
	case *pb.ParamsRequest:
		return &pb.Message{Sum: &pb.Message_ParamsRequest{ParamsRequest: msg}}
	case *pb.ParamsResponse:
		return &pb.Message{Sum: &pb.Message_ParamsResponse{ParamsResponse: msg}}
	default:
		panic("unreachable")
	}
}

const (
	// SnapshotChannel exchanges snapshot metadata
	SnapshotChannel = p2p.ChannelID(0x60)

	// ChunkChannel exchanges chunk contents
	ChunkChannel = p2p.ChannelID(0x61)

	// LightBlockChannel exchanges light blocks
	LightBlockChannel = p2p.ChannelID(0x62)

	// ParamsChannel exchanges consensus params
	ParamsChannel = p2p.ChannelID(0x63)

	// recentSnapshots is the number of recent snapshots to send and receive per peer.
	recentSnapshots = 10

	// snapshotMsgSize is the maximum size of a snapshotResponseMessage
	snapshotMsgSize = int(4e6) // ~4MB

	// chunkMsgSize is the maximum size of a chunkResponseMessage
	chunkMsgSize = int(16e6) // ~16MB

	// lightBlockMsgSize is the maximum size of a lightBlockResponseMessage
	lightBlockMsgSize = int(1e7) // ~1MB

	// paramMsgSize is the maximum size of a paramsResponseMessage
	paramMsgSize = int(1e5) // ~100kb

	// maxLightBlockRequestRetries is the amount of retries acceptable before
	// the backfill process aborts
	maxLightBlockRequestRetries = 20
)

func GetSnapshotChannelDescriptor() p2p.ChannelDescriptor[*pb.Message] {
	return p2p.ChannelDescriptor[*pb.Message]{
		ID:                  SnapshotChannel,
		MessageType:         new(pb.Message),
		Priority:            6,
		SendQueueCapacity:   10,
		RecvMessageCapacity: snapshotMsgSize,
		RecvBufferCapacity:  128,
		Name:                "snapshot",
	}
}

func GetChunkChannelDescriptor() p2p.ChannelDescriptor[*pb.Message] {
	return p2p.ChannelDescriptor[*pb.Message]{
		ID:                  ChunkChannel,
		Priority:            3,
		MessageType:         new(pb.Message),
		SendQueueCapacity:   4,
		RecvMessageCapacity: chunkMsgSize,
		RecvBufferCapacity:  128,
		Name:                "chunk",
	}
}

func GetLightBlockChannelDescriptor() p2p.ChannelDescriptor[*pb.Message] {
	return p2p.ChannelDescriptor[*pb.Message]{
		ID:                  LightBlockChannel,
		MessageType:         new(pb.Message),
		Priority:            5,
		SendQueueCapacity:   10,
		RecvMessageCapacity: lightBlockMsgSize,
		RecvBufferCapacity:  128,
		Name:                "light-block",
	}
}

func GetParamsChannelDescriptor() p2p.ChannelDescriptor[*pb.Message] {
	return p2p.ChannelDescriptor[*pb.Message]{
		ID:                  ParamsChannel,
		MessageType:         new(pb.Message),
		Priority:            2,
		SendQueueCapacity:   10,
		RecvMessageCapacity: paramMsgSize,
		RecvBufferCapacity:  128,
		Name:                "params",
	}
}

// Metricer defines an interface used for the rpc sync info query, please see statesync.metrics
// for the details.
type Metricer interface {
	TotalSnapshots() int64
	ChunkProcessAvgTime() time.Duration
	SnapshotHeight() int64
	SnapshotChunksCount() int64
	SnapshotChunksTotal() int64
	BackFilledBlocks() int64
	BackFillBlocksTotal() int64
}

// Reactor handles state sync, both restoring snapshots for the local node and
// serving snapshots for other nodes.
type Reactor struct {
	service.BaseService
	logger log.Logger

	chainID       string
	initialHeight int64
	cfg           config.StateSyncConfig
	stateStore    sm.Store
	blockStore    *store.BlockStore

	conn         abci.Application
	tempDir      string
	router       *p2p.Router
	evict        func(types.NodeID, error)
	postSyncHook func(context.Context, sm.State) error

	// when true, the reactor will, during startup perform a
	// statesync for this node, and otherwise just provide
	// snapshots to other nodes.
	needsStateSync bool

	// Dispatcher is used to multiplex light block requests and responses over multiple
	// peers used by the p2p state provider and in reverse sync.
	dispatcher *Dispatcher
	peers      *PeerList

	// These will only be set when a state sync is in progress. It is used to feed
	// received snapshots and chunks into the syncer and manage incoming and outgoing
	// providers.
	mtx            sync.RWMutex
	initSyncer     func() *syncer
	requestSnaphot func() error
	syncer         *syncer
	providers      map[types.NodeID]*BlockProvider
	stateProvider  StateProvider

	eventBus           *eventbus.EventBus
	metrics            *Metrics
	backfillBlockTotal int64
	backfilledBlocks   int64

	// For some reason channels below used to be processed synchronously.
	// Now each of these has their own processing loop, but to simulate the previous
	// behavior we use a mutex to ensure only one message is processed at a time across all channels.
	// TODO(gprusak): verify that the message handlers can be executed concurrenty and remove this mutex.
	processChGuard    sync.Mutex
	snapshotChannel   *p2p.Channel[*pb.Message]
	chunkChannel      *p2p.Channel[*pb.Message]
	lightBlockChannel *p2p.Channel[*pb.Message]
	paramsChannel     *p2p.Channel[*pb.Message]

	// keep track of the last time we saw no available peers, so we can restart if it's been too long
	lastNoAvailablePeers time.Time

	// Used to signal a restart the node on the application level
	restartEvent                  func()
	restartNoAvailablePeersWindow time.Duration
}

// NewReactor returns a reference to a new state sync reactor, which implements
// the service.Service interface. It accepts a logger, connections for snapshots
// and querying, a router used to open the required p2p channels, and a channel
// to listen for peer updates on. Note, the reactor will close all p2p Channels
// when stopping.
func NewReactor(
	chainID string,
	initialHeight int64,
	cfg config.StateSyncConfig,
	logger log.Logger,
	conn abci.Application,
	router *p2p.Router,
	stateStore sm.Store,
	blockStore *store.BlockStore,
	tempDir string,
	ssMetrics *Metrics,
	eventBus *eventbus.EventBus,
	postSyncHook func(context.Context, sm.State) error,
	needsStateSync bool,
	restartEvent func(),
	selfRemediationConfig *config.SelfRemediationConfig,
) (*Reactor, error) {
	snapshotChannel, err := p2p.OpenChannel(router, GetSnapshotChannelDescriptor())
	if err != nil {
		return nil, fmt.Errorf("open snapshot channel: %w", err)
	}
	chunkChannel, err := p2p.OpenChannel(router, GetChunkChannelDescriptor())
	if err != nil {
		return nil, fmt.Errorf("open chunk channel: %w", err)
	}
	lightBlockChannel, err := p2p.OpenChannel(router, GetLightBlockChannelDescriptor())
	if err != nil {
		return nil, fmt.Errorf("open light block channel: %w", err)
	}
	paramsChannel, err := p2p.OpenChannel(router, GetParamsChannelDescriptor())
	if err != nil {
		return nil, fmt.Errorf("open params channel: %w", err)
	}
	r := &Reactor{
		logger:                        logger,
		chainID:                       chainID,
		initialHeight:                 initialHeight,
		cfg:                           cfg,
		conn:                          conn,
		router:                        router,
		tempDir:                       tempDir,
		stateStore:                    stateStore,
		blockStore:                    blockStore,
		peers:                         NewPeerList(),
		providers:                     make(map[types.NodeID]*BlockProvider),
		metrics:                       ssMetrics,
		eventBus:                      eventBus,
		postSyncHook:                  postSyncHook,
		needsStateSync:                needsStateSync,
		snapshotChannel:               snapshotChannel,
		chunkChannel:                  chunkChannel,
		lightBlockChannel:             lightBlockChannel,
		paramsChannel:                 paramsChannel,
		lastNoAvailablePeers:          time.Time{},
		restartEvent:                  restartEvent,
		restartNoAvailablePeersWindow: time.Duration(selfRemediationConfig.StatesyncNoPeersRestartWindowSeconds) * time.Second, //nolint:gosec // validated in config.ValidateBasic against MaxInt64
	}

	r.BaseService = *service.NewBaseService(logger, "StateSync", r)
	return r, nil
}

func (r *Reactor) initStateProvider(ctx context.Context, chainID string, initialHeight int64) error {
	to := light.TrustOptions{
		Period: r.cfg.TrustPeriod,
		Height: r.cfg.TrustHeight,
		Hash:   r.cfg.TrustHashBytes(),
	}
	spLogger := r.logger.With("module", "stateprovider")
	spLogger.Info("initializing state provider", "trustPeriod", to.Period,
		"trustHeight", to.Height, "useP2P", r.cfg.UseP2P)

	if r.cfg.UseP2P {
		if err := r.waitForEnoughPeers(ctx, 2); err != nil {
			return err
		}

		peers := r.peers.All()
		providers := make([]provider.Provider, len(peers))
		for idx, p := range peers {
			providers[idx] = NewBlockProvider(p, chainID, r.dispatcher)
		}

		stateProvider, err := NewP2PStateProvider(ctx, chainID, initialHeight, r.cfg.VerifyLightBlockTimeout, providers, to, r.paramsChannel, r.logger.With("module", "stateprovider"), r.cfg.BlacklistTTL)
		if err != nil {
			return fmt.Errorf("failed to initialize P2P state provider: %w", err)
		}
		r.stateProvider = stateProvider
		return nil
	}

	stateProvider, err := NewRPCStateProvider(ctx, chainID, initialHeight, r.cfg.VerifyLightBlockTimeout, r.cfg.RPCServers, to, spLogger, r.cfg.BlacklistTTL)
	if err != nil {
		return fmt.Errorf("failed to initialize RPC state provider: %w", err)
	}
	r.stateProvider = stateProvider
	return nil
}

// OnStart starts separate go routines for each p2p Channel and listens for
// ms on each. In addition, it also listens for peer updates and handles
// messages on that p2p channel accordingly. Note, we do not launch a go-routine to
// handle individual ms as to not have to deal with bounding workers or pools.
// The caller must be sure to execute OnStop to ensure the outbound p2p Channels are
// closed. No error is returned.
func (r *Reactor) OnStart(ctx context.Context) error {
	// define constructor and helper functions, that hold
	// references to these channels for use later. This is not
	// ideal.
	r.initSyncer = func() *syncer {
		return &syncer{
			logger:           r.logger,
			stateProvider:    r.stateProvider,
			conn:             r.conn,
			snapshots:        newSnapshotPool(),
			snapshotCh:       r.snapshotChannel,
			chunkCh:          r.chunkChannel,
			tempDir:          r.tempDir,
			fetchers:         r.cfg.Fetchers,
			retryTimeout:     r.cfg.ChunkRequestTimeout,
			metrics:          r.metrics,
			useLocalSnapshot: r.cfg.UseLocalSnapshot,
		}
	}
	r.dispatcher = NewDispatcher(r.lightBlockChannel)
	r.requestSnaphot = func() error {
		// request snapshots from all currently connected peers
		if !r.cfg.UseLocalSnapshot {
			r.snapshotChannel.Broadcast(wrap(&pb.SnapshotsRequest{}))
		}
		return nil
	}
	r.evict = r.router.Evict

	go r.processSnapshotCh(ctx)
	go r.processChunkCh(ctx)
	go r.processLightBlockCh(ctx)
	go r.processParamsCh(ctx)

	if !r.cfg.UseLocalSnapshot {
		go r.processPeerUpdates(ctx)
	}

	if r.needsStateSync {
		r.logger.Info("This node needs state sync, going to perform a state sync")
		if _, err := r.Sync(ctx); err != nil {
			r.logger.Error("state sync failed; shutting down this node", "err", err)
			return err
		}
	}

	return nil
}

// OnStop stops the reactor by signaling to all spawned goroutines to exit and
// blocking until they all exit.
func (r *Reactor) OnStop() {
	// tell the dispatcher to stop sending any more requests
	r.dispatcher.Close()
}

// Sync runs a state sync, fetching snapshots and providing chunks to the
// application. At the close of the operation, Sync will bootstrap the state
// store and persist the commit at that height so that either consensus or
// blocksync can commence. It will then proceed to backfill the necessary amount
// of historical blocks before participating in consensus
func (r *Reactor) Sync(ctx context.Context) (sm.State, error) {
	if r.eventBus != nil {
		if err := r.eventBus.PublishEventStateSyncStatus(types.EventDataStateSyncStatus{
			Complete: false,
			Height:   r.initialHeight,
		}); err != nil {
			return sm.State{}, err
		}
	}

	if !r.cfg.UseLocalSnapshot {
		// We need at least two peers (for cross-referencing of light blocks) before we can
		// begin state sync
		if err := r.waitForEnoughPeers(ctx, 2); err != nil {
			return sm.State{}, err
		}
		r.logger.Info("Finished waiting for 2 peers to start state sync")
	}

	r.mtx.Lock()
	if r.syncer != nil {
		r.mtx.Unlock()
		return sm.State{}, errors.New("a state sync is already in progress")
	}

	if err := r.initStateProvider(ctx, r.chainID, r.initialHeight); err != nil {
		r.mtx.Unlock()
		return sm.State{}, err
	}

	r.syncer = r.initSyncer()
	r.mtx.Unlock()

	defer func() {
		r.mtx.Lock()
		// reset syncing objects at the close of Sync
		r.syncer = nil
		r.stateProvider = nil
		r.mtx.Unlock()
	}()

	r.logger.Info("starting state sync")

	if r.cfg.UseLocalSnapshot {
		snapshotList, _ := r.recentSnapshots(context.Background(), recentSnapshots)
		for _, snap := range snapshotList {
			if _, err := r.syncer.AddSnapshot("self", snap); err != nil {
				return sm.State{}, fmt.Errorf("failed to add snapshot at height %d: %w", snap.Height, err)
			}
		}
	}

	state, commit, err := r.syncer.SyncAny(ctx, r.cfg.DiscoveryTime, r.requestSnaphot)
	r.logger.Info("Finished state sync, fetching state and commit to bootstrap the node")
	if err != nil {
		return sm.State{}, err
	}

	if err := r.stateStore.Bootstrap(state); err != nil {
		return sm.State{}, fmt.Errorf("failed to bootstrap node with new state: %w", err)
	}

	if err := r.blockStore.SaveSeenCommit(state.LastBlockHeight, commit); err != nil {
		return sm.State{}, fmt.Errorf("failed to store last seen commit: %w", err)
	}

	if !r.cfg.UseLocalSnapshot {
		if err := r.Backfill(ctx, state); err != nil {
			r.logger.Error("backfill failed. Proceeding optimistically...", "err", err)
		}
	}

	if r.eventBus != nil {
		if err := r.eventBus.PublishEventStateSyncStatus(types.EventDataStateSyncStatus{
			Complete: true,
			Height:   state.LastBlockHeight,
		}); err != nil {
			return sm.State{}, err
		}
	}

	if r.postSyncHook != nil {
		r.logger.Info("Executing post tate sync hook")
		if err := r.postSyncHook(ctx, state); err != nil {
			return sm.State{}, err
		}
	}

	return state, nil
}

// Backfill sequentially fetches, verifies and stores light blocks in reverse
// order. It does not stop verifying blocks until reaching a block with a height
// and time that is less or equal to the stopHeight and stopTime. The
// trustedBlockID should be of the header at startHeight.
func (r *Reactor) Backfill(ctx context.Context, state sm.State) error {
	stopHeight := state.LastBlockHeight - r.cfg.BackfillBlocks
	stopTime := state.LastBlockTime.Add(-r.cfg.BackfillDuration)
	// ensure that stop height doesn't go below the initial height
	if stopHeight < state.InitialHeight {
		stopHeight = state.InitialHeight
		// this essentially makes stop time a void criteria for termination
		stopTime = state.LastBlockTime
	}
	return r.backfill(
		ctx,
		state.ChainID,
		state.LastBlockHeight,
		stopHeight,
		state.InitialHeight,
		state.LastBlockID,
		stopTime,
	)
}

func (r *Reactor) backfill(
	ctx context.Context,
	chainID string,
	startHeight, stopHeight, initialHeight int64,
	trustedBlockID types.BlockID,
	stopTime time.Time,
) error {
	r.logger.Info("starting backfill process...", "startHeight", startHeight,
		"stopHeight", stopHeight, "stopTime", stopTime, "trustedBlockID", trustedBlockID)

	r.backfillBlockTotal = startHeight - stopHeight + 1
	r.metrics.BackFillBlocksTotal.Set(float64(r.backfillBlockTotal))

	const sleepTime = 1 * time.Second
	var (
		lastValidatorSet *types.ValidatorSet
		lastChangeHeight = startHeight
	)

	queue := newBlockQueue(startHeight, stopHeight, initialHeight, stopTime, maxLightBlockRequestRetries)

	// fetch light blocks across four workers. The aim with deploying concurrent
	// workers is to equate the network messaging time with the verification
	// time. Ideally we want the verification process to never have to be
	// waiting on blocks. If it takes 4s to retrieve a block and 1s to verify
	// it, then steady state involves four workers.
	for i := 0; i < int(r.cfg.Fetchers); i++ {
		ctxWithCancel, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case height := <-queue.nextHeight():
					// pop the next peer of the list to send a request to
					peer := r.peers.Pop(ctx)
					r.logger.Debug("fetching next block", "height", height, "peer", peer)
					subCtx, cancel := context.WithTimeout(ctxWithCancel, r.cfg.LightBlockResponseTimeout)
					defer cancel()
					lb, err := func() (*types.LightBlock, error) {
						defer cancel()
						// request the light block with a timeout
						return r.dispatcher.LightBlock(subCtx, height, peer)
					}()
					// once the peer has returned a value, add it back to the peer list to be used again
					r.peers.Append(peer)
					if errors.Is(err, context.Canceled) {
						return
					}
					if err != nil {
						queue.retry(height)
						if errors.Is(err, ErrNoConnectedPeers) {
							r.logger.Info("backfill: no connected peers to fetch light blocks from; sleeping...",
								"sleepTime", sleepTime)
							time.Sleep(sleepTime)
						} else {
							// we don't punish the peer as it might just have not responded in time
							r.logger.Info("backfill: error with fetching light block",
								"height", height, "err", err)
						}
						continue
					}
					if lb == nil {
						r.logger.Info("backfill: peer didn't have block, fetching from another peer", "height", height)
						queue.retry(height)
						// As we are fetching blocks backwards, if this node doesn't have the block it likely doesn't
						// have any prior ones, thus we remove it from the peer list.
						r.peers.Remove(peer)
						continue
					}

					// run a validate basic. This checks the validator set and commit
					// hashes line up
					err = lb.ValidateBasic(chainID)
					if err != nil || lb.Height != height {
						r.logger.Info("backfill: fetched light block failed validate basic, removing peer...",
							"err", err, "height", height)
						queue.retry(height)
						r.evict(peer, fmt.Errorf("statesync: received invalid light block: %w", err))
						continue
					}

					// add block to queue to be verified
					queue.add(lightBlockResponse{
						block: lb,
						peer:  peer,
					})
					r.logger.Debug("backfill: added light block to processing queue", "height", height)

				case <-queue.done():
					return
				}
			}
		}()
	}

	// verify all light blocks
	for {
		select {
		case <-ctx.Done():
			queue.close()
			return nil
		case resp := <-queue.verifyNext():
			// validate the header hash. We take the last block id of the
			// previous header (i.e. one height above) as the trusted hash which
			// we equate to. ValidatorsHash and CommitHash have already been
			// checked in the `ValidateBasic`
			if w, g := trustedBlockID.Hash, resp.block.Hash(); !bytes.Equal(w, g) {
				r.logger.Info("received invalid light block. header hash doesn't match trusted LastBlockID",
					"trustedHash", w, "receivedHash", g, "height", resp.block.Height)
				r.evict(resp.peer, fmt.Errorf("statesync: received invalid light block. Expected hash %v, got: %v", w, g))
				queue.retry(resp.block.Height)
				continue
			}

			// save the signed headers
			if err := r.blockStore.SaveSignedHeader(resp.block.SignedHeader, trustedBlockID); err != nil {
				return err
			}

			// check if there has been a change in the validator set
			if lastValidatorSet != nil && !bytes.Equal(resp.block.ValidatorsHash, resp.block.NextValidatorsHash) {
				// save all the heights that the last validator set was the same
				if err := r.stateStore.SaveValidatorSets(resp.block.Height+1, lastChangeHeight, lastValidatorSet); err != nil {
					return err
				}

				// update the lastChangeHeight
				lastChangeHeight = resp.block.Height
			}

			trustedBlockID = resp.block.LastBlockID
			queue.success()
			r.logger.Info("backfill: verified and stored light block", "height", resp.block.Height)

			lastValidatorSet = resp.block.ValidatorSet

			r.backfilledBlocks++
			r.metrics.BackFilledBlocks.Add(1)

			// The block height might be less than the stopHeight because of the stopTime condition
			// hasn't been fulfilled.
			if resp.block.Height < stopHeight {
				r.backfillBlockTotal++
				r.metrics.BackFillBlocksTotal.Set(float64(r.backfillBlockTotal))
			}

		case <-queue.done():
			if err := queue.error(); err != nil {
				return err
			}

			// save the final batch of validators
			if err := r.stateStore.SaveValidatorSets(queue.terminal.Height, lastChangeHeight, lastValidatorSet); err != nil {
				return err
			}

			r.logger.Info("successfully completed backfill process", "endHeight", queue.terminal.Height)
			return nil
		}
	}
}

// handleSnapshotMessage handles ms sent from peers on the
// SnapshotChannel. It returns an error only if the Envelope.Message is unknown
// for this channel. This should never be called outside of handleMessage.
func (r *Reactor) handleSnapshotMessage(ctx context.Context, m p2p.RecvMsg[*pb.Message]) (err error) {
	defer r.recoverToErr(&err)
	logger := r.logger.With("peer", m.From)
	snapshotCh := r.snapshotChannel

	switch msg := m.Message.Sum.(type) {
	case *pb.Message_SnapshotsRequest:
		snapshots, err := r.recentSnapshots(ctx, recentSnapshots)
		if err != nil {
			logger.Error("failed to fetch snapshots", "err", err)
			return nil
		}

		for _, snapshot := range snapshots {
			logger.Info(
				"advertising snapshot",
				"height", snapshot.Height,
				"format", snapshot.Format,
				"peer", m.From,
			)

			snapshotCh.Send(wrap(&pb.SnapshotsResponse{
				Height:   snapshot.Height,
				Format:   snapshot.Format,
				Chunks:   snapshot.Chunks,
				Hash:     snapshot.Hash,
				Metadata: snapshot.Metadata,
			}), m.From)
		}

	case *pb.Message_SnapshotsResponse:
		resp := msg.SnapshotsResponse
		r.mtx.RLock()
		defer r.mtx.RUnlock()

		if r.syncer == nil {
			logger.Debug("received unexpected snapshot; no state sync in progress")
			return nil
		}

		logger.Info("received snapshot", "height", resp.GetHeight(), "format", resp.GetFormat())
		_, err := r.syncer.AddSnapshot(m.From, &snapshot{
			Height:   resp.GetHeight(),
			Format:   resp.GetFormat(),
			Chunks:   resp.GetChunks(),
			Hash:     resp.GetHash(),
			Metadata: resp.GetMetadata(),
		})
		if err != nil {
			logger.Error(
				"failed to add snapshot",
				"height", resp.GetHeight(),
				"format", resp.GetFormat(),
				"err", err,
			)
			return nil
		}
		logger.Info("added snapshot", "height", resp.GetHeight(), "format", resp.GetFormat())

	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}

	return nil
}

// handleChunkMessage handles ms sent from peers on the ChunkChannel.
// It returns an error only if the Envelope.Message is unknown for this channel.
// This should never be called outside of handleMessage.
func (r *Reactor) handleChunkMessage(ctx context.Context, m p2p.RecvMsg[*pb.Message]) (err error) {
	chunkCh := r.chunkChannel
	defer r.recoverToErr(&err)
	switch msg := m.Message.Sum.(type) {
	case *pb.Message_ChunkRequest:
		req := msg.ChunkRequest
		r.logger.Debug(
			"received chunk request",
			"height", req.GetHeight(),
			"format", req.GetFormat(),
			"chunk", req.GetIndex(),
			"peer", m.From,
		)
		resp, err := r.conn.LoadSnapshotChunk(ctx, &abci.RequestLoadSnapshotChunk{
			Height: req.GetHeight(),
			Format: req.GetFormat(),
			Chunk:  req.GetIndex(),
		})
		if err != nil {
			r.logger.Error(
				"failed to load chunk",
				"height", req.GetHeight(),
				"format", req.GetFormat(),
				"chunk", req.GetIndex(),
				"err", err,
				"peer", m.From,
			)
			return nil
		}

		r.logger.Debug(
			"sending chunk",
			"height", req.GetHeight(),
			"format", req.GetFormat(),
			"chunk", req.GetIndex(),
			"peer", m.From,
		)
		chunkCh.Send(wrap(&pb.ChunkResponse{
			Height:  req.GetHeight(),
			Format:  req.GetFormat(),
			Index:   req.GetIndex(),
			Chunk:   resp.Chunk,
			Missing: resp.Chunk == nil,
		}), m.From)

	case *pb.Message_ChunkResponse:
		resp := msg.ChunkResponse
		r.mtx.RLock()
		defer r.mtx.RUnlock()

		if r.syncer == nil {
			r.logger.Debug("received unexpected chunk; no state sync in progress", "peer", m.From)
			return nil
		}

		r.logger.Debug(
			"received chunk; adding to sync",
			"height", resp.GetHeight(),
			"format", resp.GetFormat(),
			"chunk", resp.GetIndex(),
			"peer", m.From,
		)
		_, err := r.syncer.AddChunk(&chunk{
			Height: resp.GetHeight(),
			Format: resp.GetFormat(),
			Index:  resp.GetIndex(),
			Chunk:  resp.GetChunk(),
			Sender: m.From,
		})
		if err != nil {
			r.logger.Error(
				"failed to add chunk",
				"height", resp.GetHeight(),
				"format", resp.GetFormat(),
				"chunk", resp.GetIndex(),
				"err", err,
				"peer", m.From,
			)
			return nil
		}

	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}

	return nil
}

func (r *Reactor) handleLightBlockMessage(ctx context.Context, m p2p.RecvMsg[*pb.Message]) (err error) {
	blockCh := r.lightBlockChannel
	defer r.recoverToErr(&err)
	switch msg := m.Message.Sum.(type) {
	case *pb.Message_LightBlockRequest:
		req := msg.LightBlockRequest
		r.logger.Info("received light block request", "height", req.GetHeight())
		lb, err := r.fetchLightBlock(req.GetHeight())
		if err != nil {
			r.logger.Error("failed to retrieve light block", "err", err, "height", req.GetHeight())
			return err
		}
		if lb == nil {
			blockCh.Send(wrap(&pb.LightBlockResponse{LightBlock: nil}), m.From)
			return nil
		}

		lbproto, err := lb.ToProto()
		if err != nil {
			r.logger.Error("marshaling light block to proto", "err", err)
			return nil
		}

		// NOTE: If we don't have the light block we will send a nil light block
		// back to the requested node, indicating that we don't have it.
		blockCh.Send(wrap(&pb.LightBlockResponse{LightBlock: lbproto}), m.From)
	case *pb.Message_LightBlockResponse:
		resp := msg.LightBlockResponse
		var height int64
		if resp.LightBlock != nil {
			height = resp.LightBlock.GetSignedHeader().GetHeader().GetHeight()
		}
		r.logger.Info("received light block response", "peer", m.From, "height", height)
		if err := r.dispatcher.Respond(ctx, resp.LightBlock, m.From); err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			r.logger.Error("error processing light block response", "err", err, "height", height)
		}

	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}

	return nil
}

func (r *Reactor) handleParamsMessage(ctx context.Context, m p2p.RecvMsg[*pb.Message]) (err error) {
	defer r.recoverToErr(&err)

	switch msg := m.Message.Sum.(type) {
	case *pb.Message_ParamsRequest:
		req := msg.ParamsRequest
		if req.GetHeight() > math.MaxInt64 {
			r.logger.Error("invalid height in params request", "height", req.GetHeight())
			return nil
		}
		r.logger.Debug("received consensus params request", "height", req.GetHeight())
		cp, err := r.stateStore.LoadConsensusParams(int64(req.GetHeight())) //nolint:gosec // height from peer is validated above
		if err != nil {
			r.logger.Error("failed to fetch requested consensus params", "err", err, "height", req.GetHeight())
			return nil
		}

		cpproto := cp.ToProto()
		r.paramsChannel.Send(wrap(&pb.ParamsResponse{
			Height:          req.GetHeight(),
			ConsensusParams: cpproto,
		}), m.From)
	case *pb.Message_ParamsResponse:
		resp := msg.ParamsResponse
		r.mtx.RLock()
		defer r.mtx.RUnlock()
		r.logger.Debug("received consensus params response", "height", resp.GetHeight())

		cp := types.ConsensusParamsFromProto(resp.GetConsensusParams())

		if sp, ok := r.stateProvider.(*StateProviderP2P); ok {
			select {
			case sp.ParamsRecvCh() <- cp:
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
				return errors.New("failed to send consensus params, stateprovider not ready for response")
			}
		} else {
			r.logger.Debug("received unexpected params response; using RPC state provider", "peer", m.From)
		}

	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}

	return nil
}

func (r *Reactor) recoverToErr(err *error) {
	if e := recover(); e != nil {
		*err = fmt.Errorf("panic in processing message: %v", e)
		r.logger.Error(
			"recovering from processing message panic",
			"err", *err,
			"stack", string(debug.Stack()),
		)
	}
}

func (r *Reactor) processSnapshotCh(ctx context.Context) {
	for ctx.Err() == nil {
		m, err := r.snapshotChannel.Recv(ctx)
		if err != nil {
			return
		}
		r.processChGuard.Lock()
		if err := r.handleSnapshotMessage(ctx, m); err != nil && ctx.Err() == nil {
			r.router.Evict(m.From, fmt.Errorf("statesync.snapshot: %w", err))
		}
		r.processChGuard.Unlock()
	}
}

func (r *Reactor) processChunkCh(ctx context.Context) {
	for ctx.Err() == nil {
		m, err := r.chunkChannel.Recv(ctx)
		if err != nil {
			return
		}
		r.processChGuard.Lock()
		if err := r.handleChunkMessage(ctx, m); err != nil && ctx.Err() == nil {
			r.router.Evict(m.From, fmt.Errorf("statesync.chunk: %w", err))
		}
		r.processChGuard.Unlock()
	}
}

func (r *Reactor) processLightBlockCh(ctx context.Context) {
	for ctx.Err() == nil {
		m, err := r.lightBlockChannel.Recv(ctx)
		if err != nil {
			return
		}
		r.processChGuard.Lock()
		if err := r.handleLightBlockMessage(ctx, m); err != nil && ctx.Err() == nil {
			r.router.Evict(m.From, fmt.Errorf("statesync.lightBlock: %w", err))
		}
		r.processChGuard.Unlock()
	}
}

func (r *Reactor) processParamsCh(ctx context.Context) {
	for ctx.Err() == nil {
		m, err := r.paramsChannel.Recv(ctx)
		if err != nil {
			return
		}
		r.processChGuard.Lock()
		if err := r.handleParamsMessage(ctx, m); err != nil && ctx.Err() == nil {
			r.router.Evict(m.From, fmt.Errorf("statesync.params: %w", err))
		}
		r.processChGuard.Unlock()
	}
}

// processPeerUpdate processes a PeerUpdate, returning an error upon failing to
// handle the PeerUpdate or if a panic is recovered.
func (r *Reactor) processPeerUpdate(peerUpdate p2p.PeerUpdate) {
	r.logger.Debug("received peer update", "peer", peerUpdate.NodeID, "status", peerUpdate.Status)

	switch peerUpdate.Status {
	case p2p.PeerStatusUp:
		if peerUpdate.Channels.Contains(SnapshotChannel) &&
			peerUpdate.Channels.Contains(ChunkChannel) &&
			peerUpdate.Channels.Contains(LightBlockChannel) &&
			peerUpdate.Channels.Contains(ParamsChannel) {

			r.peers.Append(peerUpdate.NodeID)
		} else {
			r.logger.Error("could not use peer for statesync (removing)", "peer", peerUpdate.NodeID)
			r.peers.Remove(peerUpdate.NodeID)
		}
	case p2p.PeerStatusDown:
		r.peers.Remove(peerUpdate.NodeID)
	}

	r.mtx.Lock()
	defer r.mtx.Unlock()

	if r.peers.Len() == 0 && r.restartNoAvailablePeersWindow > 0 {
		if r.lastNoAvailablePeers.IsZero() {
			r.lastNoAvailablePeers = time.Now()
		} else if time.Since(r.lastNoAvailablePeers) > r.restartNoAvailablePeersWindow {
			r.logger.Error("no available peers left for statesync (restarting router)")
			r.restartEvent()
		}
	} else {
		// Reset
		r.lastNoAvailablePeers = time.Time{}
	}

	if r.syncer == nil {
		return
	}

	switch peerUpdate.Status {
	case p2p.PeerStatusUp:
		newProvider := NewBlockProvider(peerUpdate.NodeID, r.chainID, r.dispatcher)

		r.providers[peerUpdate.NodeID] = newProvider
		r.syncer.AddPeer(peerUpdate.NodeID)
		if sp, ok := r.stateProvider.(*StateProviderP2P); ok {
			// we do this in a separate routine to not block whilst waiting for the light client to finish
			// whatever call it's currently executing
			go sp.AddProvider(newProvider)
		}

	case p2p.PeerStatusDown:
		delete(r.providers, peerUpdate.NodeID)
		r.syncer.RemovePeer(peerUpdate.NodeID)
		if sp, ok := r.stateProvider.(*StateProviderP2P); ok {
			if err := sp.RemoveProviderByID(peerUpdate.NodeID); err != nil {
				r.logger.Error("failed to remove peer witness", "peer", peerUpdate.NodeID, "error", err)
			}
		}
	}
	r.logger.Debug("processed peer update", "peer", peerUpdate.NodeID, "status", peerUpdate.Status)
}

// processPeerUpdates initiates a blocking process where we listen for and handle
// PeerUpdate messages. When the reactor is stopped, we will catch the signal and
// close the p2p PeerUpdatesCh gracefully.
func (r *Reactor) processPeerUpdates(ctx context.Context) {
	recv := r.router.Subscribe()
	for {
		peerUpdate, err := recv.Recv(ctx)
		if err != nil {
			return
		}
		r.processPeerUpdate(peerUpdate)
	}
}

// recentSnapshots fetches the n most recent snapshots from the app
func (r *Reactor) recentSnapshots(ctx context.Context, n uint32) ([]*snapshot, error) {
	resp, err := r.conn.ListSnapshots(ctx, &abci.RequestListSnapshots{})
	if err != nil {
		return nil, err
	}

	sort.Slice(resp.Snapshots, func(i, j int) bool {
		a := resp.Snapshots[i]
		b := resp.Snapshots[j]

		switch {
		case a.Height > b.Height:
			return true
		case a.Height == b.Height && a.Format > b.Format:
			return true
		default:
			return false
		}
	})

	snapshots := make([]*snapshot, 0, n)
	for i, s := range resp.Snapshots {
		if i >= recentSnapshots {
			break
		}

		snapshots = append(snapshots, &snapshot{
			Height:   s.Height,
			Format:   s.Format,
			Chunks:   s.Chunks,
			Hash:     s.Hash,
			Metadata: s.Metadata,
		})
	}

	return snapshots, nil
}

// fetchLightBlock works out whether the node has a light block at a particular
// height and if so returns it so it can be gossiped to peers
func (r *Reactor) fetchLightBlock(height uint64) (*types.LightBlock, error) {
	h := int64(height) //nolint:gosec // height validated by Message.Validate() upstream

	blockMeta := r.blockStore.LoadBlockMeta(h)
	if blockMeta == nil {
		return nil, nil
	}

	commit := r.blockStore.LoadBlockCommit(h)
	if commit == nil {
		return nil, nil
	}

	vals, err := r.stateStore.LoadValidators(h)
	if err != nil {
		return nil, err
	}
	if vals == nil {
		return nil, nil
	}

	return &types.LightBlock{
		SignedHeader: &types.SignedHeader{
			Header: &blockMeta.Header,
			Commit: commit,
		},
		ValidatorSet: vals,
	}, nil
}

func (r *Reactor) waitForEnoughPeers(ctx context.Context, numPeers int) error {
	startAt := time.Now()
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	logT := time.NewTicker(time.Minute)
	defer logT.Stop()
	var iter int
	for r.peers.Len() < numPeers {
		iter++
		select {
		case <-ctx.Done():
			return fmt.Errorf("operation canceled while waiting for peers after %.2fs [%d/%d]",
				time.Since(startAt).Seconds(), r.peers.Len(), numPeers)
		case <-t.C:
			continue
		case <-logT.C:
			r.logger.Info("waiting for sufficient peers to start statesync",
				"duration", time.Since(startAt).String(),
				"target", numPeers,
				"peers", r.peers.Len(),
				"iters", iter,
			)
			continue
		}
	}
	return nil
}

func (r *Reactor) TotalSnapshots() int64 {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	if r.syncer != nil && r.syncer.snapshots != nil {
		return int64(len(r.syncer.snapshots.snapshots))
	}
	return 0
}

func (r *Reactor) ChunkProcessAvgTime() time.Duration {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	if r.syncer != nil {
		return time.Duration(r.syncer.avgChunkTime)
	}
	return time.Duration(0)
}

func (r *Reactor) SnapshotHeight() int64 {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	if r.syncer != nil {
		return r.syncer.lastSyncedSnapshotHeight
	}
	return 0
}
func (r *Reactor) SnapshotChunksCount() int64 {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	if r.syncer != nil && r.syncer.chunks != nil {
		return int64(r.syncer.chunks.numChunksReturned())
	}
	return 0
}

func (r *Reactor) SnapshotChunksTotal() int64 {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	if r.syncer != nil && r.syncer.processingSnapshot != nil {
		return int64(r.syncer.processingSnapshot.Chunks)
	}
	return 0
}

func (r *Reactor) BackFilledBlocks() int64 {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	return r.backfilledBlocks
}

func (r *Reactor) BackFillBlocksTotal() int64 {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	return r.backfillBlockTotal
}
