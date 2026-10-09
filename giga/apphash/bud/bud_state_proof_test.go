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

	// testNotModifiedSince is the not-modified-since height of the write of testStateKey.
	testNotModifiedSince = 100

	// testWriteHeight is the height of the block that writes testStateKey.
	testWriteHeight = 107
)

// newTestWrite returns the app hash data of a block at height holding budlet among random other budlets, and the
// BUD proof of budlet against that block's BUD.
func newTestWrite(
	t *testing.T,
	// The source of the other budlets in the block.
	rng *rand.Rand,
	// The budlet to prove.
	budlet *Budlet,
	// The height of the block.
	height uint64,
) (*apphash.AppHashData, *BUDProof) {
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
	proof, found := tree.BuildBUDProof(budlet.Key())
	require.True(t, found)
	appHashData := apphash.NewAppHashData(testChainID, height, [32]byte{0xa1}, [32]byte{0xb2}, tree.BUD(),
		[32]byte{0xd4}, [32]byte{0xe5})
	return appHashData, proof
}

// newTestBUDStateProof returns the BUD state proof of testStateKey written at testWriteHeight, failing the test on
// an error.
func newTestBUDStateProof(
	t *testing.T,
	// The source of the other budlets in the block.
	rng *rand.Rand,
	// The value written, or nil for a deletion.
	value []byte,
	// The key's previous value, or nil if it was absent.
	previousValue []byte,
	// The not-modified-since height of the write.
	notModifiedSince uint64,
) *BUDStateProof {
	t.Helper()

	budlet := newTestBudlet(t, testStateKey, value, previousValue, notModifiedSince)
	stateProof, err := NewBUDStateProof(newTestWrite(t, rng, budlet, testWriteHeight))
	require.NoError(t, err)
	return stateProof
}

func TestBUDStateProofAccessors(t *testing.T) {
	budlet := newTestBudlet(t, testStateKey, []byte("value"), []byte("previous value"), testNotModifiedSince)
	appHashData, budProof := newTestWrite(t, rand.New(rand.NewSource(6)), budlet, testWriteHeight)
	stateProof, err := NewBUDStateProof(appHashData, budProof)
	require.NoError(t, err)

	require.Equal(t, []byte(testStateKey), stateProof.Key())
	require.Equal(t, uint64(testChainID), stateProof.ChainID())
	require.Equal(t, uint64(testNotModifiedSince), stateProof.StartHeight())
	require.Equal(t, uint64(testWriteHeight), stateProof.EndHeight())
	require.Equal(t, appHashData.AppHash(), stateProof.AppHash())
}

func TestBUDStateProofValueAt(t *testing.T) {
	testCases := map[string]struct {
		value            []byte
		previousValue    []byte
		notModifiedSince uint64
	}{
		"write over a value":         {[]byte("value"), []byte("previous value"), testNotModifiedSince},
		"write over an empty value":  {[]byte("value"), []byte{}, testNotModifiedSince},
		"deletion":                   {nil, []byte("previous value"), testNotModifiedSince},
		"write over an absent key":   {[]byte("value"), nil, testNotModifiedSince},
		"not modified since genesis": {[]byte("value"), []byte("previous value"), 0},
		"absent since genesis":       {[]byte("value"), nil, 0},
		"modified one height below":  {[]byte("value"), []byte("previous value"), testWriteHeight - 1},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			stateProof := newTestBUDStateProof(t, rand.New(rand.NewSource(7)), testCase.value,
				testCase.previousValue, testCase.notModifiedSince)
			require.Equal(t, testCase.notModifiedSince, stateProof.StartHeight())
			require.Equal(t, uint64(testWriteHeight), stateProof.EndHeight())

			for height := testCase.notModifiedSince; height < testWriteHeight; height++ {
				value, covered := stateProof.ValueAt(height)
				require.True(t, covered, "height %d", height)
				require.Equal(t, testCase.previousValue == nil, value == nil, "height %d", height)
				require.Equal(t, testCase.previousValue, value, "height %d", height)
			}
			value, covered := stateProof.ValueAt(testWriteHeight)
			require.True(t, covered)
			require.Equal(t, testCase.value == nil, value == nil)
			require.Equal(t, testCase.value, value)

			if testCase.notModifiedSince > 0 {
				_, covered := stateProof.ValueAt(testCase.notModifiedSince - 1)
				require.False(t, covered)
			}
			_, covered = stateProof.ValueAt(testWriteHeight + 1)
			require.False(t, covered)
		})
	}
}

func TestNewBUDStateProofRejectsWrongBUD(t *testing.T) {
	budlet := newTestBudlet(t, testStateKey, []byte("value"), []byte("previous value"), testNotModifiedSince)
	appHashData, budProof := newTestWrite(t, rand.New(rand.NewSource(8)), budlet, testWriteHeight)
	wrongBUD := appHashData.BUD()
	wrongBUD[0] ^= 1
	wrongAppHashData := apphash.NewAppHashData(appHashData.ChainID(), appHashData.BlockHeight(),
		appHashData.BlockHash(), appHashData.StateHash(), wrongBUD, appHashData.ReceiptHash(),
		appHashData.PreviousAppHash())

	_, err := NewBUDStateProof(wrongAppHashData, budProof)
	require.Error(t, err)
}

