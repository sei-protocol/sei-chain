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
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/blocksync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var _ service.Service = (*Reactor)(nil)

const (
	// BlockSyncChannel is a channel for blocks and status updates.
	BlockSyncChannel = p2p.ChannelID(0x40)

	trySyncIntervalMS = 10

	// ask for best height every 10s
	statusUpdateInterval = 10 * time.Second

	// check if we should switch to consensus reactor
	switchToConsensusIntervalSeconds = 1

	// switch to consensus after this duration of inactivity
	syncTimeout = 180 * time.Second
)

// Metricer is the RPC-facing blocksync surface. The facade and any future
// replacement can expose sync progress without leaking the concrete type.
type Metricer interface {
	GetMaxPeerBlockHeight() int64
	GetTotalSyncedTime() time.Duration
	GetRemainingSyncTime() time.Duration
}

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
		PreDecode:           utils.Some(pb.SchemaForMessage.Scan),
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
	SwitchToConsensus(state sm.State, skipWAL bool)
}

type peerError struct {
	err    error
	peerID types.NodeID
}

func (e peerError) Error() string {
	return fmt.Sprintf("error with peer %v: %s", e.peerID, e.err.Error())
}

type blocksyncResult struct {
	stateSynced bool
	state       sm.State
	syncStartAt time.Time
}

type syncState struct {
	initialState sm.State
}

// SyncerConfig groups dependencies and startup knobs used only by the active
// blocksync controller. Reactor itself does not need these when running as an
// always-on query responder only.
type SyncerConfig struct {
	BlockExec             *sm.BlockExecutor
	ConsReactor           consensusReactor
	BlockSync             bool
	Metrics               *consensus.Metrics
	EventBus              *eventbus.EventBus
	RestartEvent          func()
	SelfRemediationConfig *config.SelfRemediationConfig
}

// Reactor owns the blocksync channel and always-on query serving path, while
// delegating active sync responsibilities to a separate sync controller.
type Reactor struct {
	service.BaseService

	// stateStore and store back both the query-serving path and the sync
	// controller. They stay on the facade because inbound requests are served
	// directly from Reactor even when local blocksync is inactive.
	stateStore sm.Store
	store      sm.BlockStore

	// Reactor is the sole owner of the blocksync channel because the router
	// allows a channel ID to be opened only once.
	router  *p2p.Router
	channel *p2p.Channel[*pb.Message]

	// syncer owns all active catch-up responsibilities: pool management,
	// outgoing requests, block execution, consensus handoff, and lag metrics.
	syncer utils.Option[*syncController]
}

type syncController struct {
	// Immutable dependencies for the active sync path.
	stateStore  sm.Store
	blockExec   *sm.BlockExecutor
	store       sm.BlockStore
	router      *p2p.Router
	channel     *p2p.Channel[*pb.Message]
	consReactor consensusReactor
	metrics     *consensus.Metrics
	eventBus    *eventbus.EventBus

	// Mutable sync state initialized on start and updated as blocksync runs.
	pool *BlockPool

	// blockSync tracks whether the node is actively in blocksync mode. The
	// channel responder stays up regardless of this flag.
	blockSync             atomic.Bool
	previousMaxPeerHeight int64

	// Auto-remediation configuration and restart bookkeeping.
	restartEvent              func()
	lastRestartTime           time.Time
	blocksBehindThreshold     uint64
	blocksBehindCheckInterval time.Duration
	restartCooldownSeconds    uint64

	// blocksyncReady fires when the active sync routines should begin processing
	// work, either during OnStart or later via SwitchToBlockSync.
	blocksyncReady utils.AtomicSend[utils.Option[blocksyncResult]]
	// consensusReady fires after blocksync hands off to consensus so the
	// auto-restart monitor can start observing lag from that point forward.
	consensusReady utils.AtomicSend[bool]
}

