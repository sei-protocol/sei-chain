package blocksync

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var _ service.Service = (*Reactor)(nil)

const (
	// BlockSyncChannel is a channel for blocks and status updates
	BlockSyncChannel = p2p.ChannelID(0x40)

	trySyncIntervalMS = 10

	// ask for best height every 10s
	statusUpdateInterval = 10 * time.Second

	// check if we should switch to consensus reactor
	switchToConsensusIntervalSeconds = 1

	// switch to consensus after this duration of inactivity
	syncTimeout = 180 * time.Second
)

// TODO(gprusak): that's not sufficient - parsing proto requires checking nils everywhere.
func wrap[T *pb.BlockRequest | *pb.NoBlockResponse | *pb.BlockResponse | *pb.StatusRequest | *pb.StatusResponse](msg T) *pb.Message {
	switch msg := any(msg).(type) {
	case *pb.BlockRequest:
		return &pb.Message{Sum: &pb.Message_BlockRequest{BlockRequest: msg}}
	case *pb.NoBlockResponse:
		return &pb.Message{Sum: &pb.Message_NoBlockResponse{NoBlockResponse: msg}}
	case *pb.BlockResponse:
		return &pb.Message{Sum: &pb.Message_BlockResponse{BlockResponse: msg}}
	case *pb.StatusRequest:
		return &pb.Message{Sum: &pb.Message_StatusRequest{StatusRequest: msg}}
	case *pb.StatusResponse:
		return &pb.Message{Sum: &pb.Message_StatusResponse{StatusResponse: msg}}
	default:
		panic("unreachable")
	}
}

func GetChannelDescriptor() p2p.ChannelDescriptor[*pb.Message] {
	return p2p.ChannelDescriptor[*pb.Message]{
		ID:                  BlockSyncChannel,
		MessageType:         new(pb.Message),
		Priority:            5,
		SendQueueCapacity:   1000,
		RecvBufferCapacity:  1024,
		RecvMessageCapacity: MaxMsgSize,
		Name:                "blockSync",
	}
}

type consensusReactor interface {
	// For when we switch from block sync reactor to the consensus
	// machine.
	SwitchToConsensus(ctx context.Context, state sm.State, skipWAL bool)
}

type peerError struct {
	err    error
	peerID types.NodeID
}

func (e peerError) Error() string {
	return fmt.Sprintf("error with peer %v: %s", e.peerID, e.err.Error())
}

// Reactor handles long-term catchup syncing.
type Reactor struct {
	service.BaseService
	logger log.Logger

	// immutable
	initialState sm.State
	// store
	stateStore sm.Store

	blockExec             *sm.BlockExecutor
	store                 sm.BlockStore
	pool                  *BlockPool
	consReactor           consensusReactor
	blockSync             *atomicBool
	previousMaxPeerHeight int64

	router  *p2p.Router
	channel *p2p.Channel[*pb.Message]

	requestsCh <-chan BlockRequest
	errorsCh   <-chan peerError

	metrics  *consensus.Metrics
	eventBus *eventbus.EventBus

	syncStartTime time.Time

	restartEvent              func()
	lastRestartTime           time.Time
	blocksBehindThreshold     uint64
	blocksBehindCheckInterval time.Duration
	restartCooldownSeconds    uint64
}

// NewReactor returns new reactor instance.
func NewReactor(
	logger log.Logger,
	stateStore sm.Store,
	blockExec *sm.BlockExecutor,
	store *store.BlockStore,
	consReactor consensusReactor,
	router *p2p.Router,
	blockSync bool,
	metrics *consensus.Metrics,
	eventBus *eventbus.EventBus,
	restartEvent func(), // should be idempotent and non-blocking
	selfRemediationConfig *config.SelfRemediationConfig,
) (*Reactor, error) {
	channel, err := p2p.OpenChannel(router, GetChannelDescriptor())
	if err != nil {
		return nil, fmt.Errorf("router.AddChannel(): %w", err)
	}
	r := &Reactor{
		logger:                    logger,
		stateStore:                stateStore,
		blockExec:                 blockExec,
		store:                     store,
		consReactor:               consReactor,
		blockSync:                 newAtomicBool(blockSync),
		router:                    router,
		channel:                   channel,
		metrics:                   metrics,
		eventBus:                  eventBus,
		restartEvent:              restartEvent,
		lastRestartTime:           time.Now(),
		blocksBehindThreshold:     selfRemediationConfig.BlocksBehindThreshold,
		blocksBehindCheckInterval: time.Duration(selfRemediationConfig.BlocksBehindCheckIntervalSeconds) * time.Second, //nolint:gosec // validated in config.ValidateBasic against MaxInt64
		restartCooldownSeconds:    selfRemediationConfig.RestartCooldownSeconds,
	}

	r.BaseService = *service.NewBaseService(logger, "BlockSync", r)
	return r, nil
}

