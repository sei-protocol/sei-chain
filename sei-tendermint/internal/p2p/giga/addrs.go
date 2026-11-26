package giga

import (
	"context"
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

// ValidatorAddrEntry is a verified announcement together with the value verification derived from it.
type ValidatorAddrEntry[A any] struct {
	Ann  *atypes.Signed[atypes.ValidatorAddrAnnouncement]
	Addr A
}

// ValidatorAddrSnapshot is the entries a ValidatorAddrs held at one moment, by validator.
// It is never modified after it is published.
type ValidatorAddrSnapshot[A any] map[atypes.PublicKey]ValidatorAddrEntry[A]

// Same reports whether s and other hold the same announcements.
func (s ValidatorAddrSnapshot[A]) Same(other ValidatorAddrSnapshot[A]) bool {
	return maps.EqualFunc(s, other, func(a, b ValidatorAddrEntry[A]) bool { return a.Ann == b.Ann })
}

// ValidatorAddrs holds the newest verified address announcement of each validator, with the
// address of type A that verification derived from it.
type ValidatorAddrs[A any] struct {
	verify func(*atypes.Signed[atypes.ValidatorAddrAnnouncement]) (A, error)
	// writes serializes Receive and Retain, which publish modified copies of held. verify runs under it.
	writes sync.Mutex
	held   utils.AtomicSend[ValidatorAddrSnapshot[A]]
}

// ValidatorAddrGossip is what a Service needs of a ValidatorAddrs to run the address stream.
type ValidatorAddrGossip interface {
	// Receive stores ann if it is verified and newer than the one held for its validator.
	Receive(ann *atypes.Signed[atypes.ValidatorAddrAnnouncement]) error
	// Announcements returns every held announcement.
	Announcements() []*atypes.Signed[atypes.ValidatorAddrAnnouncement]
	// HeldVersion returns the version of the announcement held for validator, if one is held.
	HeldVersion(validator atypes.PublicKey) (uint64, bool)
}

// NewValidatorAddrs returns an empty ValidatorAddrs that keeps the announcements verify accepts,
// along with the address it returns for each.
func NewValidatorAddrs[A any](verify func(*atypes.Signed[atypes.ValidatorAddrAnnouncement]) (A, error)) *ValidatorAddrs[A] {
	return &ValidatorAddrs[A]{
		verify: verify,
		held:   utils.NewAtomicSend(ValidatorAddrSnapshot[A]{}),
	}
}

// Watch is a watch on the held entries. Every change publishes a new snapshot.
func (a *ValidatorAddrs[A]) Watch() utils.AtomicRecv[ValidatorAddrSnapshot[A]] {
	return a.held.Subscribe()
}

// HeldVersion returns the version of the announcement held for validator, if one is held.
func (a *ValidatorAddrs[A]) HeldVersion(validator atypes.PublicKey) (uint64, bool) {
	entry, ok := a.held.Load()[validator]
	if !ok {
		return 0, false
	}
	return entry.Ann.Msg().Version, true
}

// Get returns the address held for validator.
func (a *ValidatorAddrs[A]) Get(validator atypes.PublicKey) (A, bool) {
	entry, ok := a.held.Load()[validator]
	return entry.Addr, ok
}

// Announcements returns the held announcements, one per validator, in no particular order.
func (a *ValidatorAddrs[A]) Announcements() []*atypes.Signed[atypes.ValidatorAddrAnnouncement] {
	held := a.held.Load()
	anns := make([]*atypes.Signed[atypes.ValidatorAddrAnnouncement], 0, len(held))
	for _, entry := range held {
		anns = append(anns, entry.Ann)
	}
	return anns
}

// Receive stores ann if it passes verification and is newer than the held announcement of its
// validator. A stale announcement is not an error; a refused one is returned as the error.
func (a *ValidatorAddrs[A]) Receive(ann *atypes.Signed[atypes.ValidatorAddrAnnouncement]) error {
	a.writes.Lock()
	defer a.writes.Unlock()
	held := a.held.Load()
	// A version already held is not verified again: the held entry passed verification when it was
	// stored, and peers repeat their whole table every interval.
	if old, ok := held[ann.Key()]; ok && old.Ann.Msg().Version >= ann.Msg().Version {
		return nil
	}
	// Verification runs under the lock so that Retain cannot run between it and the store: an
	// announcement admitted against an Anchor is then kept or dropped by the next Retain, never
	// left behind after it.
	addr, err := a.verify(ann)
	if err != nil {
		return err
	}
	next := maps.Clone(held)
	next[ann.Key()] = ValidatorAddrEntry[A]{Ann: ann, Addr: addr}
	a.held.Store(next)
	return nil
}

// Retain drops the entries of the validators keep rejects.
func (a *ValidatorAddrs[A]) Retain(keep func(atypes.PublicKey) bool) {
	a.writes.Lock()
	defer a.writes.Unlock()
	held := a.held.Load()
	next := make(ValidatorAddrSnapshot[A], len(held))
	for validator, entry := range held {
		if keep(validator) {
			next[validator] = entry
		}
	}
	if len(next) < len(held) {
		a.held.Store(next)
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
// Messages beyond the budget are dropped.
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

// serverStreamValidatorAddrs serves each validator address stream a dialing peer opens. A stream that
// fails ends only itself, so the serve loop keeps accepting the peer's next one.
func (x *Service) serverStreamValidatorAddrs(ctx context.Context, server rpc.Server[API]) error {
	err := StreamValidatorAddrs.Serve(ctx, server, func(ctx context.Context, stream validatorAddrStream) error {
		return endValidatorAddrExchange(ctx, x.exchangeValidatorAddrs(ctx, stream))
	})
	return endValidatorAddrExchange(ctx, err)
}

// clientStreamValidatorAddrs runs the stream to the peer that was dialed. Once it ends it is not
// reopened until the next connection.
func (x *Service) clientStreamValidatorAddrs(ctx context.Context, client rpc.Client[API]) error {
	stream, err := StreamValidatorAddrs.Call(ctx, client)
	if err != nil {
		return endValidatorAddrExchange(ctx, fmt.Errorf("client.StreamValidatorAddrs(): %w", err))
	}
	defer stream.Close()
	return endValidatorAddrExchange(ctx, x.exchangeValidatorAddrs(ctx, stream))
}

// endValidatorAddrExchange logs why the gossip exchange ended and returns nil.
func endValidatorAddrExchange(ctx context.Context, err error) error {
	if err != nil && ctx.Err() == nil {
		logger.Warn("validator address exchange ended", "err", err)
	}
	// Gossip is not needed for consensus, so the scope running the exchange keeps its consensus streams.
	return nil
}

// peerHolds is one stream's record of what the peer holds: the newest version of each validator the
// peer sent us or acked, and the acks owed to it.
type peerHolds struct {
	state utils.Mutex[peerState]
	// ready holds a signal while acks are queued, so the sender flushes them without waiting for a push.
	ready chan struct{}
}

// peerState is guarded by peerHolds.state.
type peerState struct {
	// versions are the newest version of each validator the peer sent or acked. Entries stay after
	// Retain drops their validator, until the stream closes.
	versions map[atypes.PublicKey]uint64
	// acks are the versions the peer sent that are owed an ack, until the next flush.
	acks map[atypes.PublicKey]uint64
}

func newPeerHolds() *peerHolds {
	return &peerHolds{
		state: utils.NewMutex(peerState{
			versions: map[atypes.PublicKey]uint64{},
			acks:     map[atypes.PublicKey]uint64{},
		}),
		ready: make(chan struct{}, 1),
	}
}

// covers reports whether the peer holds a version of validator at least version.
func (p *peerHolds) covers(validator atypes.PublicKey, version uint64) bool {
	for s := range p.state.Lock() {
		held, ok := s.versions[validator]
		return ok && held >= version
	}
	panic("unreachable")
}

// received notes that the peer sent ann and owes it an ack, including when the peer resends a version
// it already sent.
func (p *peerHolds) received(ann *atypes.Signed[atypes.ValidatorAddrAnnouncement]) {
	for s := range p.state.Lock() {
		key, version := ann.Key(), ann.Msg().Version
		s.versions[key] = max(s.versions[key], version)
		s.acks[key] = max(s.acks[key], version)
		select {
		case p.ready <- struct{}{}:
		default:
		}
	}
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
func encodeAnnouncement(ann *atypes.Signed[atypes.ValidatorAddrAnnouncement]) *apb.ValidatorAddrMsg {
	return &apb.ValidatorAddrMsg{T: &apb.ValidatorAddrMsg_Announcement{Announcement: atypes.SignedValidatorAddrAnnouncementConv.Encode(ann)}}
}

// encodeAck wraps ack as a message on the validator address stream.
func encodeAck(ack atypes.ValidatorAddrAck) *apb.ValidatorAddrMsg {
	return &apb.ValidatorAddrMsg{T: &apb.ValidatorAddrMsg_Ack{Ack: atypes.ValidatorAddrAckConv.Encode(ack)}}
}

// exchangeValidatorAddrs runs both directions of the stream until one of them fails.
func (x *Service) exchangeValidatorAddrs(ctx context.Context, stream validatorAddrStream) error {
	peer := newPeerHolds()
	recv := rate.NewLimiter(validatorAddrRate, validatorAddrBurst)
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.sendValidatorAddrs(ctx, stream, peer) })
		s.Spawn(func() error { return x.receiveValidatorAddrs(ctx, stream, peer, recv) })
		return nil
	})
}

