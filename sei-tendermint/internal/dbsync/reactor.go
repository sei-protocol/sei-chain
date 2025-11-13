package dbsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/internal/eventbus"
	"github.com/tendermint/tendermint/internal/p2p"
	sm "github.com/tendermint/tendermint/internal/state"
	"github.com/tendermint/tendermint/internal/store"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/service"
	"github.com/tendermint/tendermint/light"
	"github.com/tendermint/tendermint/light/provider"
	dstypes "github.com/tendermint/tendermint/proto/tendermint/dbsync"
	"github.com/tendermint/tendermint/types"
)

const (
	// MetadataChannel exchanges DB metadata
	MetadataChannel = p2p.ChannelID(0x70)

	// FileChannel exchanges file data
	FileChannel = p2p.ChannelID(0x71)

	LightBlockChannel = p2p.ChannelID(0x72)

	ParamsChannel = p2p.ChannelID(0x73)

	metadataMsgSize = int(4e6) // ~4MB

	fileMsgSize = int(16e6) // ~16MB

	lightBlockMsgSize = int(1e7) // ~1MB

	paramMsgSize = int(1e5) // ~100kb

	MetadataHeightFilename   = "LATEST_HEIGHT"
	HeightSubdirectoryPrefix = "snapshot_"
	MetadataFilename         = "METADATA"
)

func GetMetadataChannelDescriptor() p2p.ChannelDescriptor {
	return p2p.ChannelDescriptor{
		ID:                  MetadataChannel,
		MessageType:         new(dstypes.Message),
		Priority:            6,
		SendQueueCapacity:   10,
		RecvMessageCapacity: metadataMsgSize,
		RecvBufferCapacity:  128,
		Name:                "metadata",
	}
}

func GetFileChannelDescriptor() p2p.ChannelDescriptor {
	return p2p.ChannelDescriptor{
		ID:                  FileChannel,
		Priority:            3,
		MessageType:         new(dstypes.Message),
		SendQueueCapacity:   4,
		RecvMessageCapacity: fileMsgSize,
		RecvBufferCapacity:  128,
		Name:                "chunk",
	}
}

func GetLightBlockChannelDescriptor() p2p.ChannelDescriptor {
	return p2p.ChannelDescriptor{
		ID:                  LightBlockChannel,
		MessageType:         new(dstypes.Message),
		Priority:            5,
		SendQueueCapacity:   10,
		RecvMessageCapacity: lightBlockMsgSize,
		RecvBufferCapacity:  128,
		Name:                "light-block",
	}
}

func GetParamsChannelDescriptor() p2p.ChannelDescriptor {
	return p2p.ChannelDescriptor{
		ID:                  ParamsChannel,
		MessageType:         new(dstypes.Message),
		Priority:            2,
		SendQueueCapacity:   10,
		RecvMessageCapacity: paramMsgSize,
		RecvBufferCapacity:  128,
		Name:                "params",
	}
}

type Reactor struct {
	service.BaseService
	logger log.Logger

	// Dispatcher is used to multiplex light block requests and responses over multiple
	// peers used by the p2p state provider and in reverse sync.
	dispatcher    *light.Dispatcher
	peers         *light.PeerList
	stateStore    sm.Store
	blockStore    *store.BlockStore
	initialHeight int64
	shouldSync    bool

	chainID       string
	config        config.DBSyncConfig
	providers     map[types.NodeID]*light.BlockProvider
	stateProvider light.StateProvider

	metadataChannel   *p2p.Channel
	fileChannel       *p2p.Channel
	lightBlockChannel *p2p.Channel
	paramsChannel     *p2p.Channel

	router   *p2p.Router
	eventBus *eventbus.EventBus

	syncer *Syncer

	mtx sync.RWMutex

	postSyncHook func(context.Context, sm.State) error
}

