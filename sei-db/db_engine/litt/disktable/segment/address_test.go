//go:build littdb_wip

package segment

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

// randomAddress builds an Address with each field independently randomized over its full domain.
func randomAddress(rand *util.TestRandom) types.Address {
	return types.NewAddress(
		rand.Uint32(),
		rand.Uint32(),
		uint8(rand.Uint32Range(0, 256)),
		rand.Uint32(),
	)
}

// assertRoundTrip serializes the input, deserializes the result, and asserts equality in both directions.
func assertRoundTrip(t *testing.T, address types.Address) {
	t.Helper()

	serialized := address.Serialize()
	require.Len(t, serialized, types.AddressSerializedSize)

	deserialized, err := types.DeserializeAddress(serialized)
	require.NoError(t, err)
	require.Equal(t, address, deserialized)

	// Going the other direction (bytes -> Address -> bytes) should also be stable.
	reserialized := deserialized.Serialize()
	require.Equal(t, serialized, reserialized)
}

func TestAddressGetters(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()

	index := rand.Uint32()
	offset := rand.Uint32()
	shardID := uint8(rand.Uint32Range(0, 256))
	valueSize := rand.Uint32()
	address := types.NewAddress(index, offset, shardID, valueSize)

	require.Equal(t, index, address.Index())
	require.Equal(t, offset, address.Offset())
	require.Equal(t, shardID, address.ShardID())
	require.Equal(t, valueSize, address.ValueSize())
}

// TestAddressZeroValueRoundTrip verifies that the zero Address (which is what callers see for keymap misses)
// roundtrips cleanly through serialize/deserialize.
func TestAddressZeroValueRoundTrip(t *testing.T) {
	t.Parallel()

	zero := types.Address{}
	require.Equal(t, uint32(0), zero.Index())
	require.Equal(t, uint32(0), zero.Offset())
	require.Equal(t, uint8(0), zero.ShardID())
	require.Equal(t, uint32(0), zero.ValueSize())

	serialized := zero.Serialize()
	require.Len(t, serialized, types.AddressSerializedSize)
	for i, b := range serialized {
		require.Equal(t, byte(0), b, "byte %d should be zero", i)
	}

	deserialized, err := types.DeserializeAddress(serialized)
	require.NoError(t, err)
	require.Equal(t, zero, deserialized)
}

// TestAddressBoundaryRoundTrips covers the corners of the value domain to make sure
// no field-truncation or sign-extension bugs sneak in.
func TestAddressBoundaryRoundTrips(t *testing.T) {
	t.Parallel()

	cases := []types.Address{
		types.NewAddress(0, 0, 0, 0),
		types.NewAddress(math.MaxUint32, math.MaxUint32, math.MaxUint8, math.MaxUint32),
		types.NewAddress(math.MaxUint32, 0, 0, 0),
		types.NewAddress(0, math.MaxUint32, 0, 0),
		types.NewAddress(0, 0, math.MaxUint8, 0),
		types.NewAddress(0, 0, 0, math.MaxUint32),
		types.NewAddress(1, 2, 3, 4),
		types.NewAddress(math.MaxUint32, 0, math.MaxUint8, 0),
		types.NewAddress(0, math.MaxUint32, 0, math.MaxUint32),
	}

	for i, addr := range cases {
		addr := addr
		t.Run("", func(t *testing.T) {
			t.Parallel()
			assertRoundTrip(t, addr)
			require.NotPanicsf(t, func() { _ = addr.String() }, "case %d", i)
		})
	}
}

// TestAddressAllShardIDsRoundTrip exhaustively covers every legal shard ID (0..255) so that we know
// the single byte slot is wired up for every value it can take.
func TestAddressAllShardIDsRoundTrip(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()

	for shard := 0; shard < 256; shard++ {
		address := types.NewAddress(rand.Uint32(), rand.Uint32(), uint8(shard), rand.Uint32())
		assertRoundTrip(t, address)
		require.Equal(t, uint8(shard), address.ShardID())
	}
}

// TestAddressRandomRoundTrips fuzzes the round trip with a large batch of independently random addresses.
func TestAddressRandomRoundTrips(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()

	for i := 0; i < 1000; i++ {
		assertRoundTrip(t, randomAddress(rand))
	}
}

