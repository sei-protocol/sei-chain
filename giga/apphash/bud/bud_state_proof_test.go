package bud

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

const (
	// testStateKey is the key whose writes the BUD state proof tests prove.
	testStateKey = "evm/state-key"

	// testChainID is the chain ID of the test blocks.
	testChainID = 0x5e1

	// testFirstHeight is the height of the block that first writes testStateKey.
	testFirstHeight = 100

	// testSecondHeight is the height of the block that next writes testStateKey.
	testSecondHeight = 107
)

// testConsecutiveWrites is a key written at one block and next written at a later one, with each write's app hash
// data and BUD proof.
type testConsecutiveWrites struct {
	// The app hash data of the two blocks, in height order.
	appHashData []*apphash.AppHashData

	// The BUD proof of each write, against its block's BUD.
	budProofs []*BUDProof
}

// newTestConsecutiveWrites returns testStateKey written at testFirstHeight and next written at testSecondHeight,
// each in a block of random other budlets.
func newTestConsecutiveWrites(
	t *testing.T,
	// The source of the other budlets in each block.
	rng *rand.Rand,
	// The value of the second write, or nil to make it a deletion.
	secondValue []byte,
) testConsecutiveWrites {
	t.Helper()

	first := newTestBudlet(t, testStateKey, []byte("first value"), 40)
	second, err := NewBudlet([]byte(testStateKey), secondValue, testFirstHeight)
	require.NoError(t, err)
	var writes testConsecutiveWrites
	for i, write := range []struct {
		height uint64
		budlet *Budlet
	}{{testFirstHeight, first}, {testSecondHeight, second}} {
		tree := newTestBUDTreeWith(t, rng, write.budlet)
		proof, found := tree.BuildBUDProof(write.budlet.Key())
		require.True(t, found)
		ahd := apphash.NewAppHashData(testChainID, write.height, [32]byte{byte(i)}, [32]byte{0xb2}, tree.BUD(),
			[32]byte{0xd4}, [32]byte{0xe5})
		writes.appHashData = append(writes.appHashData, ahd)
		writes.budProofs = append(writes.budProofs, proof)
	}
	return writes
}

// newTestBUDTreeWith returns a BUD tree holding budlet among random other budlets.
func newTestBUDTreeWith(t *testing.T, rng *rand.Rand, budlet *Budlet) *BUDTree {
	t.Helper()

	budlets := slices.DeleteFunc(randomBudlets(t, rng, 6), func(other *Budlet) bool {
		return bytes.Equal(other.Key(), budlet.Key())
	})
	budlets = append(budlets, budlet)
	slices.SortFunc(budlets, func(a *Budlet, b *Budlet) int {
		return bytes.Compare(a.Key(), b.Key())
	})
	tree, err := NewBUDTree(budlets)
	require.NoError(t, err)
	return tree
}

func TestBUDStateProofOfOneWrite(t *testing.T) {
	writes := newTestConsecutiveWrites(t, rand.New(rand.NewSource(6)), []byte("second value"))
	proof, err := NewBUDStateProof(writes.appHashData[:1], writes.budProofs[:1])
	require.NoError(t, err)

	require.Equal(t, []byte(testStateKey), proof.Key())
	require.Equal(t, uint64(testChainID), proof.ChainID())
	require.Equal(t, uint64(testFirstHeight), proof.StartHeight())
	require.Equal(t, uint64(testFirstHeight), proof.EndHeight())
	require.Equal(t, [][32]byte{writes.appHashData[0].AppHash()}, proof.AppHashes())
	value, found := proof.ValueAt(testFirstHeight)
	require.True(t, found)
	require.Equal(t, []byte("first value"), value)
	for _, height := range []uint64{testFirstHeight - 1, testFirstHeight + 1} {
		_, found := proof.ValueAt(height)
		require.False(t, found, "height %d", height)
	}
}

func TestBUDStateProofOfConsecutiveWrites(t *testing.T) {
	writes := newTestConsecutiveWrites(t, rand.New(rand.NewSource(7)), []byte("second value"))
	proof, err := NewBUDStateProof(writes.appHashData, writes.budProofs)
	require.NoError(t, err)

	require.Equal(t, uint64(testChainID), proof.ChainID())
	require.Equal(t, uint64(testFirstHeight), proof.StartHeight())
	require.Equal(t, uint64(testSecondHeight), proof.EndHeight())
	appHashes := [][32]byte{writes.appHashData[0].AppHash(), writes.appHashData[1].AppHash()}
	require.Equal(t, appHashes, proof.AppHashes())
	for height := uint64(testFirstHeight); height < testSecondHeight; height++ {
		value, found := proof.ValueAt(height)
		require.True(t, found, "height %d", height)
		require.Equal(t, []byte("first value"), value, "height %d", height)
	}
	value, found := proof.ValueAt(testSecondHeight)
	require.True(t, found)
	require.Equal(t, []byte("second value"), value)
	for _, height := range []uint64{testFirstHeight - 1, testSecondHeight + 1} {
		_, found := proof.ValueAt(height)
		require.False(t, found, "height %d", height)
	}
}