func NewReactor(
	logger log.Logger,
	config config.DBSyncConfig,
	baseConfig config.BaseConfig,
	router *p2p.Router,
	stateStore sm.Store,
	blockStore *store.BlockStore,
	initialHeight int64,
	chainID string,
	eventBus *eventbus.EventBus,
	shouldSync bool,
	postSyncHook func(context.Context, sm.State) error,
) (*Reactor, error) {
	metadataCh, err := router.OpenChannel(GetMetadataChannelDescriptor())
	if err != nil {
		return nil, fmt.Errorf("router.OpenChannel(metadata): %w", err)
	}
	fileCh, err := router.OpenChannel(GetFileChannelDescriptor())
	if err != nil {
		return nil, fmt.Errorf("router.OpenChannel(file): %w", err)
	}
	lightBlockCh, err := router.OpenChannel(GetLightBlockChannelDescriptor())
	if err != nil {
		return nil, fmt.Errorf("router.OpenChannel(lightBlock): %w", err)
	}
	paramsCh, err := router.OpenChannel(GetParamsChannelDescriptor())
	if err != nil {
		return nil, fmt.Errorf("router.OpenChannel(params): %w", err)
	}
	reactor := &Reactor{
		logger:            logger,
		router:            router,
		metadataChannel:   metadataCh,
		fileChannel:       fileCh,
		lightBlockChannel: lightBlockCh,
		paramsChannel:     paramsCh,
		peers:             light.NewPeerList(),
		stateStore:        stateStore,
		blockStore:        blockStore,
		initialHeight:     initialHeight,
		chainID:           chainID,
		providers:         make(map[types.NodeID]*light.BlockProvider),
		eventBus:          eventBus,
		config:            config,
		postSyncHook:      postSyncHook,
		shouldSync:        shouldSync,
	}
	syncer := NewSyncer(logger, config, baseConfig, shouldSync, reactor.requestMetadata, reactor.requestFile, reactor.commitState, reactor.postSync, defaultResetDirFn)
	reactor.syncer = syncer

	reactor.BaseService = *service.NewBaseService(logger, "DBSync", reactor)
	return reactor, nil
}

func (r *Reactor) OnStart(ctx context.Context) error {
	go r.processPeerUpdates(ctx)
	r.dispatcher = light.NewDispatcher(r.lightBlockChannel, func(height uint64) proto.Message {
		return &dstypes.LightBlockRequest{
			Height: height,
		}
	})
	go r.processMetadataCh(ctx, r.metadataChannel)
	go r.processFileCh(ctx, r.fileChannel)
	go r.processLightBlockCh(ctx, r.lightBlockChannel)
	go r.processParamsCh(ctx, r.paramsChannel)
	if r.shouldSync {
		to := light.TrustOptions{
			Period: r.config.TrustPeriod,
			Height: r.config.TrustHeight,
			Hash:   r.config.TrustHashBytes(),
		}
		r.logger.Info("begin waiting for at least 2 peers")
		if err := r.waitForEnoughPeers(ctx, 2); err != nil {
			return err
		}
		r.logger.Info("enough peers discovered")

		peers := r.peers.All()
		providers := make([]provider.Provider, len(peers))
		for idx, p := range peers {
			providers[idx] = light.NewBlockProvider(p, r.chainID, r.dispatcher)
		}

		stateProvider, err := light.NewP2PStateProvider(ctx, r.chainID, r.initialHeight, r.config.VerifyLightBlockTimeout, providers, to, r.paramsChannel, r.logger.With("module", "stateprovider"), r.config.BlacklistTTL, func(height uint64) proto.Message {
			return &dstypes.ParamsRequest{
				Height: height,
			}
		})
		if err != nil {
			return fmt.Errorf("failed to initialize P2P state provider: %w", err)
		}
		r.stateProvider = stateProvider
	}

	go r.syncer.Process(ctx)
	return nil
}

func (r *Reactor) OnStop() {
	// tell the dispatcher to stop sending any more requests
	r.dispatcher.Close()
	// clear up half-populated directories
	r.syncer.Stop()
}

func (r *Reactor) handleMetadataRequest(from types.NodeID) (err error) {
	responded := false
	defer func() {
		if err != nil {
			r.logger.Debug(fmt.Sprintf("handle metadata request encountered error %s", err))
		}
		if !responded {
			r.metadataChannel.Send(&dstypes.MetadataResponse{
				Height:    0,
				Hash:      []byte{},
				Filenames: []string{},
			}, from)
			err = nil
		}
	}()

	if r.config.SnapshotDirectory == "" {
		return nil
	}

	metadataHeightFile := filepath.Join(r.config.SnapshotDirectory, MetadataHeightFilename)
	heightData, err := os.ReadFile(metadataHeightFile)
	if err != nil {
		return fmt.Errorf("cannot read height file %s due to %s", metadataHeightFile, err)
	}
	height, err := strconv.ParseUint(string(heightData), 10, 64)
	if err != nil {
		return fmt.Errorf("height data should be an integer but got %s", heightData)
	}
	heightSubdirectory := filepath.Join(r.config.SnapshotDirectory, fmt.Sprintf("%s%d", HeightSubdirectoryPrefix, height))
	metadataFilename := filepath.Join(heightSubdirectory, MetadataFilename)
	data, err := os.ReadFile(metadataFilename)
	if err != nil {
		return fmt.Errorf("cannot read metadata file %s due to %s", metadataFilename, err)
	}
	msg := dstypes.MetadataResponse{}
	if err := msg.Unmarshal(data); err != nil {
		return fmt.Errorf("cannot unmarshal metadata file %s due to %s", metadataFilename, err)
	}
	r.metadataChannel.Send(&msg, from)
	responded = true
	return nil
}