// OnStart starts separate go routines for each p2p Channel and listens for
// envelopes on each. In addition, it also listens for peer updates and handles
// messages on that p2p channel accordingly. The caller must be sure to execute
// OnStop to ensure the outbound p2p Channels are closed.
//
// If blockSync is enabled, we also start the pool and the pool processing
// goroutine. If the pool fails to start, an error is returned.
func (r *Reactor) OnStart(ctx context.Context) error {
	state, err := r.stateStore.Load()
	if err != nil {
		return err
	}
	r.initialState = state
	r.lastRestartTime = time.Now()

	if state.LastBlockHeight != r.store.Height() {
		return fmt.Errorf("state (%v) and store (%v) height mismatch", state.LastBlockHeight, r.store.Height())
	}

	startHeight := r.store.Height() + 1
	if startHeight == 1 {
		startHeight = state.InitialHeight
	}

	requestsCh := make(chan BlockRequest, maxTotalRequesters)
	errorsCh := make(chan peerError, maxPeerErrBuffer) // NOTE: The capacity should be larger than the peer count.
	r.pool = NewBlockPool(r.logger, startHeight, requestsCh, errorsCh, r.router)
	r.requestsCh = requestsCh
	r.errorsCh = errorsCh

	if r.blockSync.IsSet() {
		if err := r.pool.Start(ctx); err != nil {
			return err
		}
		go r.requestRoutine(ctx)

		go r.poolRoutine(ctx, false)
	}

	go r.processBlockSyncCh(ctx)
	go r.processPeerUpdates(ctx)

	return nil
}

// OnStop stops the reactor by signaling to all spawned goroutines to exit and
// blocking until they all exit.
func (r *Reactor) OnStop() {
	if r.blockSync.IsSet() {
		r.pool.Stop()
	}
}

// respondToPeer loads a block and sends it to the requesting peer, if we have it.
// Otherwise, we'll respond saying we do not have it.
func (r *Reactor) respondToPeer(msg *pb.BlockRequest, peerID types.NodeID) error {
	block := r.store.LoadBlock(msg.GetHeight())
	if block == nil {
		r.logger.Info("peer requesting a block we do not have", "peer", peerID, "height", msg.GetHeight())
		r.channel.Send(wrap(&pb.NoBlockResponse{Height: msg.GetHeight()}), peerID)
		return nil
	}

	blockProto, err := block.ToProto()
	if err != nil {
		return fmt.Errorf("failed to convert block to protobuf: %w", err)
	}

	r.channel.Send(wrap(&pb.BlockResponse{Block: blockProto}), peerID)
	return nil
}

// handleMessage handles an Envelope sent from a peer on a specific p2p Channel.
// It will handle errors and any possible panics gracefully. A caller can handle
// any error returned by sending a PeerError on the respective channel.
func (r *Reactor) handleMessage(m p2p.RecvMsg[*pb.Message]) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("panic in processing message: %v", e)
			r.logger.Error(
				"recovering from processing message panic",
				"err", err,
				"stack", string(debug.Stack()),
			)
		}
	}()

	r.logger.Debug("received message", "message", m.Message, "peer", m.From)

	switch msg := m.Message.Sum.(type) {
	case *pb.Message_BlockRequest:
		return r.respondToPeer(msg.BlockRequest, m.From)
	case *pb.Message_BlockResponse:
		block, err := types.BlockFromProto(msg.BlockResponse.GetBlock())
		if err != nil {
			return fmt.Errorf("types.BlockFromProto(): %w", err)
		}
		r.logger.Info("received block response from peer", "peer", m.From, "height", block.Height)
		if err := r.pool.AddBlock(m.From, block, block.Size()); err != nil {
			r.logger.Error("failed to add block", "err", err)
		}
		return nil
	case *pb.Message_StatusRequest:
		r.channel.Send(wrap(&pb.StatusResponse{
			Height: r.store.Height(),
			Base:   r.store.Base(),
		}), m.From)
		return nil
	case *pb.Message_StatusResponse:
		r.pool.SetPeerRange(m.From, msg.StatusResponse.GetBase(), msg.StatusResponse.GetHeight())
		return nil
	case *pb.Message_NoBlockResponse:
		r.logger.Debug("peer does not have the requested block",
			"peer", m.From,
			"height", msg.NoBlockResponse.GetHeight())
		return nil
	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}
}