func TestBUDStateProofOfWriteThenDeletion(t *testing.T) {
	writes := newTestConsecutiveWrites(t, rand.New(rand.NewSource(12)), nil)
	proof, err := NewBUDStateProof(writes.appHashData, writes.budProofs)
	require.NoError(t, err)

	value, found := proof.ValueAt(testSecondHeight - 1)
	require.True(t, found)
	require.Equal(t, []byte("first value"), value)
	value, found = proof.ValueAt(testSecondHeight)
	require.True(t, found)
	require.Nil(t, value)
}

func TestNewBUDStateProofRejectsWrongBUD(t *testing.T) {
	writes := newTestConsecutiveWrites(t, rand.New(rand.NewSource(8)), []byte("second value"))
	for i := range writes.appHashData {
		appHashData := slices.Clone(writes.appHashData)
		original := appHashData[i]
		wrongBUD := original.BUD()
		wrongBUD[0] ^= 1
		appHashData[i] = apphash.NewAppHashData(original.ChainID(), original.BlockHeight(),
			original.BlockHash(), original.StateHash(), wrongBUD, original.ReceiptHash(),
			original.PreviousAppHash())

		_, err := NewBUDStateProof(appHashData, writes.budProofs)
		require.Error(t, err, "pair %d", i)
	}
}

func TestNewBUDStateProofRejectsInconsistentPairs(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	writes := newTestConsecutiveWrites(t, rng, []byte("second value"))
	first, second := writes.appHashData[0], writes.appHashData[1]
	withHeightAndChain := func(ahd *apphash.AppHashData, height uint64, chainID uint64) *apphash.AppHashData {
		return apphash.NewAppHashData(chainID, height, ahd.BlockHash(), ahd.StateHash(), ahd.BUD(),
			ahd.ReceiptHash(), ahd.PreviousAppHash())
	}
	// A second write that breaks one condition, paired with app hash data holding its own tree's BUD so that
	// only that condition fails.
	secondWriteOf := func(budlet *Budlet) (*apphash.AppHashData, *BUDProof) {
		tree := newTestBUDTreeWith(t, rng, budlet)
		proof, found := tree.BuildBUDProof(budlet.Key())
		require.True(t, found)
		return apphash.NewAppHashData(testChainID, testSecondHeight, second.BlockHash(), second.StateHash(),
			tree.BUD(), second.ReceiptHash(), second.PreviousAppHash()), proof
	}
	otherKeyAppHashData, otherKeyProof := secondWriteOf(newTestBudlet(t, "evm/other-key", []byte{0x01},
		testFirstHeight))
	wrongPreviousAppHashData, wrongPreviousProof := secondWriteOf(newTestBudlet(t, testStateKey, []byte{0x01},
		testFirstHeight-1))

	testCases := map[string]struct {
		appHashData []*apphash.AppHashData
		budProofs   []*BUDProof
	}{
		"no pairs": {nil, nil},
		"three pairs": {
			[]*apphash.AppHashData{first, first, second},
			slices.Concat(writes.budProofs, writes.budProofs[1:]),
		},
		"unequal counts": {writes.appHashData, writes.budProofs[:1]},
		"different keys": {
			[]*apphash.AppHashData{first, otherKeyAppHashData},
			[]*BUDProof{writes.budProofs[0], otherKeyProof},
		},
		"wrong previous height": {
			[]*apphash.AppHashData{first, wrongPreviousAppHashData},
			[]*BUDProof{writes.budProofs[0], wrongPreviousProof},
		},
		"reversed order": {
			[]*apphash.AppHashData{second, first},
			[]*BUDProof{writes.budProofs[1], writes.budProofs[0]},
		},
		"equal heights": {
			[]*apphash.AppHashData{first, withHeightAndChain(second, testFirstHeight, testChainID)},
			writes.budProofs,
		},
		"different chain": {
			[]*apphash.AppHashData{first, withHeightAndChain(second, testSecondHeight, testChainID+1)},
			writes.budProofs,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := NewBUDStateProof(testCase.appHashData, testCase.budProofs)
			require.Error(t, err)
		})
	}
}

