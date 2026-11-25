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
	// resyncs counts resync requests. Each client stream opened before a request reopens after it.
	resyncs utils.AtomicSend[uint64]
}

// ValidatorAddrGossip is what a Service needs of a ValidatorAddrs to run the address stream.
type ValidatorAddrGossip interface {
	// Receive stores ann if it is verified and newer than the one held for its validator.
	Receive(ann *atypes.Signed[atypes.ValidatorAddrAnnouncement]) error
	// AwaitMissing waits until some held announcement is one peerHolds rejects, and returns
	// every such announcement.
	AwaitMissing(ctx context.Context, peerHolds func(validator atypes.PublicKey, version uint64) bool) ([]*atypes.Signed[atypes.ValidatorAddrAnnouncement], error)
	// Resyncs changes on every resync request.
	Resyncs() utils.AtomicRecv[uint64]
}

// NewValidatorAddrs returns an empty ValidatorAddrs that keeps the announcements verify accepts,
// along with the address it returns for each.
func NewValidatorAddrs[A any](verify func(*atypes.Signed[atypes.ValidatorAddrAnnouncement]) (A, error)) *ValidatorAddrs[A] {
	return &ValidatorAddrs[A]{
		verify:  verify,
		held:    utils.NewAtomicSend(ValidatorAddrSnapshot[A]{}),
		resyncs: utils.NewAtomicSend(uint64(0)),
	}
}

// Watch is a watch on the held entries. Every change publishes a new snapshot.
func (a *ValidatorAddrs[A]) Watch() utils.AtomicRecv[ValidatorAddrSnapshot[A]] {
	return a.held.Subscribe()
}

// RequestResync makes every client stream open again, so each peer sends its full table.
func (a *ValidatorAddrs[A]) RequestResync() {
	// writes makes the load and store one step, so concurrent requests cannot lose an increment.
	a.writes.Lock()
	defer a.writes.Unlock()
	a.resyncs.Store(a.resyncs.Load() + 1)
}

// Resyncs is a watch on the resync count, which RequestResync increments.
func (a *ValidatorAddrs[A]) Resyncs() utils.AtomicRecv[uint64] {
	return a.resyncs.Subscribe()
}

// Get returns the address held for validator.
func (a *ValidatorAddrs[A]) Get(validator atypes.PublicKey) (A, bool) {
	entry, ok := a.held.Load()[validator]
	return entry.Addr, ok
}