// processBlockSyncCh initiates a blocking process where we listen for and handle
// envelopes on the BlockSyncChannel and blockSyncOutBridgeCh. Any error encountered during
// message execution will result in a PeerError being sent on the BlockSyncChannel.
// When the reactor is stopped, we will catch the signal and close the p2p Channel
// gracefully.
func (r *Reactor) processBlockSyncCh(ctx context.Context) {
	for ctx.Err() == nil {
		m, err := r.channel.Recv(ctx)
		if err != nil {
			return
		}
		if err := r.handleMessage(m); err != nil && ctx.Err() == nil {
			r.router.Evict(m.From, fmt.Errorf("blocksync: %w", err))
		}
	}
}

// autoRestartIfBehind will check if the node is behind the max peer height by
// a certain threshold. If it is, the node will attempt to restart itself
// TODO(gprusak): this should be a sub task of the consensus reactor instead.
func (r *Reactor) autoRestartIfBehind(ctx context.Context) {
	if r.blocksBehindThreshold == 0 || r.blocksBehindCheckInterval <= 0 {
		r.logger.Info("Auto remediation is disabled")
		return
	}

	r.logger.Info("checking if node is behind threshold, auto restarting if its behind", "threshold", r.blocksBehindThreshold, "interval", r.blocksBehindCheckInterval)
	for {
		select {
		case <-time.After(r.blocksBehindCheckInterval):
			selfHeight := r.store.Height()
			maxPeerHeight := r.pool.MaxPeerHeight()
			threshold := int64(r.blocksBehindThreshold) //nolint:gosec // validated in config.ValidateBasic against MaxInt64
			behindHeight := maxPeerHeight - selfHeight
			blockSyncIsSet := r.blockSync.IsSet()
			if maxPeerHeight > r.previousMaxPeerHeight {
				r.previousMaxPeerHeight = maxPeerHeight
			}

			// We do not restart if we are not lagging behind, or we are already in block sync mode
			if maxPeerHeight == 0 || behindHeight < threshold || blockSyncIsSet {
				r.logger.Debug("does not exceed threshold or is already in block sync mode", "threshold", threshold, "behindHeight", behindHeight, "maxPeerHeight", maxPeerHeight, "selfHeight", selfHeight, "blockSyncIsSet", blockSyncIsSet)
				continue
			}
			// Check if we have met cooldown time
			if time.Since(r.lastRestartTime).Seconds() < float64(r.restartCooldownSeconds) {
				r.logger.Debug("we are lagging behind, going to trigger a restart after cooldown time passes")
				continue
			}
			r.logger.Info("Blocks behind threshold, restarting node", "threshold", threshold, "behindHeight", behindHeight, "maxPeerHeight", maxPeerHeight, "selfHeight", selfHeight)

			// Send signal to restart the node
			r.blockSync.Set()
			r.restartEvent()
			return
		case <-ctx.Done():
			return
		}
	}
}

// processPeerUpdate processes a PeerUpdate.
func (r *Reactor) processPeerUpdate(peerUpdate p2p.PeerUpdate) {
	r.logger.Debug("received peer update", "peer", peerUpdate.NodeID, "status", peerUpdate.Status)

	switch peerUpdate.Status {
	case p2p.PeerStatusUp:
		// send a status update the newly added peer
		r.channel.Send(wrap(&pb.StatusResponse{
			Base:   r.store.Base(),
			Height: r.store.Height(),
		}), peerUpdate.NodeID)
	case p2p.PeerStatusDown:
		r.pool.RemovePeer(peerUpdate.NodeID)
	}
}

// processPeerUpdates initiates a blocking process where we listen for and handle
// PeerUpdate messages. When the reactor is stopped, we will catch the signal and
// close the p2p PeerUpdatesCh gracefully.
func (r *Reactor) processPeerUpdates(ctx context.Context) {
	recv := r.router.Subscribe()
	for {
		update, err := recv.Recv(ctx)
		if err != nil {
			return
		}
		r.processPeerUpdate(update)
	}
}