func TestNewBUDStateProofRejectsNilAndInvalidPairs(t *testing.T) {
	writes := newTestConsecutiveWrites(t, rand.New(rand.NewSource(13)), []byte("second value"))
	first := writes.appHashData[0]
	zeroProof := &BUDProof{}
	// App hash data holding the BUD the zero proof computes, so that only the proof's validity is at fault.
	zeroProofAppHashData := apphash.NewAppHashData(testChainID, testFirstHeight, first.BlockHash(),
		first.StateHash(), zeroProof.ComputeBUD(), first.ReceiptHash(), first.PreviousAppHash())
	missingSiblings := *writes.budProofs[0]
	require.NotEmpty(t, missingSiblings.siblings)
	missingSiblings.siblings = nil
	indexOutOfRange := *writes.budProofs[0]
	indexOutOfRange.index = indexOutOfRange.count

	testCases := map[string]struct {
		appHashData []*apphash.AppHashData
		budProofs   []*BUDProof
	}{
		"nil app hash data":         {[]*apphash.AppHashData{nil}, writes.budProofs[:1]},
		"nil BUD proof":             {writes.appHashData[:1], []*BUDProof{nil}},
		"nil second app hash data":  {[]*apphash.AppHashData{first, nil}, writes.budProofs},
		"nil second BUD proof":      {writes.appHashData, []*BUDProof{writes.budProofs[0], nil}},
		"zero BUD proof":            {[]*apphash.AppHashData{zeroProofAppHashData}, []*BUDProof{zeroProof}},
		"missing siblings":          {writes.appHashData[:1], []*BUDProof{&missingSiblings}},
		"index not less than count": {writes.appHashData[:1], []*BUDProof{&indexOutOfRange}},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := NewBUDStateProof(testCase.appHashData, testCase.budProofs)
			require.Error(t, err)
		})
	}
}

func TestBUDStateProofSerializationRoundTrip(t *testing.T) {
	writes := newTestConsecutiveWrites(t, rand.New(rand.NewSource(10)), []byte("second value"))
	for count := 1; count <= 2; count++ {
		proof, err := NewBUDStateProof(writes.appHashData[:count], writes.budProofs[:count])
		require.NoError(t, err)

		deserialized, err := DeserializeBUDStateProof(proof.Serialize())
		require.NoError(t, err, "count %d", count)
		require.Equal(t, proof, deserialized, "count %d", count)
	}
}

func TestDeserializeBUDStateProofRejectsMalformedInput(t *testing.T) {
	writes := newTestConsecutiveWrites(t, rand.New(rand.NewSource(11)), []byte("second value"))
	serializePair := func(appHashData []byte, budProof *BUDProof) []byte {
		length := uint32(len(appHashData)) //nolint:gosec // G115 - small test data
		data := binary.BigEndian.AppendUint32(nil, length)
		data = append(data, appHashData...)
		return append(data, budProof.Serialize()...)
	}
	first := serializePair(writes.appHashData[0].Serialize(), writes.budProofs[0])
	second := serializePair(writes.appHashData[1].Serialize(), writes.budProofs[1])
	serialize := func(version uint8, count uint8, pairs ...[]byte) []byte {
		return slices.Concat(append([][]byte{{version, count}}, pairs...)...)
	}
	valid := serialize(budStateProofVersion, 2, first, second)
	_, err := DeserializeBUDStateProof(valid)
	require.NoError(t, err)

	shortAppHashData := writes.appHashData[0].Serialize()
	shortAppHashData = shortAppHashData[:len(shortAppHashData)-1]
	shortAppHashPair := serializePair(shortAppHashData, writes.budProofs[0])
	mismatchedPair := serializePair(writes.appHashData[0].Serialize(), writes.budProofs[1])
	hugeAppHashLength := append([]byte{0xff, 0xff, 0xff, 0xff}, first[4:]...)
	testCases := map[string][]byte{
		"empty":                   nil,
		"unsupported version":     serialize(budStateProofVersion+1, 2, first, second),
		"missing count":           {budStateProofVersion},
		"zero count":              serialize(budStateProofVersion, 0),
		"three pairs":             serialize(budStateProofVersion, 3, first, second, second),
		"count too high":          serialize(budStateProofVersion, 2, first),
		"count too low":           serialize(budStateProofVersion, 1, first, second),
		"app hash length too big": serialize(budStateProofVersion, 1, hugeAppHashLength),
		"short app hash data":     serialize(budStateProofVersion, 1, shortAppHashPair),
		"malformed BUD proof":     serialize(budStateProofVersion, 1, first[:len(first)-1]),
		"reversed pairs":          serialize(budStateProofVersion, 2, second, first),
		"mismatched BUD":          serialize(budStateProofVersion, 1, mismatchedPair),
		"trailing byte":           append(slices.Clone(valid), 0),
	}
	for name, data := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := DeserializeBUDStateProof(data)
			require.Error(t, err)
		})
	}
}