// NewReactor returns new reactor instance.
func NewReactor(
	stateStore sm.Store,
	store *store.BlockStore,
	router *p2p.Router,
	syncerConfig utils.Option[SyncerConfig],
) (*Reactor, error) {
	channel, err := p2p.OpenChannel(router, GetChannelDescriptor())
	if err != nil {
		return nil, fmt.Errorf("router.AddChannel(): %w", err)
	}

	syncer := utils.None[*syncController]()
	if cfg, ok := syncerConfig.Get(); ok {
		s := &syncController{
			stateStore:                stateStore,
			blockExec:                 cfg.BlockExec,
			store:                     store,
			router:                    router,
			channel:                   channel,
			consReactor:               cfg.ConsReactor,
			metrics:                   cfg.Metrics,
			eventBus:                  cfg.EventBus,
			restartEvent:              cfg.RestartEvent,
			lastRestartTime:           time.Now(),
			blocksBehindThreshold:     cfg.SelfRemediationConfig.BlocksBehindThreshold,
			blocksBehindCheckInterval: time.Duration(cfg.SelfRemediationConfig.BlocksBehindCheckIntervalSeconds) * time.Second, //nolint:gosec // validated in config.ValidateBasic against MaxInt64
			restartCooldownSeconds:    cfg.SelfRemediationConfig.RestartCooldownSeconds,
			blocksyncReady:            utils.NewAtomicSend(utils.None[blocksyncResult]()),
			consensusReady:            utils.NewAtomicSend(false),
		}
		if cfg.BlockSync {
			s.blockSync.Store(true)
		}
		syncer = utils.Some(s)
	}

	r := &Reactor{
		stateStore: stateStore,
		store:      store,
		router:     router,
		channel:    channel,
		syncer:     syncer,
	}
	r.BaseService = *service.NewBaseService("BlockSync", r)
	return r, nil
}

// OnStart starts the always-on query handling loops and one sync controller
// supervisor task. The active sync routines inside that controller remain
// gated until blocksync is enabled, either on startup or via
// SwitchToBlockSync after state sync.
func (r *Reactor) OnStart(ctx context.Context) error {
	state, err := r.stateStore.Load()
	if err != nil {
		return err
	}
	if state.LastBlockHeight != r.store.Height() {
		return fmt.Errorf("state (%v) and store (%v) height mismatch", state.LastBlockHeight, r.store.Height())
	}

	startHeight := r.store.Height() + 1
	if startHeight == 1 {
		startHeight = state.InitialHeight
	}

	r.SpawnCritical("processBlockSyncCh", func(ctx context.Context) error {
		r.processBlockSyncCh(ctx)
		return nil
	})
	r.SpawnCritical("processPeerUpdates", func(ctx context.Context) error {
		r.processPeerUpdates(ctx)
		return nil
	})
	if syncer, ok := r.syncer.Get(); ok {
		r.SpawnCritical("syncController.run", func(ctx context.Context) error {
			return syncer.run(ctx)
		})
		if syncer.blockSync.Load() {
			syncer.blocksyncReady.Store(utils.Some(blocksyncResult{
				stateSynced: false,
				state:       state,
				syncStartAt: time.Now(),
			}))
		}
	}
	return nil
}

// OnStop relies on the query loops and sync controller supervisor being
// registered with BaseService via Spawn. Their internal cleanup runs as those
// tasks exit.
func (r *Reactor) OnStop() {}

// SwitchToBlockSync is called by the state sync reactor when switching to fast
// sync.
func (r *Reactor) SwitchToBlockSync(state sm.State) error {
	syncer, ok := r.syncer.Get()
	if !ok {
		return errors.New("blocksync syncer is not configured")
	}
	return syncer.switchToBlockSync(state)
}

func (r *Reactor) GetMaxPeerBlockHeight() int64 {
	if syncer, ok := r.syncer.Get(); ok {
		return syncer.GetMaxPeerBlockHeight()
	}
	return 0
}

func (r *Reactor) GetTotalSyncedTime() time.Duration {
	if syncer, ok := r.syncer.Get(); ok {
		return syncer.GetTotalSyncedTime()
	}
	return 0
}

func (r *Reactor) GetRemainingSyncTime() time.Duration {
	if syncer, ok := r.syncer.Get(); ok {
		return syncer.GetRemainingSyncTime()
	}
	return 0
}

// respondToPeer loads a block and sends it to the requesting peer, if we have it.
// Otherwise, it responds saying we do not have it.
func (r *Reactor) respondToPeer(msg *pb.BlockRequest, peerID types.NodeID) error {
	block := r.store.LoadBlock(msg.GetHeight())
	if block == nil {
		logger.Info("peer requesting a block we do not have", "peer", peerID, "height", msg.GetHeight())
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

// handleMessage handles an inbound blocksync message. Reactor only owns block
// request serving; every other blocksync message is forwarded to the sync
// controller.
func (r *Reactor) handleMessage(m p2p.RecvMsg[*pb.Message]) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("panic in processing message: %v", e)
			logger.Error(
				"recovering from processing message panic",
				"err", err,
				"stack", string(debug.Stack()),
			)
		}
	}()

	logger.Debug("received message", "message", m.Message, "peer", m.From)

	if msg, ok := m.Message.Sum.(*pb.Message_BlockRequest); ok {
		return r.respondToPeer(msg.BlockRequest, m.From)
	}
	syncer, ok := r.syncer.Get()
	if !ok {
		return nil
	}
	return syncer.handleMessage(m)
}