// SwitchToBlockSync is called by the state sync reactor when switching to fast
// sync.
func (r *Reactor) SwitchToBlockSync(ctx context.Context, state sm.State) error {
	r.blockSync.Set()
	r.initialState = state
	r.pool.height = state.LastBlockHeight + 1

	if err := r.pool.Start(ctx); err != nil {
		return err
	}

	r.syncStartTime = time.Now()

	go r.requestRoutine(ctx)
	go r.poolRoutine(ctx, true)

	if err := r.PublishStatus(types.EventDataBlockSyncStatus{
		Complete: false,
		Height:   state.LastBlockHeight,
	}); err != nil {
		return err
	}

	return nil
}

func (r *Reactor) requestRoutine(ctx context.Context) {
	statusUpdateTicker := time.NewTicker(statusUpdateInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-r.requestsCh:
			r.channel.Send(wrap(&pb.BlockRequest{Height: request.Height}), request.PeerID)
		case pErr := <-r.errorsCh:
			r.router.Evict(pErr.peerID, fmt.Errorf("blocksync.request: %w", pErr.err))
		case <-statusUpdateTicker.C:
			r.channel.Broadcast(wrap(&pb.StatusRequest{}))
		}
	}
}

// poolRoutine handles messages from the poolReactor telling the reactor what to
// do.
//
// NOTE: Don't sleep in the FOR_LOOP or otherwise slow it down!
func (r *Reactor) poolRoutine(ctx context.Context, stateSynced bool) {
	var (
		trySyncTicker           = time.NewTicker(trySyncIntervalMS * time.Millisecond)
		switchToConsensusTicker = time.NewTicker(switchToConsensusIntervalSeconds * time.Second)
		lastApplyBlockTime      = time.Now()

		blocksSynced = uint64(0)

		chainID = r.initialState.ChainID
		state   = r.initialState

		lastHundred = time.Now()
		lastRate    = 0.0

		didProcessCh = make(chan struct{}, 1)
	)

	defer trySyncTicker.Stop()
	defer switchToConsensusTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-switchToConsensusTicker.C:
			var (
				height, numPending, lenRequesters = r.pool.GetStatus()
				lastAdvance                       = r.pool.LastAdvance()
			)

			r.logger.Debug(
				"consensus ticker",
				"num_pending", numPending,
				"total", lenRequesters,
				"height", height,
			)

			switch {
			case r.pool.IsCaughtUp() && r.previousMaxPeerHeight <= r.pool.MaxPeerHeight():
				r.logger.Info("switching to consensus reactor after caught up", "height", height)

			case time.Since(lastAdvance) > syncTimeout:
				r.logger.Error("no progress since last advance", "last_advance", lastAdvance)
				continue

			default:
				r.logger.Info(
					"not caught up yet",
					"height", height,
					"max_peer_height", r.pool.MaxPeerHeight(),
					"timeout_in", syncTimeout-time.Since(lastAdvance),
				)
				continue
			}

			r.pool.Stop()

			r.blockSync.UnSet()

			if r.consReactor != nil {
				r.logger.Info("switching to consensus reactor", "height", height, "blocks_synced", blocksSynced, "state_synced", stateSynced, "max_peer_height", r.pool.MaxPeerHeight())
				r.consReactor.SwitchToConsensus(ctx, state, blocksSynced > 0 || stateSynced)

				// Auto restart should only be checked after switching to consensus mode
				go r.autoRestartIfBehind(ctx)
			}

			return

		case <-trySyncTicker.C:
			select {
			case didProcessCh <- struct{}{}:
			default:
			}
		case <-didProcessCh:
			// NOTE: It is a subtle mistake to process more than a single block at a
			// time (e.g. 10) here, because we only send one BlockRequest per loop
			// iteration. The ratio mismatch can result in starving of blocks, i.e. a
			// sudden burst of requests and responses, and repeat. Consequently, it is
			// better to split these routines rather than coupling them as it is
			// written here.
			//
			// TODO: Uncouple from request routine.

			// see if there are any blocks to sync
			first, second := r.pool.PeekTwoBlocks()
			if first == nil || second == nil {
				// we need to have fetched two consecutive blocks in order to perform blocksync verification
				continue
			}

			// try again quickly next loop
			didProcessCh <- struct{}{}

			firstParts, err := first.MakePartSet(types.BlockPartSizeBytes)
			if err != nil {
				r.logger.Error("failed to make ",
					"height", first.Height,
					"err", err.Error())
				return
			}

			var (
				firstPartSetHeader = firstParts.Header()
				firstID            = types.BlockID{Hash: first.Hash(), PartSetHeader: firstPartSetHeader}
			)

			// Finally, verify the first block using the second's commit.
			//
			// NOTE: We can probably make this more efficient, but note that calling
			// first.Hash() doesn't verify the tx contents, so MakePartSet() is
			// currently necessary.
			// TODO(sergio): Should we also validate against the extended commit?
			err = state.Validators.VerifyCommitLight(chainID, firstID, first.Height, second.LastCommit)

			if err == nil {
				// validate the block before we persist it
				err = r.blockExec.ValidateBlock(ctx, state, first)
			}
			// If either of the checks failed we log the error and request for a new block
			// at that height
			if err != nil {
				r.logger.Error(
					err.Error(),
					"last_commit", second.LastCommit,
					"block_id", firstID,
					"height", first.Height,
				)

				// NOTE: We've already removed the peer's request, but we still need
				// to clean up the rest.
				peerID := r.pool.RedoRequest(first.Height)
				r.router.Evict(peerID, fmt.Errorf("blocksync: %w", err))

				peerID2 := r.pool.RedoRequest(second.Height)
				if peerID2 != peerID {
					r.router.Evict(peerID2, fmt.Errorf("blocksync: %w", err))
				}
				return
			}

			r.pool.PopRequest()

			// We use LastCommit here instead of extCommit. extCommit is not
			// guaranteed to be populated by the peer if extensions are not enabled.
			// Currently, the peer should provide an extCommit even if the vote extension data are absent
			// but this may change so using second.LastCommit is safer.
			r.store.SaveBlock(first, firstParts, second.LastCommit)

			// TODO: Same thing for app - but we would need a way to get the hash
			// without persisting the state.
			r.logger.Info(fmt.Sprintf("Requesting block %d from peer took %s", first.Height, time.Since(lastApplyBlockTime)))
			startTime := time.Now()
			state, err = r.blockExec.ApplyBlock(ctx, state, firstID, first, nil)
			r.logger.Info(fmt.Sprintf("ApplyBlock %d took %s", first.Height, time.Since(startTime)))
			lastApplyBlockTime = time.Now()
			if err != nil {
				panic(fmt.Sprintf("failed to process committed block (%d:%X): %v", first.Height, first.Hash(), err))
			}

			r.metrics.RecordConsMetrics(first)

			blocksSynced++

			if blocksSynced%100 == 0 {
				lastRate = 0.9*lastRate + 0.1*(100/time.Since(lastHundred).Seconds())
				r.logger.Info(
					"block sync rate",
					"height", r.pool.height,
					"max_peer_height", r.pool.MaxPeerHeight(),
					"blocks/s", lastRate,
				)

				lastHundred = time.Now()
			}
		}
	}
}

