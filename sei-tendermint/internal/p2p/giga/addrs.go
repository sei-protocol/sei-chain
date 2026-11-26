package giga

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"golang.org/x/time/rate"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	apb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// ValidatorAddrSnapshot is the announcements a ValidatorAddrs held at one moment, by validator.
// It is never modified after it is published.
type ValidatorAddrSnapshot map[atypes.PublicKey]*atypes.SignedValidatorAddrAnnouncement

// Same reports whether s and other hold the same announcements.
func (s ValidatorAddrSnapshot) Same(other ValidatorAddrSnapshot) bool {
	return maps.Equal(s, other)
}

// ValidatorAddrs holds the newest verified address announcement of each validator.
type ValidatorAddrs struct {
	verify func(*atypes.SignedValidatorAddrAnnouncement) bool
	// writes serializes Receive and Retain, which publish modified copies of held. verify runs under it.
	writes sync.Mutex
	held   utils.AtomicSend[ValidatorAddrSnapshot]
	// announced is the announcements in held, published with it.
	announced utils.AtomicSend[[]*atypes.SignedValidatorAddrAnnouncement]
}

// NewValidatorAddrs returns an empty ValidatorAddrs that keeps the announcements verify accepts.
func NewValidatorAddrs(verify func(*atypes.SignedValidatorAddrAnnouncement) bool) *ValidatorAddrs {
	return &ValidatorAddrs{
		verify:    verify,
		held:      utils.NewAtomicSend(ValidatorAddrSnapshot{}),
		announced: utils.NewAtomicSend([]*atypes.SignedValidatorAddrAnnouncement{}),
	}
}

// Watch is a watch on the held entries. Every change publishes a new snapshot.
func (a *ValidatorAddrs) Watch() utils.AtomicRecv[ValidatorAddrSnapshot] {
	return a.held.Subscribe()
}

// HeldVersion returns the version of the announcement held for validator, if one is held.
func (a *ValidatorAddrs) HeldVersion(validator atypes.PublicKey) (uint64, bool) {
	ann, ok := a.held.Load()[validator]
	if !ok {
		return 0, false
	}
	return ann.Msg().Version, true
}

// Get returns the announcement held for validator.
func (a *ValidatorAddrs) Get(validator atypes.PublicKey) (*atypes.SignedValidatorAddrAnnouncement, bool) {
	ann, ok := a.held.Load()[validator]
	return ann, ok
}

// WatchAnnouncements is a watch on the held announcements, one per validator, in no particular order.
func (a *ValidatorAddrs) WatchAnnouncements() utils.AtomicRecv[[]*atypes.SignedValidatorAddrAnnouncement] {
	return a.announced.Subscribe()
}

// publish stores next as the held entries and the announcements they carry.
func (a *ValidatorAddrs) publish(next ValidatorAddrSnapshot) {
	anns := make([]*atypes.SignedValidatorAddrAnnouncement, 0, len(next))
	for _, ann := range next {
		anns = append(anns, ann)
	}
	a.held.Store(next)
	a.announced.Store(anns)
}

// Receive stores ann if verify accepts it and it is newer than the held announcement of its
// validator. It reports whether the table holds ann or a newer announcement of its validator
// afterwards, so false means verify refused ann.
func (a *ValidatorAddrs) Receive(ann *atypes.SignedValidatorAddrAnnouncement) bool {
	a.writes.Lock()
	defer a.writes.Unlock()
	held := a.held.Load()
	// A version already held is not verified again: the held entry passed verification when it was
	// stored, and peers repeat their whole table every interval.
	if old, ok := held[ann.Key()]; ok && old.Msg().Version >= ann.Msg().Version {
		return true
	}
	// Verification runs under the lock so that Retain cannot run between it and the store: an
	// announcement admitted against an Anchor is then kept or dropped by the next Retain, never
	// left behind after it.
	if !a.verify(ann) {
		return false
	}
	next := maps.Clone(held)
	next[ann.Key()] = ann
	a.publish(next)
	return true
}