// sendValidatorAddrs pushes the held announcements the peer has not sent us, on connect and again
// every validatorAddrInterval, sending acks as they are queued in between.
func (x *Service) sendValidatorAddrs(ctx context.Context, stream validatorAddrStream, peer *peerHolds) error {
	for {
		if err := sendAcks(ctx, stream, peer); err != nil {
			return err
		}
		for _, ann := range x.validatorAddrs.Announcements() {
			if peer.covers(ann.Key(), ann.Msg().Version) {
				continue
			}
			if err := stream.Send(ctx, encodeAnnouncement(ann)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
		}
		if err := waitForPush(ctx, stream, peer); err != nil {
			return err
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

// waitForPush returns once validatorAddrInterval has elapsed, sending each ack as soon as it is queued.
func waitForPush(ctx context.Context, stream validatorAddrStream, peer *peerHolds) error {
	timer := time.NewTimer(validatorAddrInterval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-peer.ready:
			if err := sendAcks(ctx, stream, peer); err != nil {
				return err
			}
		}
	}
}

// receiveValidatorAddrs hands each announcement from the peer to the table and records each ack. A
// message beyond validatorAddrRate is dropped without ending the stream, since the peer's next push
// repeats it.
func (x *Service) receiveValidatorAddrs(ctx context.Context, stream validatorAddrStream, peer *peerHolds, limiter *rate.Limiter) error {
	for {
		raw, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		if !limiter.Allow() {
			continue
		}
		switch t := raw.T.(type) {
		case *apb.ValidatorAddrMsg_Announcement:
			ann, err := atypes.SignedValidatorAddrAnnouncementConv.Decode(t.Announcement)
			if err != nil {
				return fmt.Errorf("SignedValidatorAddrAnnouncementConv.Decode(): %w", err)
			}
			if err := x.validatorAddrs.Receive(ann); err != nil {
				logger.Debug("dropped validator address announcement", "validator", ann.Key(), "version", ann.Msg().Version, "err", err)
				continue
			}
			// Recorded only after the table accepts the announcement, so the record holds verified entries alone.
			peer.received(ann)
		case *apb.ValidatorAddrMsg_Ack:
			ack, err := atypes.ValidatorAddrAckConv.Decode(t.Ack)
			if err != nil {
				return fmt.Errorf("ValidatorAddrAckConv.Decode(): %w", err)
			}
			// An ack is honored only for a version this table holds. A peer that acks more than
			// this node holds would otherwise suppress versions this node could send it.
			if held, ok := x.validatorAddrs.HeldVersion(ack.Validator); ok && ack.Version <= held {
				peer.acked(ack.Validator, ack.Version)
			}
		default:
			return fmt.Errorf("validator address message: unknown type %T", t)
		}
	}
}