func (r *Reactor) GetMaxPeerBlockHeight() int64 {
	return r.pool.MaxPeerHeight()
}

func (r *Reactor) GetTotalSyncedTime() time.Duration {
	if !r.blockSync.IsSet() || r.syncStartTime.IsZero() {
		return time.Duration(0)
	}
	return time.Since(r.syncStartTime)
}

func (r *Reactor) GetRemainingSyncTime() time.Duration {
	if !r.blockSync.IsSet() {
		return time.Duration(0)
	}

	targetSyncs := r.pool.targetSyncBlocks()
	currentSyncs := r.store.Height() - r.pool.startHeight + 1
	lastSyncRate := r.pool.getLastSyncRate()
	if currentSyncs < 0 || lastSyncRate < 0.001 {
		return time.Duration(0)
	}

	remain := float64(targetSyncs-currentSyncs) / lastSyncRate

	return time.Duration(int64(remain * float64(time.Second)))
}

func (r *Reactor) PublishStatus(event types.EventDataBlockSyncStatus) error {
	if r.eventBus == nil {
		return errors.New("event bus is not configured")
	}
	return r.eventBus.PublishEventBlockSyncStatus(event)
}

// atomicBool is an atomic Boolean, safe for concurrent use by multiple
// goroutines.
type atomicBool int32

// newAtomicBool creates an atomicBool with given initial value.
func newAtomicBool(ok bool) *atomicBool {
	ab := new(atomicBool)
	if ok {
		ab.Set()
	}
	return ab
}

// Set sets the Boolean to true.
func (ab *atomicBool) Set() { atomic.StoreInt32((*int32)(ab), 1) }

// UnSet sets the Boolean to false.
func (ab *atomicBool) UnSet() { atomic.StoreInt32((*int32)(ab), 0) }

// IsSet returns whether the Boolean is true.
func (ab *atomicBool) IsSet() bool { return atomic.LoadInt32((*int32)(ab))&1 == 1 }