func (r *Reactor) handleMetadataMessage(ctx context.Context, m p2p.RecvMsg) error {
	logger := r.logger.With("peer", m.From)

	switch msg := m.Message.(type) {
	case *dstypes.MetadataRequest:
		return r.handleMetadataRequest(m.From)

	case *dstypes.MetadataResponse:
		if msg.Height == 0 {
			return nil
		}
		logger.Info("received metadata", "height", msg.Height, "size", len(msg.Filenames))
		r.syncer.SetMetadata(ctx, m.From, msg)

	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}

	return nil
}

func (r *Reactor) handleFileRequest(req *dstypes.FileRequest, from types.NodeID) (err error) {
	responded := false
	defer func() {
		if err != nil {
			r.logger.Debug(fmt.Sprintf("handle file request encountered error %s", err))
		}
		if !responded {
			r.fileChannel.Send(&dstypes.FileResponse{
				Height:   0,
				Filename: "",
				Data:     []byte{},
			}, from)
			err = nil
		}
	}()

	if r.config.SnapshotDirectory == "" {
		return nil
	}

	heightSubdirectory := filepath.Join(r.config.SnapshotDirectory, fmt.Sprintf("%s%d", HeightSubdirectoryPrefix, req.Height))
	filename := filepath.Join(heightSubdirectory, req.Filename)
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("cannot read file %s due to %s", filename, err)
	}
	r.fileChannel.Send(&dstypes.FileResponse{
		Height:   req.Height,
		Filename: req.Filename,
		Data:     data,
	}, from)
	responded = true
	return nil
}

func (r *Reactor) handleFileMessage(m p2p.RecvMsg) error {
	switch msg := m.Message.(type) {
	case *dstypes.FileRequest:
		return r.handleFileRequest(msg, m.From)

	case *dstypes.FileResponse:
		// using msg.Height is a more reliable check for empty response than
		// check msg.Data since it's valid to have empty files sync'ed over
		if msg.Height == 0 {
			return nil
		}
		r.syncer.PushFile(msg)

	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}

	return nil
}