func TestNewBUDStateProofRejectsNotModifiedSinceNotBelowBlockHeight(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for _, notModifiedSince := range []uint64{testWriteHeight, testWriteHeight + 1} {
		budlet := newTestBudlet(t, testStateKey, []byte("value"), []byte("previous value"), notModifiedSince)
		_, err := NewBUDStateProof(newTestWrite(t, rng, budlet, testWriteHeight))
		require.Error(t, err, "not-modified-since height %d", notModifiedSince)
	}
}

func TestNewBUDStateProofRejectsNilAndInvalidParts(t *testing.T) {
	budlet := newTestBudlet(t, testStateKey, []byte("value"), []byte("previous value"), testNotModifiedSince)
	appHashData, budProof := newTestWrite(t, rand.New(rand.NewSource(13)), budlet, testWriteHeight)
	zeroProof := &BUDProof{}
	// App hash data holding the BUD the zero proof computes, so that only the proof's validity is at fault.
	zeroProofAppHashData := apphash.NewAppHashData(testChainID, testWriteHeight, appHashData.BlockHash(),
		appHashData.StateHash(), zeroProof.ComputeBUD(), appHashData.ReceiptHash(),
		appHashData.PreviousAppHash())
	missingSiblings := *budProof
	require.NotEmpty(t, missingSiblings.siblings)
	missingSiblings.siblings = nil
	indexOutOfRange := *budProof
	indexOutOfRange.index = indexOutOfRange.count

	testCases := map[string]struct {
		appHashData *apphash.AppHashData
		budProof    *BUDProof
	}{
		"nil app hash data":         {nil, budProof},
		"nil BUD proof":             {appHashData, nil},
		"zero BUD proof":            {zeroProofAppHashData, zeroProof},
		"missing siblings":          {appHashData, &missingSiblings},
		"index not less than count": {appHashData, &indexOutOfRange},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := NewBUDStateProof(testCase.appHashData, testCase.budProof)
			require.Error(t, err)
		})
	}
}

func TestBUDStateProofSerializationRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(10))
	for _, stateProof := range []*BUDStateProof{
		newTestBUDStateProof(t, rng, []byte("value"), []byte("previous value"), testNotModifiedSince),
		newTestBUDStateProof(t, rng, nil, []byte{}, testNotModifiedSince),
		newTestBUDStateProof(t, rng, []byte{}, nil, 0),
	} {
		deserialized, err := DeserializeBUDStateProof(stateProof.Serialize())
		require.NoError(t, err)
		require.Equal(t, stateProof, deserialized)
	}
}

func TestDeserializeBUDStateProofRejectsMalformedInput(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	budlet := newTestBudlet(t, testStateKey, []byte("value"), []byte("previous value"), testNotModifiedSince)
	appHashData, budProof := newTestWrite(t, rng, budlet, testWriteHeight)
	_, otherBUDProof := newTestWrite(t, rng, budlet, testWriteHeight)
	serialize := func(version uint8, appHashData []byte, budProof []byte) []byte {
		length := uint32(len(appHashData)) //nolint:gosec // G115 - small test data
		data := binary.BigEndian.AppendUint32([]byte{version}, length)
		data = append(data, appHashData...)
		return append(data, budProof...)
	}
	serializedAppHashData := appHashData.Serialize()
	serializedBUDProof := budProof.Serialize()
	serializedOtherBUDProof := otherBUDProof.Serialize()
	valid := serialize(budStateProofVersion, serializedAppHashData, serializedBUDProof)
	_, err := DeserializeBUDStateProof(valid)
	require.NoError(t, err)

	hugeAppHashDataLength := slices.Concat([]byte{budStateProofVersion, 0xff, 0xff, 0xff, 0xff},
		valid[budStateProofAppHashDataOffset+appHashDataLengthSize:])
	testCases := map[string][]byte{
		"empty":                nil,
		"unsupported version":  serialize(budStateProofVersion+1, serializedAppHashData, serializedBUDProof),
		"missing length":       {budStateProofVersion},
		"truncated length":     valid[:budStateProofAppHashDataOffset+appHashDataLengthSize-1],
		"huge app hash length": hugeAppHashDataLength,
		"short app hash data":  serialize(budStateProofVersion, serializedAppHashData[1:], serializedBUDProof),
		"missing BUD proof":    serialize(budStateProofVersion, serializedAppHashData, nil),
		"truncated BUD proof":  valid[:len(valid)-1],
		"mismatched BUD":       serialize(budStateProofVersion, serializedAppHashData, serializedOtherBUDProof),
		"trailing byte":        append(slices.Clone(valid), 0),
		"malformed app hash":   serialize(budStateProofVersion, []byte{0xff}, serializedBUDProof),
	}
	for name, data := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := DeserializeBUDStateProof(data)
			require.Error(t, err)
		})
	}
}
