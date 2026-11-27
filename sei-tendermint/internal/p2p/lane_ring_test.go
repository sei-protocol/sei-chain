package p2p

import (
	"fmt"
	"math"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

// laneRingSizes are the committee sizes that the lane ring benchmarks compare.
var laneRingSizes = []int{40, 100}

func genLaneSeed(rng utils.Rng) laneSeed {
	return laneSeed(utils.GenBytes(rng, len(laneSeed{})))
}

func genSenders(rng utils.Rng, n int) []common.Address {
	return rng.GenSliceN(n, func(rng utils.Rng) common.Address {
		return common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
	})
}

// genEqualStakeCommittee returns a committee of n random validators that all have weight 1.
func genEqualStakeCommittee(rng utils.Rng, n int) *atypes.Committee {
	weights := make(map[atypes.PublicKey]uint64, n)
	for range n {
		weights[atypes.GenPublicKey(rng)] = 1
	}
	return utils.OrPanic1(atypes.NewCommittee(weights))
}

// committeeWithout returns committee c without validator v.
func committeeWithout(c *atypes.Committee, v atypes.PublicKey) *atypes.Committee {
	weights := map[atypes.PublicKey]uint64{}
	for lane := range c.Lanes().All() {
		if lane.Validator != v {
			weights[lane.Validator] = c.Weight(lane.Validator)
		}
	}
	return utils.OrPanic1(atypes.NewCommittee(weights))
}

// committeeWith returns committee c with validator v added at weight 1.
func committeeWith(c *atypes.Committee, v atypes.PublicKey) *atypes.Committee {
	weights := map[atypes.PublicKey]uint64{v: 1}
	for lane := range c.Lanes().All() {
		weights[lane.Validator] = c.Weight(lane.Validator)
	}
	return utils.OrPanic1(atypes.NewCommittee(weights))
}

// committeeWithWeightsPlusOne returns committee c with each weight raised by one.
func committeeWithWeightsPlusOne(c *atypes.Committee) *atypes.Committee {
	weights := map[atypes.PublicKey]uint64{}
	for lane := range c.Lanes().All() {
		weights[lane.Validator] = c.Weight(lane.Validator) + 1
	}
	return utils.OrPanic1(atypes.NewCommittee(weights))
}

func allReachable(atypes.PublicKey) bool { return true }

// assignLanes returns the lane that a assigns to each sender.
func assignLanes(a laneAssigner, senders []common.Address, reachable func(atypes.PublicKey) bool) []atypes.PublicKey {
	lanes := make([]atypes.PublicKey, len(senders))
	for i, s := range senders {
		lanes[i] = a.Lane(s, reachable).OrPanic("no reachable lane")
	}
	return lanes
}

func TestLaneRing_SameInputsGiveSameLane(t *testing.T) {
	rng := utils.TestRng()
	c, _ := atypes.GenCommittee(rng, 40)
	seed := genLaneSeed(rng)
	senders := genSenders(rng, 1000)

	got := assignLanes(newLaneRing(seed, c), senders, allReachable)

	require.Equal(t, got, assignLanes(newLaneRing(seed, c), senders, allReachable))
}

func TestLaneRing_SeedsMapSendersIndependently(t *testing.T) {
	rng := utils.TestRng()
	const n = 40
	c, _ := atypes.GenCommittee(rng, n)
	senders := genSenders(rng, 20000)

	a := assignLanes(newLaneRing(genLaneSeed(rng), c), senders, allReachable)
	b := assignLanes(newLaneRing(genLaneSeed(rng), c), senders, allReachable)

	// Independent mappings agree on about 1/n of the senders.
	same := 0
	for i := range senders {
		if a[i] == b[i] {
			same++
		}
	}
	require.Less(t, float64(same)/float64(len(senders)), 2.0/n)
}

func TestLaneRing_SpreadsSendersEvenly(t *testing.T) {
	for _, n := range laneRingSizes {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			rng := utils.TestRng()
			c, _ := atypes.GenCommittee(rng, n)
			senders := genSenders(rng, 200000)

			lanes := assignLanes(newLaneRing(genLaneSeed(rng), c), senders, allReachable)

			counts := map[atypes.PublicKey]int{}
			for _, l := range lanes {
				counts[l]++
			}
			avg := float64(len(senders)) / float64(n)
			variance := 0.0
			for lane := range c.Lanes().All() {
				d := float64(counts[lane.Validator])/avg - 1
				variance += d * d
			}
			require.Less(t, math.Sqrt(variance/float64(n)), 0.10)
		})
	}
}

func TestLaneRing_LeavingValidatorMovesOnlyItsSenders(t *testing.T) {
	rng := utils.TestRng()
	c, _ := atypes.GenCommittee(rng, 40)
	seed := genLaneSeed(rng)
	senders := genSenders(rng, 20000)
	gone := c.Lanes().At(7).Validator

	before := assignLanes(newLaneRing(seed, c), senders, allReachable)
	after := assignLanes(newLaneRing(seed, committeeWithout(c, gone)), senders, allReachable)

	for i := range senders {
		if before[i] != gone {
			require.Equal(t, before[i], after[i])
		}
	}
}

func TestLaneRing_JoiningValidatorTakesSendersOnlyForItself(t *testing.T) {
	rng := utils.TestRng()
	c, _ := atypes.GenCommittee(rng, 40)
	seed := genLaneSeed(rng)
	senders := genSenders(rng, 20000)
	joined := atypes.GenPublicKey(rng)

	before := assignLanes(newLaneRing(seed, c), senders, allReachable)
	after := assignLanes(newLaneRing(seed, committeeWith(c, joined)), senders, allReachable)

	for i := range senders {
		if after[i] != joined {
			require.Equal(t, before[i], after[i])
		}
	}
}

