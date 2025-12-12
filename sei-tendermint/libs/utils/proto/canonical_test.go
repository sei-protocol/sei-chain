package proto

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/libs/utils"
	testpb "github.com/tendermint/tendermint/proto_v2/test"
)

func TestMarshalCanonicalRoundTrip(t *testing.T) {
	// NOTE: math/rand.New uses the Go stdlib implementation, so hashes might change
	// if that implementation ever changes. If that happens, switch to a hardcoded PRNG
	// instead of updating hashes blindly.
	testCases := []struct {
		name     string
		seed     int64
		wantHash string
	}{
		{name: "Seed0", seed: 0x79, wantHash: "18e941dacee4ed1f374c11b7572c08d717ab10f1d87a2996b3028b246aba5577"},
		{name: "Seed1", seed: 0xca, wantHash: "445bfc4d0e8234bda1bb9235c5953adc9eb5854b9f33ac793eddfb5e19690c4d"},
		{name: "Seed2", seed: 0x12f, wantHash: "ba93fb2883451328cd88f1f695b5d6d3c29e3d11281c534f7a162c96eac80437"},
		{name: "Seed3", seed: 0x194, wantHash: "41a1af29fdbc352dc3e74c34a92f6052db5a3d95395eb95bdb4a2fc584d56ea4"},
		{name: "Seed4", seed: 0x1f9, wantHash: "006a588ad882c03507f9c9a50911b3ddd49f091a772bd280fc372f9679fba1cd"},
		{name: "Seed5", seed: 0x25e, wantHash: "dbaf9ad50c0a227af6067a86ef6dcb704cff08f7e96dd4f15554c1ea2b769f89"},
		{name: "Seed6", seed: 0x2c3, wantHash: "003689d064ca46f46cb1ad709321c124e2b6f0bd8f311e16d3ca175ae8fa1e70"},
		{name: "Seed7", seed: 0x328, wantHash: "2ee310817f095f168cdccb1ebd3c8cea3b119c59db73159e6293c8b5ec8e39ac"},
		{name: "Seed8", seed: 0x38d, wantHash: "0f09c55353603a270f128036efadd6748a5e267025f8501be2b9b374b4a8f02a"},
		{name: "Seed9", seed: 0x3f2, wantHash: "87e328d813903d83d4838578f9653072aee3aa17542fb949804e272b9a466f49"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := msgFromSeed(tc.seed)

			var decoded testpb.AllKinds
			canonical := MarshalCanonical(msg)
			require.NoError(t, proto.Unmarshal(canonical, &decoded))
			require.NoError(t, utils.TestDiff(msg, &decoded))

			gotHash := sha256.Sum256(canonical)
			require.Equal(t, tc.wantHash, hex.EncodeToString(gotHash[:]))
		})
	}
}

func msgFromSeed(seed int64) *testpb.AllKinds {
	r := rand.New(rand.NewSource(seed))
	msg := &testpb.AllKinds{}

	if r.Intn(2) == 0 {
		msg.BoolValue = utils.Alloc(r.Intn(2) == 0)
	}
	if r.Intn(2) == 0 {
		msg.EnumValue = utils.Alloc(testpb.SampleEnum(r.Intn(3)))
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
		msg.RepeatedMessage = randomSlice(r,randomNested)
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
	n := r.Intn(5)+10
	b := make([]byte, n)
	_, _ = r.Read(b)
	return b
}

func randomSlice[T any](r *rand.Rand, gen func(*rand.Rand) T) []T {
	n := r.Intn(5)+3
	out := make([]T, n)
	for i := range out {
		out[i] = gen(r)
	}
	return out
}

func randomNested(r *rand.Rand) *testpb.Nested {
	nested := &testpb.Nested{}
	switch r.Intn(3) {
	case 0: nested.Note = utils.Alloc(randomString(r))
	case 1: nested.Value = utils.Alloc(uint32(r.Uint32()))
	case 2: // empty oneof 
	}
	return nested
}