// TestAddressSerializeWireFormat pins down the on-disk byte layout so an accidental change to the wire format
// is caught by tests rather than by silently corrupting persisted data.
func TestAddressSerializeWireFormat(t *testing.T) {
	t.Parallel()

	const (
		index     uint32 = 0x01020304
		offset    uint32 = 0x05060708
		shardID   uint8  = 0x09
		valueSize uint32 = 0x0A0B0C0D
	)

	expected := []byte{
		0x01, 0x02, 0x03, 0x04, // index
		0x05, 0x06, 0x07, 0x08, // offset
		0x09,                   // shardID
		0x0A, 0x0B, 0x0C, 0x0D, // valueSize
	}
	require.Len(t, expected, types.AddressSerializedSize)

	address := types.NewAddress(index, offset, shardID, valueSize)
	require.Equal(t, expected, address.Serialize())

	deserialized, err := types.DeserializeAddress(expected)
	require.NoError(t, err)
	require.Equal(t, address, deserialized)
}

// TestAddressDeserializeSerializeRoundTrip confirms that arbitrary 13-byte buffers are stable when
// fed through deserialize → serialize.
func TestAddressDeserializeSerializeRoundTrip(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()

	for i := 0; i < 1000; i++ {
		raw := rand.Bytes(types.AddressSerializedSize)

		address, err := types.DeserializeAddress(raw)
		require.NoError(t, err)

		require.Equal(t, raw, address.Serialize())

		// Sanity: every byte is reachable through one of the getters and big-endian decoding.
		require.Equal(t, binary.BigEndian.Uint32(raw[0:4]), address.Index())
		require.Equal(t, binary.BigEndian.Uint32(raw[4:8]), address.Offset())
		require.Equal(t, raw[8], address.ShardID())
		require.Equal(t, binary.BigEndian.Uint32(raw[9:13]), address.ValueSize())
	}
}

// TestAddressSerializeReturnsFreshBuffer guards against a future "optimization" that returns a shared
// underlying array, which would silently cause callers (e.g. the key file writer) to see corrupted data
// if they retain the slice across calls.
func TestAddressSerializeReturnsFreshBuffer(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()

	address := randomAddress(rand)

	first := address.Serialize()
	second := address.Serialize()
	require.Equal(t, first, second)

	// Mutating the first slice must not affect a subsequent serialization, nor the Address itself.
	original := append([]byte{}, first...)
	for i := range first {
		first[i] ^= 0xFF
	}

	require.Equal(t, original, second, "second serialization should not share memory with the first")
	require.Equal(t, original, address.Serialize(), "third serialization should match the original bytes")
}

// TestAddressDeserializeIsIndependentOfInput guards against deserialize aliasing the caller-owned input
// slice. Mutating the source bytes after Deserialize returns must not perturb the resulting Address.
func TestAddressDeserializeIsIndependentOfInput(t *testing.T) {
	t.Parallel()
	rand := util.NewTestRandom()

	source := rand.Bytes(types.AddressSerializedSize)
	address, err := types.DeserializeAddress(source)
	require.NoError(t, err)

	expected := address
	for i := range source {
		source[i] ^= 0xFF
	}

	require.Equal(t, expected, address)
}

// TestDeserializeAddressLengthError checks that DeserializeAddress rejects every length that is not exactly
// AddressSerializedSize, including the legacy 8-byte address length.
func TestDeserializeAddressLengthError(t *testing.T) {
	t.Parallel()

	badLengths := []int{
		0,
		1,
		8, // the pre-refactor uint64 length, included as a regression guard
		types.AddressSerializedSize - 1,
		types.AddressSerializedSize + 1,
		32,
		1024,
	}

	for _, badLength := range badLengths {
		_, err := types.DeserializeAddress(make([]byte, badLength))
		require.Errorf(t, err, "expected error for length %d", badLength)
	}
}

// TestAddressString provides smoke coverage for the String formatter so that the human-readable form
// is at least guaranteed to mention every field.
func TestAddressString(t *testing.T) {
	t.Parallel()

	address := types.NewAddress(11, 22, 33, 44)
	s := address.String()

	for _, want := range []string{"11", "22", "33", "44"} {
		require.Contains(t, s, want, "String() = %q should contain %q", s, want)
	}
}