func TestLaneRing_UnreachableLaneMovesOnlyItsSenders(t *testing.T) {
	rng := utils.TestRng()
	c, _ := atypes.GenCommittee(rng, 40)
	seed := genLaneSeed(rng)
	r := newLaneRing(seed, c)
	senders := genSenders(rng, 20000)
	down := c.Lanes().At(3).Validator

	before := assignLanes(r, senders, allReachable)
	after := assignLanes(r, senders, func(v atypes.PublicKey) bool { return v != down })

	for i := range senders {
		require.NotEqual(t, down, after[i])
		if before[i] != down {
			require.Equal(t, before[i], after[i])
		}
	}
	// An unreachable lane sends each of its senders where the ring without that
	// lane's validator sends it.
	require.Equal(t, assignLanes(newLaneRing(seed, committeeWithout(c, down)), senders, allReachable), after)
}

func TestLaneRing_NoReachableLaneGivesNone(t *testing.T) {
	rng := utils.TestRng()
	c, _ := atypes.GenCommittee(rng, 10)
	r := newLaneRing(genLaneSeed(rng), c)

	got := r.Lane(genSenders(rng, 1)[0], func(atypes.PublicKey) bool { return false })

	require.False(t, got.IsPresent())
}

func TestLaneRing_ServesOnlyCommitteesWithItsValidators(t *testing.T) {
	rng := utils.TestRng()
	c, _ := atypes.GenCommittee(rng, 40)
	r := newLaneRing(genLaneSeed(rng), c)
	v := c.Lanes().At(5).Validator

	require.True(t, r.Serves(committeeWithWeightsPlusOne(c)))
	require.False(t, r.Serves(committeeWithout(c, v)))
	require.False(t, r.Serves(committeeWith(c, atypes.GenPublicKey(rng))))
	require.False(t, r.Serves(committeeWith(committeeWithout(c, v), atypes.GenPublicKey(rng))))
}

func TestLaneAssignments_RebuildsOnlyWhenValidatorsChange(t *testing.T) {
	rng := utils.TestRng()
	c, _ := atypes.GenCommittee(rng, 10)
	reweighted := committeeWithWeightsPlusOne(c)
	changed := committeeWithout(c, c.Lanes().At(0).Validator)
	seed := genLaneSeed(rng)
	var built []*atypes.Committee
	a := newLaneAssignments(func(c *atypes.Committee) laneAssigner {
		built = append(built, c)
		return newLaneRing(seed, c)
	})

	// The same committee builds once, and a committee with the same validators
	// and other weights reuses that build.
	first := a.For(c)
	require.Equal(t, first, a.For(c))
	require.Equal(t, first, a.For(reweighted))
	require.Equal(t, []*atypes.Committee{c}, built)
	// A committee with other validators builds again.
	a.For(changed)
	require.Equal(t, []*atypes.Committee{c, changed}, built)
}

func TestLaneAssignments_ConcurrentLookups(t *testing.T) {
	rng := utils.TestRng()
	c1, _ := atypes.GenCommittee(rng, 10)
	c2 := committeeWithout(c1, c1.Lanes().At(0).Validator)
	seed := genLaneSeed(rng)
	a := newLaneAssignments(func(c *atypes.Committee) laneAssigner { return newLaneRing(seed, c) })
	senders := genSenders(rng, 100)
	want1 := assignLanes(newLaneRing(seed, c1), senders, allReachable)
	want2 := assignLanes(newLaneRing(seed, c2), senders, allReachable)

	// Lookups alternate between two committees, so the cache keeps being replaced
	// while other goroutines read it. Each lookup must still use its own committee.
	err := scope.Parallel(func(s scope.ParallelScope) error {
		for w := range 8 {
			s.Spawn(func() error {
				for i, sender := range senders {
					c, want := c1, want1
					if (i+w)%2 == 1 {
						c, want = c2, want2
					}
					if got := a.For(c).Lane(sender, allReachable).OrPanic("no lane"); got != want[i] {
						return fmt.Errorf("sender %d: lane %v, want %v", i, got, want[i])
					}
				}
				return nil
			})
		}
		return nil
	})
	require.NoError(t, err)
}

func BenchmarkLaneRingBuild(b *testing.B) {
	for _, n := range laneRingSizes {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			rng := utils.TestRng()
			c := genEqualStakeCommittee(rng, n)
			seed := genLaneSeed(rng)
			b.ReportAllocs()
			var r *laneRing
			for b.Loop() {
				r = newLaneRing(seed, c)
			}
			b.ReportMetric(float64(len(r.positions)*8+len(r.owners)), "ring-bytes")
		})
	}
}

// BenchmarkLaneLookup measures a lane ring lookup with 0, 1, a third, and all
// but one of the lanes unreachable.
func BenchmarkLaneLookup(b *testing.B) {
	for _, n := range laneRingSizes {
		rng := utils.TestRng()
		c := genEqualStakeCommittee(rng, n)
		r := newLaneRing(genLaneSeed(rng), c)
		senders := genSenders(rng, 1024)

		for _, dc := range []struct {
			name  string
			nDown int
		}{
			{"all-up", 0},
			{"one-down", 1},
			{"third-down", n / 3},
			{"all-but-one-down", n - 1},
		} {
			down := map[atypes.PublicKey]bool{}
			for i := range dc.nDown {
				down[c.Lanes().At(i).Validator] = true
			}
			reachable := func(v atypes.PublicKey) bool { return !down[v] }
			b.Run(fmt.Sprintf("ring-%s/N=%d", dc.name, n), func(b *testing.B) {
				b.ReportAllocs()
				i := 0
				for b.Loop() {
					r.Lane(senders[i%len(senders)], reachable)
					i++
				}
			})
		}
	}
}