// Retain drops the entries of the validators keep rejects.
func (a *ValidatorAddrs) Retain(keep func(atypes.PublicKey) bool) {
	a.writes.Lock()
	defer a.writes.Unlock()
	held := a.held.Load()
	next := make(ValidatorAddrSnapshot, len(held))
	for validator, entry := range held {
		if keep(validator) {
			next[validator] = entry
		}
	}
	if len(next) < len(held) {
		a.publish(next)
	}
}

// validatorAddrInterval is how often a stream pushes the full table. An announcement a peer refused
// reaches it within one interval.
const validatorAddrInterval = 10 * time.Second

// validatorAddrTableBound is the most announcements a table holds: one for each possible member
// of each Anchor epoch.
const validatorAddrTableBound = len(data.Anchor{}.Epochs) * atypes.MaxValidators

// validatorAddrRotationBound is the most announcements a table holds during an epoch rotation, when
// the departing epoch's members remain until Retain runs.
const validatorAddrRotationBound = validatorAddrTableBound + atypes.MaxValidators

// validatorAddrRate is the refill rate of a stream's receive budget: two rotation-sized tables per
// validatorAddrInterval, one for announcements and one for the acks a peer sends back for them.
// Messages beyond the budget are held back, not read: the receiver waits for a token before it reads.
//
// A connection serves one stream at a time and accepts at most one per interval, and each stream
// starts with a full validatorAddrBurst, so a peer that reopens its stream every interval is admitted
// about twice the per-interval budget.
var validatorAddrRate = rate.Every(validatorAddrInterval / time.Duration(2*validatorAddrRotationBound))

// validatorAddrBurst is the most messages a stream admits at once: the peer's rotation-sized table plus
// the acks for ours.
const validatorAddrBurst = 2 * validatorAddrRotationBound

// validatorAddrStream is a validator address stream as seen from either end of a connection.
type validatorAddrStream = rpc.Stream[*apb.ValidatorAddrMsg, *apb.ValidatorAddrMsg]

// errValidatorAddrViolation marks a message from the peer that the protocol does not allow. The peer's
// connection is closed for it.
var errValidatorAddrViolation = errors.New("validator address protocol violation")

// serverStreamValidatorAddrs serves each validator address stream a dialing peer opens. A stream that
// fails ends only itself, so the serve loop keeps accepting the peer's next one, unless the peer
// violated the protocol.
func (x *Service) serverStreamValidatorAddrs(ctx context.Context, server rpc.Server[API]) (err error) {
	defer func() { err = endValidatorAddrExchange(ctx, err) }()
	return StreamValidatorAddrs.Serve(ctx, server, func(ctx context.Context, stream validatorAddrStream) (err error) {
		defer func() { err = endValidatorAddrExchange(ctx, err) }()
		return x.exchangeValidatorAddrs(ctx, stream)
	})
}

// clientStreamValidatorAddrs runs the stream to the peer that was dialed. Once it ends it is not
// reopened until the next connection.
func (x *Service) clientStreamValidatorAddrs(ctx context.Context, client rpc.Client[API]) (err error) {
	defer func() { err = endValidatorAddrExchange(ctx, err) }()
	stream, err := StreamValidatorAddrs.Call(ctx, client)
	if err != nil {
		return fmt.Errorf("client.StreamValidatorAddrs(): %w", err)
	}
	defer stream.Close()
	return x.exchangeValidatorAddrs(ctx, stream)
}

// endValidatorAddrExchange returns a protocol violation, which closes the connection. Any other error
// is logged and returns nil, so only the gossip exchange ends.
func endValidatorAddrExchange(ctx context.Context, err error) error {
	if errors.Is(err, errValidatorAddrViolation) {
		return err
	}
	if err != nil && ctx.Err() == nil {
		logger.Warn("validator address exchange ended", "err", err)
	}
	// Gossip is not needed for consensus, so the scope running the exchange keeps its consensus streams.
	return nil
}

// peerHolds is one stream's record of what the peer holds: the newest version of each validator the
// peer sent us or acked, the acks owed to it, and the announcements queued to send it.
type peerHolds struct {
	state utils.Watch[*peerState]
}

