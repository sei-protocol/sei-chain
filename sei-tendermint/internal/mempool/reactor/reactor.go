package reactor

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/clist"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/service"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/seilog"
)

var (
	logger = seilog.NewLogger("tendermint", "internal", "mempool")

	_ service.Service = (*Reactor)(nil)
)

const MempoolChannel p2p.ChannelID = 0x30

// Reactor implements a service that contains mempool of txs that are broadcasted
// amongst peers. It maintains a map from peer ID to counter, to prevent gossiping
// txs to the peers you received it from.
type Reactor struct {
	service.BaseService

	cfg     *config.MempoolConfig
	mempool *mempool.TxMempool
	ids     *IDs

	router *p2p.Router

	failedCheckTxCounts utils.Mutex[map[types.NodeID]int]

	channel      *p2p.Channel[*pb.Message]
	readyToStart chan struct{}
}

// NewReactor returns a reference to a new reactor.
func NewReactor(txmp *mempool.TxMempool, router *p2p.Router) (*Reactor, error) {
	channel, err := p2p.OpenChannel(router, GetChannelDescriptor(txmp.Config()))
	if err != nil {
		return nil, fmt.Errorf("router.OpenChannel(): %w", err)
	}
	r := &Reactor{
		cfg:                 txmp.Config(),
		mempool:             txmp,
		ids:                 NewMempoolIDs(),
		router:              router,
		channel:             channel,
		failedCheckTxCounts: utils.NewMutex(map[types.NodeID]int{}),
		readyToStart:        make(chan struct{}, 1),
	}
	r.BaseService = *service.NewBaseService("Mempool", r)
	return r, nil
}

func (r *Reactor) MarkReadyToStart() { r.readyToStart <- struct{}{} }

// GetChannelDescriptor produces an instance of a descriptor for this package's
// required channels.
func GetChannelDescriptor(cfg *config.MempoolConfig) p2p.ChannelDescriptor[*pb.Message] {
	largestTx := make([]byte, cfg.MaxTxBytes)
	batchMsg := &pb.Message{
		Sum: &pb.Message_Txs{
			Txs: &pb.Txs{Txs: [][]byte{largestTx}},
		},
	}

	return p2p.ChannelDescriptor[*pb.Message]{
		ID:                  MempoolChannel,
		MessageType:         new(pb.Message),
		Priority:            5,
		RecvMessageCapacity: batchMsg.Size(),
		RecvBufferCapacity:  128,
		Name:                "mempool",
	}
}

// OnStart starts separate goroutines for each p2p channel and listens for
// envelopes on each. In addition, it also listens for peer updates and handles
// messages on that p2p channel accordingly. The caller must be sure to execute
// OnStop to ensure the outbound p2p channels are closed.
func (r *Reactor) OnStart(ctx context.Context) error {
	if !r.cfg.Broadcast {
		logger.Info("tx broadcasting is disabled")
	}
	r.SpawnCritical("processMempoolCh", r.processMempoolCh)
	r.SpawnCritical("processPeerUpdates", r.processPeerUpdates)
	r.SpawnCritical("mempool", r.mempool.Run)
	return nil
}

// OnStop stops the reactor by signaling to all spawned goroutines to exit and
// blocking until they all exit.
func (r *Reactor) OnStop() {}

// handleMempoolMessage handles envelopes sent from peers on the MempoolChannel.
// For every tx in the message, we execute CheckTx. It returns an error if an
// empty set of txs are sent in an envelope or if we receive an unexpected
// message type.
func (r *Reactor) handleMempoolMessage(ctx context.Context, m p2p.RecvMsg[*pb.Message]) error {
	switch msg := m.Message.Sum.(type) {
	case *pb.Message_Txs:
		if err := msg.Txs.Validate(); err != nil {
			return err
		}
		protoTxs := msg.Txs.GetTxs()

		txInfo := mempool.TxInfo{SenderID: r.ids.GetForPeer(m.From)}
		if len(m.From) != 0 {
			txInfo.SenderNodeID = m.From
		}

		for _, tx := range protoTxs {
			if err := r.mempool.CheckTx(ctx, tx, nil, txInfo); err != nil {
				r.accountFailedCheckTx(m.From, err)
				if errors.Is(err, mempool.ErrTxInCache) {
					// If the tx is in the cache, then we've been gossiped a tx
					// that we've already got. Gossip should be smarter, but it's
					// not a problem.
					continue
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					// Do not propagate context cancellation errors, but do not
					// continue to check transactions from this message if we are
					// shutting down.
					return nil
				}

				logger.Debug("checktx failed for tx",
					"tx", types.Tx(tx).Key(),
					"peer", m.From,
					"err", err)
			}
		}

	default:
		return fmt.Errorf("received unknown message: %T", msg)
	}

	return nil
}

