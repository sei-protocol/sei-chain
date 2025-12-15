package hashable

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/hashable/pb"
)

// Test checking that the canonical encoding is a valid proto encoding and that it is stable.
func TestMarshalCanonicalRoundTrip(t *testing.T) {
	// NOTE: math/rand.New uses the Go stdlib implementation, so hashes might change
	// if that implementation ever changes. If that happens, switch to a hardcoded PRNG
	// instead of updating hashes blindly.
	testCases := []struct {
		name     string
		seed     int64
		wantHash string
	}{
		{name: "Seed0", seed: 0x79, wantHash: "4787b6b8c6807694bd979b56d1a86c8cbe37f3764fb787f653cfbd82d91ab116"},
		{name: "Seed1", seed: 0xca, wantHash: "41f05a42ac8a1bd3fc5079202e516a93f0464e9d1bdd3a78c8e3d207ef9fa09d"},
		{name: "Seed2", seed: 0x12f, wantHash: "8b003e47c39776e8db30bb6153ada73ee60cffb15091c0facb68f31a20f099a3"},
		{name: "Seed3", seed: 0x194, wantHash: "b5ef94d6af6be1b2bc16fac8fefefad047f488798503bc4997bff63fbc1e6393"},
		{name: "Seed4", seed: 0x1f9, wantHash: "c54b74a5de4883d7dbd8b8bc2be7147c99b62384de8241a880802ce0cf23bf81"},
		{name: "Seed5", seed: 0x25e, wantHash: "ff465e5ecfc3446152f34fb3e48387b9316d49cc66876608c18d34d17bac072d"},
		{name: "Seed6", seed: 0x2c3, wantHash: "bb65b0f1869173abdd618c18e2b91eae2dc1d647cf2d01d6e9ed1c97b90a3b65"},
		{name: "Seed7", seed: 0x328, wantHash: "0ec51f6b630acdd89ffaa016850b88c2f9d278f5a464daa4a8ab95195b6c896d"},
		{name: "Seed8", seed: 0x38d, wantHash: "1c615c400ebddf4846fdfd4cb478f9158faf8aaa52f5871028327e27f8c1dd59"},
		{name: "Seed9", seed: 0x3f2, wantHash: "5a4bbc7725ed37b05c00d45a88a95e8918a97d17cfa86cd1f500fd8b2382a6f6"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := msgFromSeed(tc.seed)

			var decoded pb.AllKinds
			canonical := MarshalCanonical(msg)
			require.NoError(t, proto.Unmarshal(canonical, &decoded))
			require.NoError(t, utils.TestDiff(msg, &decoded))

			gotHash := sha256.Sum256(canonical)
			require.Equal(t, tc.wantHash, hex.EncodeToString(gotHash[:]))
		})
	}
}

func msgFromSeed(seed int64) *pb.AllKinds {
	r := rand.New(rand.NewSource(seed))
	msg := &pb.AllKinds{}

	if r.Intn(2) == 0 {
		msg.BoolValue = utils.Alloc(r.Intn(2) == 0)
	}
	if r.Intn(2) == 0 {
		msg.EnumValue = utils.Alloc(pb.SampleEnum(r.Intn(3)))
	}
	if r.Intn(2) == 0 {
		msg.Int32Value = utils.Alloc(int32(r.Int63n(1 << 31)))
	}
	if r.Intn(2) == 0 {
		msg.Int64Value = utils.Alloc(r.Int63())
	}
	if r.Intn(2) == 0 {
		msg.Sint32Value = utils.Alloc(int32(r.Intn(1<<15)) - 1<<14)
	}
	if r.Intn(2) == 0 {
		msg.Sint64Value = utils.Alloc(r.Int63n(1<<40) - 1<<39)
	}
	if r.Intn(2) == 0 {
		msg.Uint32Value = utils.Alloc(uint32(r.Uint32()))
	}
	if r.Intn(2) == 0 {
		msg.Uint64Value = utils.Alloc(r.Uint64())
	}
	if r.Intn(2) == 0 {
		msg.Fixed32Value = utils.Alloc(uint32(r.Uint32()))
	}
	if r.Intn(2) == 0 {
		msg.Fixed64Value = utils.Alloc(r.Uint64())
	}
	if r.Intn(2) == 0 {
		msg.Sfixed32Value = utils.Alloc(int32(r.Int63n(1 << 31)))
	}
	if r.Intn(2) == 0 {
		msg.Sfixed64Value = utils.Alloc(r.Int63())
	}
	if r.Intn(2) == 0 {
		msg.BytesValue = randomBytes(r)
	}
	if r.Intn(2) == 0 {
		msg.StringValue = utils.Alloc(randomString(r))
	}
	if r.Intn(2) == 0 {
		msg.MessageValue = randomNested(r)
	}
	if r.Intn(2) == 0 {
		msg.RepeatedPackable = randomSlice(r, func(r *rand.Rand) int64 { return r.Int63() })
	}
	if r.Intn(2) == 0 {
		msg.RepeatedString = randomSlice(r, randomString)
	}
	if r.Intn(2) == 0 {
		msg.RepeatedMessage = randomSlice(r, randomNested)
	}
	if r.Intn(2) == 0 {
		msg.OptionalMessage = randomNested(r)
	}
	if r.Intn(2) == 0 {
		msg.RepeatedPackableSingleton = []uint32{uint32(r.Uint32())}
	}
	if r.Intn(2) == 0 {
		msg.RepeatedBytes = randomSlice(r, randomBytes)
	}
	if r.Intn(2) == 0 {
		msg.RepeatedPackableEmpty = make([]uint64, 0)
	}

	return msg
}

func randomString(r *rand.Rand) string {
	return fmt.Sprintf("hello-%d", r.Int())
}

func randomBytes(r *rand.Rand) []byte {
	n := r.Intn(5) + 10
	b := make([]byte, n)
	_, _ = r.Read(b)
	return b
}

func randomSlice[T any](r *rand.Rand, gen func(*rand.Rand) T) []T {
	n := r.Intn(5) + 3
	out := make([]T, n)
	for i := range out {
		out[i] = gen(r)
	}
	return out
}

func randomNested(r *rand.Rand) *pb.Nested {
	nested := &pb.Nested{}
	switch r.Intn(3) {
	case 0:
		nested.T = &pb.Nested_Note{Note: randomString(r)}
	case 1:
		nested.T = &pb.Nested_Value{Value: uint32(r.Uint32())}
	default:
		// leave oneof unset to test empty case
	}
	return nested
}