// processBlockSyncCh listens for messages on the shared blocksync channel.
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

// processPeerUpdate handles the subset of peer lifecycle needed by blocksync:
// advertise our local range on connect and let the sync controller clean up any
// in-flight state on disconnect.
func (r *Reactor) processPeerUpdate(peerUpdate p2p.PeerUpdate) {
	logger.Debug("received peer update", "peer", peerUpdate.NodeID, "status", peerUpdate.Status)

	switch peerUpdate.Status {
	case p2p.PeerStatusUp:
		r.channel.Send(wrap(&pb.StatusResponse{
			Base:   r.store.Base(),
			Height: r.store.Height(),
		}), peerUpdate.NodeID)
	case p2p.PeerStatusDown:
		if syncer, ok := r.syncer.Get(); ok {
			syncer.handlePeerDown(peerUpdate.NodeID)
		}
	}
}

// processPeerUpdates listens for peer updates. The reactor owns peer-up
// status announcements; the sync controller only receives peer-down callbacks
// for pool cleanup.
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

// run owns the active sync controller's internal concurrency. A single
// coordinator task waits for blocksyncReady, then starts the pool and spawns
// the active sync subtasks for that session.
func (s *syncController) run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, sc scope.Scope) error {
		sc.SpawnNamed("blocksyncSession", func() error {
			result, err := s.blocksyncReady.Wait(ctx, func(o utils.Option[blocksyncResult]) bool {
				return o.IsPresent()
			})
			if err != nil {
				return err
			}
			res := result.OrPanic("no blocksync result")
			s.lastRestartTime = time.Now()

			state := syncState{
				initialState: res.state,
			}
			s.pool = NewBlockPool(startHeightForState(res.state), s.router)

			return scope.Run(ctx, func(ctx context.Context, session scope.Scope) error {
				session.SpawnNamed("pool.run", func() error {
					return s.pool.run(ctx)
				})
				session.SpawnNamed("requestRoutine", func() error {
					s.requestRoutine(ctx, state)
					return nil
				})
				session.SpawnNamed("poolRoutine", func() error {
					s.poolRoutine(ctx, state, res.stateSynced)
					return nil
				})
				return nil
			})
		})
		sc.SpawnNamed("autoRestartIfBehind", func() error {
			if _, err := s.consensusReady.Wait(ctx, func(ready bool) bool { return ready }); err != nil {
				return err
			}
			s.autoRestartIfBehind(ctx)
			return nil
		})
		return nil
	})
}

// handleMessage processes all non-BlockRequest blocksync protocol messages.
func (s *syncController) handleMessage(m p2p.RecvMsg[*pb.Message]) error {
	switch msg := m.Message.Sum.(type) {
	case *pb.Message_BlockResponse:
		block, err := types.BlockFromProto(msg.BlockResponse.GetBlock())
		if err != nil {
			return fmt.Errorf("types.BlockFromProto(): %w", err)
		}
		return s.handleBlockResponse(m.From, block)
	case *pb.Message_StatusRequest:
		s.channel.Send(wrap(&pb.StatusResponse{
			Height: s.store.Height(),
			Base:   s.store.Base(),
		}), m.From)
		return nil
	case *pb.Message_StatusResponse:
		s.handleStatusResponse(m.From, msg.StatusResponse.GetBase(), msg.StatusResponse.GetHeight())
		return nil
	case *pb.Message_NoBlockResponse:
		s.handleNoBlockResponse(m.From, msg.NoBlockResponse.GetHeight())
		return nil
	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}
}

func (s *syncController) handleBlockResponse(peerID types.NodeID, block *types.Block) error {
	logger.Info("received block response from peer", "peer", peerID, "height", block.Height)
	if err := s.pool.AddBlock(peerID, block, block.Size()); err != nil {
		logger.Error("failed to add block", "err", err)
	}
	return nil
}

func (s *syncController) handleStatusResponse(peerID types.NodeID, base, height int64) {
	s.pool.SetPeerRange(peerID, base, height)
}

func (s *syncController) handleNoBlockResponse(peerID types.NodeID, height int64) {
	logger.Debug("peer does not have the requested block", "peer", peerID, "height", height)
}