// peerState is guarded by peerHolds.state.
type peerState struct {
	// versions are the newest version of each validator the peer sent or acked. Entries stay after
	// Retain drops their validator, until the stream closes.
	versions map[atypes.PublicKey]uint64
	// acks are the versions the peer sent that are owed an ack, until the next flush.
	acks map[atypes.PublicKey]uint64
	// queued are the announcements to send the peer, one per validator, until the next flush.
	queued map[atypes.PublicKey]*atypes.SignedValidatorAddrAnnouncement
}

func newPeerHolds() *peerHolds {
	return &peerHolds{
		state: utils.NewWatch(&peerState{
			versions: map[atypes.PublicKey]uint64{},
			acks:     map[atypes.PublicKey]uint64{},
			queued:   map[atypes.PublicKey]*atypes.SignedValidatorAddrAnnouncement{},
		}),
	}
}

// received notes that the peer sent ann and owes it an ack, including when the peer resends a version
// it already sent.
func (p *peerHolds) received(ann *atypes.SignedValidatorAddrAnnouncement) {
	for s, ctrl := range p.state.Lock() {
		key, version := ann.Key(), ann.Msg().Version
		s.versions[key] = max(s.versions[key], version)
		s.acks[key] = max(s.acks[key], version)
		ctrl.Updated()
	}
}

// queue queues each announcement in anns that the peer does not hold, replacing any queued announcement
// of the same validator.
func (p *peerHolds) queue(anns []*atypes.SignedValidatorAddrAnnouncement) {
	for s, ctrl := range p.state.Lock() {
		queued := false
		for _, ann := range anns {
			key, version := ann.Key(), ann.Msg().Version
			if held, ok := s.versions[key]; ok && held >= version {
				continue
			}
			s.queued[key] = ann
			queued = true
		}
		if queued {
			ctrl.Updated()
		}
	}
}

// takeQueued returns the queued announcements the peer still does not hold, and clears the queue.
func (p *peerHolds) takeQueued() []*atypes.SignedValidatorAddrAnnouncement {
	for s := range p.state.Lock() {
		anns := make([]*atypes.SignedValidatorAddrAnnouncement, 0, len(s.queued))
		for key, ann := range s.queued {
			if held, ok := s.versions[key]; ok && held >= ann.Msg().Version {
				continue
			}
			anns = append(anns, ann)
		}
		clear(s.queued)
		return anns
	}
	panic("unreachable")
}

// awaitWork returns once an ack or an announcement is queued, or once ctx ends.
func (p *peerHolds) awaitWork(ctx context.Context) error {
	for s, ctrl := range p.state.Lock() {
		return ctrl.WaitUntil(ctx, func() bool { return len(s.acks) > 0 || len(s.queued) > 0 })
	}
	panic("unreachable")
}

// acked notes that the peer holds version of validator. An ack is not itself acked.
func (p *peerHolds) acked(validator atypes.PublicKey, version uint64) {
	for s := range p.state.Lock() {
		s.versions[validator] = max(s.versions[validator], version)
	}
}

// takeAcks returns the acks owed since the last call.
func (p *peerHolds) takeAcks() []atypes.ValidatorAddrAck {
	for s := range p.state.Lock() {
		acks := make([]atypes.ValidatorAddrAck, 0, len(s.acks))
		for validator, version := range s.acks {
			acks = append(acks, atypes.ValidatorAddrAck{Validator: validator, Version: version})
		}
		clear(s.acks)
		return acks
	}
	panic("unreachable")
}

// encodeAnnouncement wraps ann as a message on the validator address stream.
func encodeAnnouncement(ann *atypes.SignedValidatorAddrAnnouncement) *apb.ValidatorAddrMsg {
	return atypes.ValidatorAddrMsgConv.Encode(atypes.ValidatorAddrAnnouncementMsg{Ann: ann})
}

// encodeAck wraps ack as a message on the validator address stream.
func encodeAck(ack atypes.ValidatorAddrAck) *apb.ValidatorAddrMsg {
	return atypes.ValidatorAddrMsgConv.Encode(ack)
}