func (r *Reactor) handleLightBlockMessage(ctx context.Context, envelope p2p.RecvMsg) error {
	switch msg := envelope.Message.(type) {
	case *dstypes.LightBlockRequest:
		lb, err := r.fetchLightBlock(msg.Height)
		if err != nil {
			r.logger.Error("failed to retrieve light block", "err", err, "height", msg.Height)
			return err
		}
		if lb == nil {
			r.lightBlockChannel.Send(&dstypes.LightBlockResponse{LightBlock: nil}, envelope.From)
			return nil
		}

		lbproto, err := lb.ToProto()
		if err != nil {
			r.logger.Error("marshaling light block to proto", "err", err)
			return nil
		}

		// NOTE: If we don't have the light block we will send a nil light block
		// back to the requested node, indicating that we don't have it.
		r.lightBlockChannel.Send(&dstypes.LightBlockResponse{LightBlock: lbproto}, envelope.From)

	case *dstypes.LightBlockResponse:
		var height int64
		if msg.LightBlock != nil {
			height = msg.LightBlock.SignedHeader.Header.Height
		}
		if err := r.dispatcher.Respond(ctx, msg.LightBlock, envelope.From); err != nil {
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

func (r *Reactor) handleParamsMessage(ctx context.Context, m p2p.RecvMsg) error {
	switch msg := m.Message.(type) {
	case *dstypes.ParamsRequest:
		cp, err := r.stateStore.LoadConsensusParams(int64(msg.Height))
		if err != nil {
			r.logger.Error("failed to fetch requested consensus params", "err", err, "height", msg.Height)
			return nil
		}

		cpproto := cp.ToProto()
		r.paramsChannel.Send(&dstypes.ParamsResponse{
			Height:          msg.Height,
			ConsensusParams: cpproto,
		}, m.From)
	case *dstypes.ParamsResponse:
		r.mtx.RLock()
		defer r.mtx.RUnlock()

		cp := types.ConsensusParamsFromProto(msg.ConsensusParams)

		if sp, ok := r.stateProvider.(*light.StateProviderP2P); ok {
			select {
			case sp.ParamsRecvCh() <- cp:
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
				r.logger.Error("failed to send consensus params, stateprovider not ready for response")
			}
		} else {
			r.logger.Debug("received unexpected params response; using RPC state provider", "peer", m.From)
		}

	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}

	return nil
}

func (r *Reactor) processPeerUpdate(peerUpdate p2p.PeerUpdate) {
	r.logger.Debug("received peer update", "peer", peerUpdate.NodeID, "status", peerUpdate.Status)

	switch peerUpdate.Status {
	case p2p.PeerStatusUp:
		if peerUpdate.Channels.Contains(MetadataChannel) && peerUpdate.Channels.Contains(FileChannel) {
			r.peers.Append(peerUpdate.NodeID)
		} else {
			r.logger.Error("could not use peer for dbsync (removing)", "peer", peerUpdate.NodeID)
			r.peers.Remove(peerUpdate.NodeID)
		}
	case p2p.PeerStatusDown:
		r.peers.Remove(peerUpdate.NodeID)
	}

	r.mtx.Lock()
	defer r.mtx.Unlock()

	switch peerUpdate.Status {
	case p2p.PeerStatusUp:
		newProvider := light.NewBlockProvider(peerUpdate.NodeID, r.chainID, r.dispatcher)

		r.providers[peerUpdate.NodeID] = newProvider
		if sp, ok := r.stateProvider.(*light.StateProviderP2P); ok {
			// we do this in a separate routine to not block whilst waiting for the light client to finish
			// whatever call it's currently executing
			go sp.AddProvider(newProvider)
		}

	case p2p.PeerStatusDown:
		delete(r.providers, peerUpdate.NodeID)
	}
	r.logger.Debug("processed peer update", "peer", peerUpdate.NodeID, "status", peerUpdate.Status)
}

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

func (r *Reactor) processMetadataCh(ctx context.Context, ch *p2p.Channel) {
	for {
		m, err := ch.Recv(ctx)
		if err != nil {
			return
		}
		if err := r.handleMetadataMessage(ctx, m); err != nil {
			r.router.Evict(m.From, fmt.Errorf("dbsync.metadata: %w", err))
		}
	}
}

func (r *Reactor) processFileCh(ctx context.Context, ch *p2p.Channel) {
	for {
		m, err := ch.Recv(ctx)
		if err != nil {
			return
		}
		if err := r.handleFileMessage(m); err != nil {
			r.router.Evict(m.From, fmt.Errorf("dbsync.file: %w", err))
		}
	}
}

func (r *Reactor) processLightBlockCh(ctx context.Context, ch *p2p.Channel) {
	for {
		m, err := ch.Recv(ctx)
		if err != nil {
			return
		}
		if err := r.handleLightBlockMessage(ctx, m); err != nil {
			r.router.Evict(m.From, fmt.Errorf("dbsync.lightBlock: %w", err))
		}
	}
}

func (r *Reactor) processParamsCh(ctx context.Context, ch *p2p.Channel) {
	for {
		m, err := ch.Recv(ctx)
		if err != nil {
			return
		}
		if err := r.handleParamsMessage(ctx, m); err != nil {
			r.router.Evict(m.From, fmt.Errorf("dbsync.params: %w", err))
		}
	}
}

func (r *Reactor) requestMetadata() { r.metadataChannel.Broadcast(&dstypes.MetadataRequest{}) }

func (r *Reactor) requestFile(peer types.NodeID, height uint64, filename string) {
	r.fileChannel.Send(&dstypes.FileRequest{
		Height:   height,
		Filename: filename,
	}, peer)
}

func (r *Reactor) fetchLightBlock(height uint64) (*types.LightBlock, error) {
	h := int64(height)

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

func (r *Reactor) commitState(ctx context.Context, height uint64) (sm.State, *types.Commit, error) {
	appHash, err := r.stateProvider.AppHash(ctx, height)
	if err != nil {
		r.logger.Error(fmt.Sprintf("error getting apphash for %d due to %s", height, err))
		return sm.State{}, nil, err
	}
	r.logger.Info(fmt.Sprintf("got apphash %X for %d", appHash, height))
	state, err := r.stateProvider.State(ctx, height)
	if err != nil {
		r.logger.Error(fmt.Sprintf("error getting state for %d due to %s", height, err))
		return sm.State{}, nil, err
	}
	commit, err := r.stateProvider.Commit(ctx, height)
	if err != nil {
		r.logger.Error(fmt.Sprintf("error committing for %d due to %s", height, err))
		return sm.State{}, nil, err
	}
	return state, commit, nil
}

func (r *Reactor) postSync(ctx context.Context, state sm.State, commit *types.Commit) error {
	if err := r.stateStore.Bootstrap(state); err != nil {
		return err
	}
	if err := r.blockStore.SaveSeenCommit(state.LastBlockHeight, commit); err != nil {
		return err
	}
	if err := r.eventBus.PublishEventStateSyncStatus(types.EventDataStateSyncStatus{
		Complete: true,
		Height:   state.LastBlockHeight,
	}); err != nil {
		return err
	}
	if err := r.postSyncHook(ctx, state); err != nil {
		r.logger.Error(fmt.Sprintf("encountered error in post sync hook: %s", err))
		return nil
	}

	return nil
}