func (s *syncController) handlePeerDown(peerID types.NodeID) {
	s.pool.RemovePeer(peerID)
}

func (s *syncController) switchToBlockSync(state sm.State) error {
	s.blockSync.Store(true)
	s.blocksyncReady.Store(utils.Some(blocksyncResult{
		stateSynced: true,
		state:       state,
		syncStartAt: time.Now(),
	}))

	if err := s.PublishStatus(types.EventDataBlockSyncStatus{
		Complete: false,
		Height:   state.LastBlockHeight,
	}); err != nil {
		return err
	}

	return nil
}

func (s *syncController) requestRoutine(ctx context.Context, state syncState) {
	statusUpdateTicker := time.NewTicker(statusUpdateInterval)
	defer statusUpdateTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case request := <-s.pool.Requests():
			s.channel.Send(wrap(&pb.BlockRequest{Height: request.Height}), request.PeerID)
		case pErr := <-s.pool.Errors():
			s.router.Evict(pErr.peerID, fmt.Errorf("blocksync.request: %w", pErr.err))
		case <-statusUpdateTicker.C:
			s.channel.Broadcast(wrap(&pb.StatusRequest{}))
		}
	}
}

// poolRoutine handles messages from the poolReactor telling the controller what
// to do.
//
// NOTE: Don't sleep in the FOR_LOOP or otherwise slow it down!
func (s *syncController) poolRoutine(ctx context.Context, syncState syncState, stateSynced bool) {
	var (
		trySyncTicker           = time.NewTicker(trySyncIntervalMS * time.Millisecond)
		switchToConsensusTicker = time.NewTicker(switchToConsensusIntervalSeconds * time.Second)
		lastApplyBlockTime      = time.Now()

		blocksSynced = uint64(0)

		chainID = syncState.initialState.ChainID
		state   = syncState.initialState

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
				height, numPending, lenRequesters = s.pool.GetStatus()
				lastAdvance                       = s.pool.LastAdvance()
			)

			logger.Debug(
				"consensus ticker",
				"num_pending", numPending,
				"total", lenRequesters,
				"height", height,
			)

			switch {
			case s.pool.IsCaughtUp() && s.previousMaxPeerHeight <= s.pool.MaxPeerHeight():
				logger.Info("switching to consensus reactor after caught up", "height", height)
			case time.Since(lastAdvance) > syncTimeout:
				logger.Error("no progress since last advance", "last_advance", lastAdvance)
				continue
			default:
				logger.Info(
					"not caught up yet",
					"height", height,
					"max_peer_height", s.pool.MaxPeerHeight(),
					"timeout_in", syncTimeout-time.Since(lastAdvance),
				)
				continue
			}

			s.blockSync.Store(false)

			if s.consReactor != nil {
				logger.Info("switching to consensus reactor", "height", height, "blocks_synced", blocksSynced, "state_synced", stateSynced, "max_peer_height", s.pool.MaxPeerHeight())
				s.consReactor.SwitchToConsensus(state, blocksSynced > 0 || stateSynced)
				s.consensusReady.Store(true)
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
			// iteration. The ratio mismatch can result in starving of blocks.
			first, second := s.pool.PeekTwoBlocks()
			if first == nil || second == nil {
				continue
			}

			didProcessCh <- struct{}{}

			firstParts, err := first.MakePartSet(types.BlockPartSizeBytes)
			if err != nil {
				logger.Error("failed to make ", "height", first.Height, "err", err)
				return
			}

			firstID := types.BlockID{Hash: first.Hash(), PartSetHeader: firstParts.Header()}

			err = state.Validators.VerifyCommitLight(chainID, firstID, first.Height, second.LastCommit)
			if err == nil {
				err = s.blockExec.ValidateBlock(ctx, state, first)
			}
			if err != nil {
				logger.Error(
					"Failed to validate block or verify commit",
					"last_commit", second.LastCommit,
					"block_id", firstID,
					"height", first.Height,
					"err", err,
				)

				peerID := s.pool.RedoRequest(first.Height)
				s.router.Evict(peerID, fmt.Errorf("blocksync: %w", err))

				peerID2 := s.pool.RedoRequest(second.Height)
				if peerID2 != peerID {
					s.router.Evict(peerID2, fmt.Errorf("blocksync: %w", err))
				}
				return
			}

			s.pool.PopRequest()
			s.store.SaveBlock(first, firstParts, second.LastCommit)

			logger.Info("Requesting block from peer", "block", first.Height, "took", time.Since(lastApplyBlockTime))
			startTime := time.Now()
			state, err = s.blockExec.ApplyBlock(ctx, state, firstID, first, nil)
			logger.Info("ApplyBlock", "block", first.Height, "took", time.Since(startTime))
			lastApplyBlockTime = time.Now()
			if err != nil {
				panic(fmt.Sprintf("failed to process committed block (%d:%X): %v", first.Height, first.Hash(), err))
			}

			s.metrics.RecordConsMetrics(first)
			blocksSynced++

			if blocksSynced%100 == 0 {
				lastRate = 0.9*lastRate + 0.1*(100/time.Since(lastHundred).Seconds())
				logger.Info(
					"block sync rate",
					"height", s.pool.height,
					"max_peer_height", s.pool.MaxPeerHeight(),
					"blocks/s", lastRate,
				)
				lastHundred = time.Now()
			}
		}
	}
}