// Receive stores ann if it passes verification and is newer than the held announcement of its
// validator. A stale announcement is not an error; a refused one is returned as the error.
func (a *ValidatorAddrs[A]) Receive(ann *atypes.Signed[atypes.ValidatorAddrAnnouncement]) error {
	a.writes.Lock()
	defer a.writes.Unlock()
	// Verification runs under the lock so that Retain cannot run between it and the store: an
	// announcement admitted against an Anchor is then kept or dropped by the next Retain, never
	// left behind after it.
	addr, err := a.verify(ann)
	if err != nil {
		return err
	}
	held := a.held.Load()
	if old, ok := held[ann.Key()]; ok && old.Ann.Msg().Version >= ann.Msg().Version {
		return nil
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

// AwaitMissing implements ValidatorAddrGossip.
func (a *ValidatorAddrs[A]) AwaitMissing(ctx context.Context, peerHolds func(atypes.PublicKey, uint64) bool) ([]*atypes.Signed[atypes.ValidatorAddrAnnouncement], error) {
	snapshot, err := a.held.Wait(ctx, func(snapshot ValidatorAddrSnapshot[A]) bool {
		for validator, entry := range snapshot {
			if !peerHolds(validator, entry.Ann.Msg().Version) {
				return true
			}
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	var missing []*atypes.Signed[atypes.ValidatorAddrAnnouncement]
	for validator, entry := range snapshot {
		if !peerHolds(validator, entry.Ann.Msg().Version) {
			missing = append(missing, entry.Ann)
		}
	}
	return missing, nil
}

// validatorAddrRate is the sustained rate of announcements accepted per stream after the initial burst.
var validatorAddrRate = rate.Every(100 * time.Millisecond)

// validatorAddrBurst admits a full table on connect with room for updates that arrive during that push:
// twice the table's capacity, one announcement per possible member of each Anchor epoch.
const validatorAddrBurst = 2 * len(data.Anchor{}.Epochs) * atypes.MaxValidators

var errValidatorAddrRateLimited = errors.New("validator address announcement rate exceeded")

// errValidatorAddrResync ends a client stream so that the next one asks the peer for its full table.
var errValidatorAddrResync = errors.New("validator address stream reopened for resync")

// validatorAddrStream is a validator address stream as seen from either end of a connection.
type validatorAddrStream = rpc.Stream[*apb.SignedValidatorAddrAnnouncement, *apb.SignedValidatorAddrAnnouncement]

// peerVersions records the newest announcement version of each validator that a peer is known to hold:
// the versions it sent to us and the versions we sent to it.
type peerVersions struct {
	versions utils.Mutex[map[atypes.PublicKey]uint64]
}

func newPeerVersions() *peerVersions {
	return &peerVersions{versions: utils.NewMutex(map[atypes.PublicKey]uint64{})}
}

func (p *peerVersions) holds(validator atypes.PublicKey, version uint64) bool {
	for versions := range p.versions.Lock() {
		held, ok := versions[validator]
		return ok && held >= version
	}
	panic("unreachable")
}

func (p *peerVersions) record(validator atypes.PublicKey, version uint64) {
	for versions := range p.versions.Lock() {
		versions[validator] = max(versions[validator], version)
	}
}

// forget undoes record for an announcement the table refused, so the record only grows
// with the validators the table holds.
func (p *peerVersions) forget(validator atypes.PublicKey, version uint64) {
	for versions := range p.versions.Lock() {
		if versions[validator] == version {
			delete(versions, validator)
		}
	}
}

// serverStreamValidatorAddrs exchanges announcements with a dialing peer. Its failure ends only this
// exchange; see endValidatorAddrExchange.
func (x *Service) serverStreamValidatorAddrs(ctx context.Context, server rpc.Server[API]) error {
	err := StreamValidatorAddrs.Serve(ctx, server, func(ctx context.Context, stream validatorAddrStream) error {
		// A stream that ends must not end the serve loop: the peer may open the next one, for a resync.
		return endValidatorAddrExchange(ctx, x.exchangeValidatorAddrs(ctx, stream, nil))
	})
	return endValidatorAddrExchange(ctx, err)
}

// clientStreamValidatorAddrs exchanges announcements with the peer that was dialed. Each resync request
// opens a new stream, so the peer sends its full table again. Its failure ends only this exchange;
// see endValidatorAddrExchange.
func (x *Service) clientStreamValidatorAddrs(ctx context.Context, client rpc.Client[API]) error {
	resyncs := x.validatorAddrs.Resyncs()
	for {
		err := x.callValidatorAddrs(ctx, client, resyncs)
		if !errors.Is(err, errValidatorAddrResync) || ctx.Err() != nil {
			return endValidatorAddrExchange(ctx, err)
		}
	}
}

// callValidatorAddrs runs one client stream until it fails, or until a resync request is made after
// it opened, which returns errValidatorAddrResync.
func (x *Service) callValidatorAddrs(ctx context.Context, client rpc.Client[API], resyncs utils.AtomicRecv[uint64]) error {
	opened := resyncs.Load()
	stream, err := StreamValidatorAddrs.Call(ctx, client)
	if err != nil {
		return fmt.Errorf("client.StreamValidatorAddrs(): %w", err)
	}
	defer stream.Close()
	return x.exchangeValidatorAddrs(ctx, stream, func(ctx context.Context) error {
		if _, err := resyncs.Wait(ctx, func(n uint64) bool { return n != opened }); err != nil {
			return err
		}
		return errValidatorAddrResync
	})
}

// endValidatorAddrExchange logs why the gossip exchange ended and returns nil, so the scope that runs
// the exchange keeps its consensus streams. Gossip is not needed for consensus, and a peer that
// violates its limit loses gossip on this connection until the next one.
func endValidatorAddrExchange(ctx context.Context, err error) error {
	if err != nil && ctx.Err() == nil {
		logger.Warn("validator address exchange ended", "err", err)
	}
	return nil
}

// exchangeValidatorAddrs sends the held announcements to the peer and hands the peer's announcements
// to the table. Announcements that fail verification are dropped; the exchange ends when the stream
// fails, the peer exceeds its rate limit, or until returns, which it does with an error.
func (x *Service) exchangeValidatorAddrs(ctx context.Context, stream validatorAddrStream, until func(context.Context) error) error {
	peer := newPeerVersions()
	recv := rate.NewLimiter(validatorAddrRate, validatorAddrBurst)
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return x.sendValidatorAddrs(ctx, stream, peer) })
		s.Spawn(func() error { return x.receiveValidatorAddrs(ctx, stream, peer, recv) })
		if until != nil {
			s.Spawn(func() error { return until(ctx) })
		}
		return nil
	})
}

// sendValidatorAddrs sends the held announcements the peer does not hold, and again whenever
// the table gains one.
func (x *Service) sendValidatorAddrs(ctx context.Context, stream validatorAddrStream, peer *peerVersions) error {
	// Push, not digest: the peer's table is unknown when the stream opens, so the whole table is
	// sent and nothing is skipped on connect. The table holds at most the members of two Anchor
	// epochs, so this stays small. A digest exchange (the peer sends the (validator, version)
	// pairs it holds, and only the missing ones are sent) is the next step if the table grows.
	for {
		anns, err := x.validatorAddrs.AwaitMissing(ctx, peer.holds)
		if err != nil {
			return err
		}
		for _, ann := range anns {
			if peer.holds(ann.Key(), ann.Msg().Version) {
				continue
			}
			if err := stream.Send(ctx, atypes.SignedValidatorAddrAnnouncementConv.Encode(ann)); err != nil {
				return fmt.Errorf("stream.Send(): %w", err)
			}
			peer.record(ann.Key(), ann.Msg().Version)
		}
	}
}

// receiveValidatorAddrs hands each announcement from the peer to the table.
func (x *Service) receiveValidatorAddrs(ctx context.Context, stream validatorAddrStream, peer *peerVersions, limiter *rate.Limiter) error {
	for {
		raw, err := stream.Recv(ctx)
		if err != nil {
			return fmt.Errorf("stream.Recv(): %w", err)
		}
		if !limiter.Allow() {
			return errValidatorAddrRateLimited
		}
		ann, err := atypes.SignedValidatorAddrAnnouncementConv.Decode(raw)
		if err != nil {
			return fmt.Errorf("SignedValidatorAddrAnnouncementConv.Decode(): %w", err)
		}
		// The peer holds what it sent, so it is not sent back. This is recorded before the table sees
		// the announcement: once stored, the table wakes the sender, which must already know.
		peer.record(ann.Key(), ann.Msg().Version)
		if err := x.validatorAddrs.Receive(ann); err != nil {
			peer.forget(ann.Key(), ann.Msg().Version)
			logger.Debug("dropped validator address announcement", "validator", ann.Key(), "version", ann.Msg().Version, "err", err)
		}
	}
}