func (r *Reactor) accountFailedCheckTx(nodeID types.NodeID, err error) {
	if !r.cfg.CheckTxErrorBlacklistEnabled || !errors.Is(err, mempool.ErrTxTooLarge) {
		return
	}
	for counts := range r.failedCheckTxCounts.Lock() {
		if _, ok := counts[nodeID]; !ok {
			return
		}
		counts[nodeID]++
		if counts[nodeID] > r.cfg.CheckTxErrorThreshold {
			r.router.Evict(nodeID, errors.New("mempool: checkTx error exceeded threshold"))
		}
	}
}

// handleMessage handles an envelope sent from a peer on a specific p2p channel.
// It will handle errors and any possible panics gracefully. A caller can handle
// any error returned by sending a PeerError on the respective channel.
func (r *Reactor) handleMessage(ctx context.Context, m p2p.RecvMsg[*pb.Message]) (err error) {
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

	logger.Debug("received message", "peer", m.From)
	return r.handleMempoolMessage(ctx, m)
}

// processMempoolCh implements a blocking event loop where we listen for p2p
// envelope messages from the mempool channel.
func (r *Reactor) processMempoolCh(ctx context.Context) error {
	<-r.readyToStart
	for {
		m, err := r.channel.Recv(ctx)
		if err != nil {
			return err
		}
		if err := r.handleMessage(ctx, m); err != nil {
			r.router.Evict(m.From, fmt.Errorf("mempool: %w", err))
		}
	}
}

// processPeerUpdates initiates a blocking process where we listen for and
// handle PeerUpdate messages. When the reactor is stopped, we will catch the
// signal and close the p2p PeerUpdatesCh gracefully.
func (r *Reactor) processPeerUpdates(ctx context.Context) error {
	if !r.cfg.Broadcast {
		return nil
	}
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		recv := r.router.Subscribe()
		peerRoutines := map[types.NodeID]context.CancelFunc{}
		for {
			update, err := recv.Recv(ctx)
			if err != nil {
				return err
			}
			logger.Debug("received peer update", "peer", update.NodeID, "status", update.Status)

			switch update.Status {
			case p2p.PeerStatusUp:
				for counts := range r.failedCheckTxCounts.Lock() {
					counts[update.NodeID] = 0
				}
				pctx, pcancel := context.WithCancel(ctx)
				peerRoutines[update.NodeID] = pcancel
				r.ids.ReserveForPeer(update.NodeID)
				s.Spawn(func() error {
					r.broadcastTxRoutine(pctx, update.NodeID)
					return nil
				})

			case p2p.PeerStatusDown:
				r.ids.Reclaim(update.NodeID)
				for counts := range r.failedCheckTxCounts.Lock() {
					delete(counts, update.NodeID)
				}
				peerRoutines[update.NodeID]()
				delete(peerRoutines, update.NodeID)
			}
		}
	})
}

func (r *Reactor) broadcastTxRoutine(ctx context.Context, peerID types.NodeID) {
	peerMempoolID := r.ids.GetForPeer(peerID)
	var nextGossipTx *clist.CElement

	defer func() {
		if e := recover(); e != nil {
			logger.Error(
				"recovering from broadcasting mempool loop",
				"err", e,
				"stack", string(debug.Stack()),
			)
		}
	}()

	var err error
	nextGossipTx,err = r.mempool.WaitForNextTx(ctx)
	if err!=nil {
		return
	}
	for ctx.Err() == nil {
		memTx := nextGossipTx.Value.(*mempool.WrappedTx)

		if ok := r.mempool.TxStore().TxHasPeer(memTx.Key(), peerMempoolID); !ok {
			r.channel.Send(&pb.Message{
				Sum: &pb.Message_Txs{
					Txs: &pb.Txs{Txs: [][]byte{memTx.Tx()}},
				},
			}, peerID)
			logger.Debug(
				"gossiped tx to peer",
				"tx", memTx.Tx().Hash(),
				"peer", peerID,
			)
		}

		if _,_,err := utils.RecvOrClosed(ctx,nextGossipTx.NextWaitChan()); err!=nil {
			return
		}
		nextGossipTx = nextGossipTx.Next()
	}
}