// autoRestartIfBehind will check if the node is behind the max peer height by
// a certain threshold. If it is, the node will attempt to restart itself.
// TODO(gprusak): this should be a sub task of the consensus reactor instead.
func (s *syncController) autoRestartIfBehind(ctx context.Context) {
	if s.blocksBehindThreshold == 0 || s.blocksBehindCheckInterval <= 0 {
		logger.Info("Auto remediation is disabled")
		return
	}

	logger.Info("checking if node is behind threshold, auto restarting if its behind", "threshold", s.blocksBehindThreshold, "interval", s.blocksBehindCheckInterval)
	for {
		select {
		case <-time.After(s.blocksBehindCheckInterval):
			selfHeight := s.store.Height()
			maxPeerHeight := s.pool.MaxPeerHeight()
			threshold := int64(s.blocksBehindThreshold) //nolint:gosec // validated in config.ValidateBasic against MaxInt64
			behindHeight := maxPeerHeight - selfHeight
			blockSyncIsSet := s.blockSync.Load()
			if maxPeerHeight > s.previousMaxPeerHeight {
				s.previousMaxPeerHeight = maxPeerHeight
			}

			if maxPeerHeight == 0 || behindHeight < threshold || blockSyncIsSet {
				logger.Debug("does not exceed threshold or is already in block sync mode", "threshold", threshold, "behindHeight", behindHeight, "maxPeerHeight", maxPeerHeight, "selfHeight", selfHeight, "blockSyncIsSet", blockSyncIsSet)
				continue
			}
			if time.Since(s.lastRestartTime).Seconds() < float64(s.restartCooldownSeconds) {
				logger.Debug("we are lagging behind, going to trigger a restart after cooldown time passes")
				continue
			}
			logger.Info("Blocks behind threshold, restarting node", "threshold", threshold, "behindHeight", behindHeight, "maxPeerHeight", maxPeerHeight, "selfHeight", selfHeight)

			s.blockSync.Store(true)
			s.restartEvent()
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *syncController) GetMaxPeerBlockHeight() int64 {
	if s.pool == nil {
		return 0
	}
	return s.pool.MaxPeerHeight()
}

func (s *syncController) GetTotalSyncedTime() time.Duration {
	if !s.blockSync.Load() {
		return time.Duration(0)
	}
	result, ok := s.blocksyncReady.Load().Get()
	if !ok || result.syncStartAt.IsZero() {
		return time.Duration(0)
	}
	return time.Since(result.syncStartAt)
}

func (s *syncController) GetRemainingSyncTime() time.Duration {
	if !s.blockSync.Load() || s.pool == nil {
		return time.Duration(0)
	}

	targetSyncs := s.pool.targetSyncBlocks()
	currentSyncs := s.store.Height() - s.pool.startHeight + 1
	lastSyncRate := s.pool.getLastSyncRate()
	if currentSyncs < 0 || lastSyncRate < 0.001 {
		return time.Duration(0)
	}

	remain := float64(targetSyncs-currentSyncs) / lastSyncRate
	return time.Duration(int64(remain * float64(time.Second)))
}

func (s *syncController) PublishStatus(event types.EventDataBlockSyncStatus) error {
	if s.eventBus == nil {
		return errors.New("event bus is not configured")
	}
	return s.eventBus.PublishEventBlockSyncStatus(event)
}

func startHeightForState(state sm.State) int64 {
	startHeight := state.LastBlockHeight + 1
	if startHeight == 1 {
		startHeight = state.InitialHeight
	}
	return startHeight
}