// exchangeValidatorAddrs runs both directions of the stream until one of them fails.
func (x *Service) exchangeValidatorAddrs(ctx context.Context, stream validatorAddrStream) error {
	peer := newPeerHolds()
	recv := rate.NewLimiter(validatorAddrRate, validatorAddrBurst)
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.queueValidatorAddrs(ctx, peer) })
		s.Spawn(func() error { return x.sendValidatorAddrs(ctx, stream, peer) })
		s.Spawn(func() error { return x.receiveValidatorAddrs(ctx, stream, peer, recv) })
		return nil
	})
}

// queueValidatorAddrs queues the announcements the peer does not hold. Each validatorAddrInterval starts
// with the whole table, which resends announcements the peer has not acked, such as ones it refused.
// Within an interval, a table change queues only the announcements whose version it has not queued.
func (x *Service) queueValidatorAddrs(ctx context.Context, peer *peerHolds) error {
	watch := x.validatorAddrs.WatchAnnouncements()
	for {
		intervalCtx, cancel := context.WithTimeout(ctx, validatorAddrInterval)
		queued := map[atypes.PublicKey]uint64{}
		// Iter calls the function with the current set, then with each newer set, and returns when intervalCtx ends.
		_ = watch.Iter(intervalCtx, func(_ context.Context, anns []*atypes.SignedValidatorAddrAnnouncement) error {
			changed := make([]*atypes.SignedValidatorAddrAnnouncement, 0, len(anns))
			for _, ann := range anns {
				key, version := ann.Key(), ann.Msg().Version
				if v, ok := queued[key]; ok && v >= version {
					continue
				}
				queued[key] = version
				changed = append(changed, ann)
			}
			peer.queue(changed)
			return nil
		})
		cancel()
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// sendValidatorAddrs sends the acks and queued announcements as they arrive.
func (x *Service) sendValidatorAddrs(ctx context.Context, stream validatorAddrStream, peer *peerHolds) error {
	for {
		if err := peer.awaitWork(ctx); err != nil {
			return err
		}
		if err := sendAcks(ctx, stream, peer); err != nil {
			return err
		}
		for _, ann := range peer.takeQueued() {
			if err := stream.Send(ctx, encodeAnnouncement(ann)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
	}
}

// sendAcks sends every ack owed to the peer.
func sendAcks(ctx context.Context, stream validatorAddrStream, peer *peerHolds) error {
	for _, ack := range peer.takeAcks() {
		if err := stream.Send(ctx, encodeAck(ack)); err != nil {
			return fmt.Errorf("stream.Send(): %w", err)
		}
	}
	return nil
}

// receiveValidatorAddrs hands each announcement from the peer to the table and records each ack. It
// waits for a token before each read, so a peer beyond validatorAddrRate is held back by the stream's
// flow control rather than having messages discarded.
func (x *Service) receiveValidatorAddrs(ctx context.Context, stream validatorAddrStream, peer *peerHolds, limiter *rate.Limiter) error {
	for {
		if err := limiter.Wait(ctx); err != nil {
			return err
		}
		raw, err := stream.Recv(ctx)
		if errors.Is(err, rpc.ErrMalformedMsg) {
			return fmt.Errorf("%w: %w", errValidatorAddrViolation, err)
		}
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		msg, err := atypes.ValidatorAddrMsgConv.Decode(raw)
		if err != nil {
			return fmt.Errorf("%w: %w", errValidatorAddrViolation, err)
		}
		switch msg := msg.(type) {
		case atypes.ValidatorAddrAnnouncementMsg:
			if !x.validatorAddrs.Receive(msg.Ann) {
				continue
			}
			// Recorded only after the table accepts the announcement, so the record holds verified entries alone.
			peer.received(msg.Ann)
		case atypes.ValidatorAddrAck:
			// An ack is honored only for a version this table holds. A peer that acks more than
			// this node holds would otherwise suppress versions this node could send it.
			if held, ok := x.validatorAddrs.HeldVersion(msg.Validator); ok && msg.Version <= held {
				peer.acked(msg.Validator, msg.Version)
			}
		default:
			return fmt.Errorf("%w: unknown message type %T", errValidatorAddrViolation, msg)
		}
	}
}
