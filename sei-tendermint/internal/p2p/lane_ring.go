package p2p

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// laneRingPointsPerLane is the number of points that each lane owns on a laneRing.
const laneRingPointsPerLane = 200

const (
	laneRingPointTag  = "lane-ring-point"
	laneRingSenderTag = "lane-ring-sender"
)

// A laneRing stores each point's owner as a uint8 index into its validators, so
// a committee must fit in 256 lanes. This constant fails to compile otherwise.
const _ = uint8(atypes.MaxValidators - 1)

// laneSeed is the per-process secret that keys a full node's lane ring.
type laneSeed [32]byte

// newLaneSeed returns a random laneSeed.
func newLaneSeed() laneSeed {
	var s laneSeed
	_, _ = rand.Read(s[:]) // crypto/rand.Read never returns an error.
	return s
}

// laneRing is a laneAssigner that uses consistent hashing. Each lane owns
// laneRingPointsPerLane points on a ring keyed by a laneSeed, and a sender
// belongs to the lane of the first point at or after the sender's own ring
// position.
type laneRing struct {
	seed       laneSeed
	validators []atypes.PublicKey
	positions  []uint64 // sorted ascending
	owners     []uint8  // owners[i] indexes validators for the point at positions[i]
}

// newLaneRing builds the laneRing of committee c for seed.
func newLaneRing(seed laneSeed, c *atypes.Committee) *laneRing {
	type point struct {
		pos   uint64
		owner uint8
	}
	lanes := c.Lanes()
	validators := make([]atypes.PublicKey, 0, lanes.Len())
	points := make([]point, 0, lanes.Len()*laneRingPointsPerLane)
	buf := make([]byte, 0, len(laneRingPointTag)+len(seed)+32+2)
	for owner := range lanes.Len() {
		validator := lanes.At(owner).Validator
		validators = append(validators, validator)
		for i := range laneRingPointsPerLane {
			buf = append(buf[:0], laneRingPointTag...)
			buf = append(buf, seed[:]...)
			buf = append(buf, validator.Bytes()...)
			buf = binary.BigEndian.AppendUint16(buf, uint16(i)) // nolint: gosec // i < laneRingPointsPerLane
			h := sha256.Sum256(buf)
			points = append(points, point{pos: binary.BigEndian.Uint64(h[:]), owner: uint8(owner)}) // nolint: gosec // owner < MaxValidators, checked above
		}
	}
	slices.SortFunc(points, func(a, b point) int { return cmp.Compare(a.pos, b.pos) })
	r := &laneRing{
		seed:       seed,
		validators: validators,
		positions:  make([]uint64, len(points)),
		owners:     make([]uint8, len(points)),
	}
	for i, p := range points {
		r.positions[i] = p.pos
		r.owners[i] = p.owner
	}
	return r
}

// Serves reports whether committee c has the same validators as the committee r
// was built for, whatever their weights and join epochs.
func (r *laneRing) Serves(c *atypes.Committee) bool {
	lanes := c.Lanes()
	if lanes.Len() != len(r.validators) {
		return false
	}
	for i, v := range r.validators {
		// Committee.Lanes is sorted by validator key, and r.validators follows that order.
		if lanes.At(i).Validator != v {
			return false
		}
	}
	return true
}

// senderPosition returns the ring position of sender.
func (r *laneRing) senderPosition(sender common.Address) uint64 {
	var buf [len(laneRingSenderTag) + len(laneSeed{}) + common.AddressLength]byte
	n := copy(buf[:], laneRingSenderTag)
	n += copy(buf[n:], r.seed[:])
	copy(buf[n:], sender[:])
	h := sha256.Sum256(buf[:])
	return binary.BigEndian.Uint64(h[:])
}

// Lane returns the validator of the first point at or after the sender's ring
// position, wrapping at the end, whose validator reachable accepts.
func (r *laneRing) Lane(sender common.Address, reachable func(atypes.PublicKey) bool) utils.Option[atypes.PublicKey] {
	start, _ := slices.BinarySearch(r.positions, r.senderPosition(sender))
	// A lane owns many points, so the walk asks reachable once per lane. The array
	// covers every uint8 owner index and stays on the stack.
	var tried [1 << 8]bool
	for n := range len(r.positions) {
		owner := r.owners[(start+n)%len(r.positions)]
		if tried[owner] {
			continue
		}
		tried[owner] = true
		if v := r.validators[owner]; reachable(v) {
			return utils.Some(v)
		}
	}
	return utils.None[atypes.PublicKey]()
}
