package proto

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"

	stdproto "google.golang.org/protobuf/proto"

	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/libs/utils"
	testpb "github.com/tendermint/tendermint/proto_v2/test"
)

func TestMarshalCanonicalRoundTrip(t *testing.T) {
	testCases := []struct {
		name     string
		seed     int64
		wantHash string
	}{
		{name: "Seed0", seed: 101, wantHash: "ec16b6f1660dafb9afc9d49bc60f3798729495541dbbec8d6e508873945e8b9e"},
		{name: "Seed1", seed: 202, wantHash: "445bfc4d0e8234bda1bb9235c5953adc9eb5854b9f33ac793eddfb5e19690c4d"},
		{name: "Seed2", seed: 303, wantHash: "ba93fb2883451328cd88f1f695b5d6d3c29e3d11281c534f7a162c96eac80437"},
		{name: "Seed3", seed: 404, wantHash: "41a1af29fdbc352dc3e74c34a92f6052db5a3d95395eb95bdb4a2fc584d56ea4"},
		{name: "Seed4", seed: 505, wantHash: "006a588ad882c03507f9c9a50911b3ddd49f091a772bd280fc372f9679fba1cd"},
		{name: "Seed5", seed: 606, wantHash: "dbaf9ad50c0a227af6067a86ef6dcb704cff08f7e96dd4f15554c1ea2b769f89"},
		{name: "Seed6", seed: 707, wantHash: "003689d064ca46f46cb1ad709321c124e2b6f0bd8f311e16d3ca175ae8fa1e70"},
		{name: "Seed7", seed: 808, wantHash: "2ee310817f095f168cdccb1ebd3c8cea3b119c59db73159e6293c8b5ec8e39ac"},
		{name: "Seed8", seed: 909, wantHash: "0f09c55353603a270f128036efadd6748a5e267025f8501be2b9b374b4a8f02a"},
		{name: "Seed9", seed: 1010, wantHash: "87e328d813903d83d4838578f9653072aee3aa17542fb949804e272b9a466f49"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := newAllKindsFromSeed(tc.seed)
			canonical := MarshalCanonical(msg)
			var decoded testpb.AllKinds
			require.NoError(t, stdproto.Unmarshal(canonical, &decoded))
			require.NoError(t, utils.TestDiff(msg, &decoded))

			gotHash := sha256.Sum256(canonical)
			require.Equal(t, tc.wantHash, hex.EncodeToString(gotHash[:]))
		})
	}
}

func newAllKindsFromSeed(seed int64) *testpb.AllKinds {
	rng := rand.New(rand.NewSource(seed))
	msg := &testpb.AllKinds{}

	if rng.Intn(2) == 0 {
		msg.BoolValue = utils.Alloc(rng.Intn(2) == 0)
	}
	if rng.Intn(2) == 0 {
		msg.EnumValue = utils.Alloc(testpb.SampleEnum(rng.Intn(3)))
	}
	if rng.Intn(2) == 0 {
		msg.Int32Value = utils.Alloc(int32(rng.Int63n(1 << 31)))
	}
	if rng.Intn(2) == 0 {
		msg.Int64Value = utils.Alloc(rng.Int63())
	}
	if rng.Intn(2) == 0 {
		msg.Sint32Value = utils.Alloc(int32(rng.Intn(1<<15)) - 1<<14)
	}
	if rng.Intn(2) == 0 {
		msg.Sint64Value = utils.Alloc(rng.Int63n(1<<40) - 1<<39)
	}
	if rng.Intn(2) == 0 {
		msg.Uint32Value = utils.Alloc(uint32(rng.Uint32()))
	}
	if rng.Intn(2) == 0 {
		msg.Uint64Value = utils.Alloc(rng.Uint64())
	}
	if rng.Intn(2) == 0 {
		msg.Fixed32Value = utils.Alloc(uint32(rng.Uint32()))
	}
	if rng.Intn(2) == 0 {
		msg.Fixed64Value = utils.Alloc(rng.Uint64())
	}
	if rng.Intn(2) == 0 {
		msg.Sfixed32Value = utils.Alloc(int32(rng.Int63n(1 << 31)))
	}
	if rng.Intn(2) == 0 {
		msg.Sfixed64Value = utils.Alloc(int64(rng.Int63()))
	}
	if rng.Intn(2) == 0 {
		msg.BytesValue = randomBytes(rng, 8+int(rng.Int31n(8)))
	}
	if rng.Intn(2) == 0 {
		msg.StringValue = utils.Alloc(randomString(rng, "string"))
	}
	if rng.Intn(2) == 0 {
		msg.MessageValue = randomNested(rng)
	}
	if rng.Intn(2) == 0 {
		msg.RepeatedPackable = randomInt64Slice(rng, 2+rng.Intn(3))
	}
	if rng.Intn(2) == 0 {
		msg.RepeatedString = randomStringSlice(rng, 1+rng.Intn(3))
	}
	if rng.Intn(2) == 0 {
		msg.RepeatedMessage = randomNestedSlice(rng, 1+rng.Intn(3))
	}
	if rng.Intn(2) == 0 {
		msg.OptionalMessage = randomNested(rng)
	}
	if rng.Intn(2) == 0 {
		msg.RepeatedPackableSingleton = []uint32{uint32(rng.Uint32())}
	}
	if rng.Intn(2) == 0 {
		msg.RepeatedBytes = randomBytesSlice(rng, 1+rng.Intn(3))
	}
	// repeated_packable_empty intentionally left nil or empty to ensure we also exercise omitted fields.
	if rng.Intn(2) == 0 {
		msg.RepeatedPackableEmpty = make([]uint64, 0)
	}

	return msg
}

func randomString(rng *rand.Rand, label string) string {
	return fmt.Sprintf("%s-%d", label, rng.Int())
}

func randomBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	_, _ = rng.Read(b)
	return b
}

func randomInt64Slice(rng *rand.Rand, n int) []int64 {
	if n <= 1 {
		n = 2
	}
	out := make([]int64, n)
	for i := range out {
		out[i] = rng.Int63n(1<<20) - 1<<19
	}
	return out
}

func randomStringSlice(rng *rand.Rand, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = randomString(rng, fmt.Sprintf("rep-str-%d", i))
	}
	return out
}

func randomBytesSlice(rng *rand.Rand, n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = randomBytes(rng, 4+int(rng.Int31n(4)))
	}
	return out
}

func randomNestedSlice(rng *rand.Rand, n int) []*testpb.Nested {
	out := make([]*testpb.Nested, n)
	for i := range out {
		out[i] = randomNested(rng)
	}
	return out
}

func randomNested(rng *rand.Rand) *testpb.Nested {
	nested := &testpb.Nested{}
	if rng.Intn(2) == 0 {
		nested.Note = utils.Alloc(randomString(rng, "nested"))
	}
	if rng.Intn(2) == 0 {
		nested.Value = utils.Alloc(uint32(rng.Uint32()))
	}
	if nested.Note == nil && nested.Value == nil {
		nested.Note = utils.Alloc(randomString(rng, "fallback"))
	}
	return nested
}
