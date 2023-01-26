// nolint: errcheck
package iavl

import (
	"bytes"
	"sort"
	"testing"

	proto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	iavlrand "github.com/cosmos/iavl/internal/rand"
	iavlproto "github.com/cosmos/iavl/proto"
)

func TestTreeGetWithProof(t *testing.T) {
	tree, err := getTestTree(0)
	require.NoError(t, err)
	require := require.New(t)
	for _, ikey := range []byte{0x11, 0x32, 0x50, 0x72, 0x99} {
		key := []byte{ikey}
		tree.Set(key, []byte(iavlrand.RandStr(8)))
	}
	root, err := tree.WorkingHash()
	require.NoError(err)

	key := []byte{0x32}
	val, proof, err := tree.GetWithProof(key)
	require.NoError(err)
	require.NotEmpty(val)
	require.NotNil(proof)

	err = proof.VerifyItem(key, val)
	require.Error(err, "%+v", err) // Verifying item before calling Verify(root)

	err = proof.Verify(root)
	require.NoError(err, "%+v", err)

	err = proof.VerifyItem(key, val)
	require.NoError(err, "%+v", err)

	key = []byte{0x1}
	val, proof, err = tree.GetWithProof(key)
	require.NoError(err)
	require.Empty(val)
	require.NotNil(proof)

	err = proof.VerifyAbsence(key)
	require.Error(err, "%+v", err) // Verifying absence before calling Verify(root)

	err = proof.Verify(root)
	require.NoError(err, "%+v", err)

	err = proof.VerifyAbsence(key)
	require.NoError(err, "%+v", err)
}

func TestTreeKeyExistsProof(t *testing.T) {
	tree, err := getTestTree(0)
	require.NoError(t, err)
	root, err := tree.WorkingHash()
	require.NoError(t, err)

	// should get false for proof with nil root
	proof, keys, values, err := tree.getRangeProof([]byte("foo"), nil, 1)
	assert.Nil(t, proof)
	assert.Error(t, proof.Verify(root))
	assert.Nil(t, keys)
	assert.Nil(t, values)
	assert.NoError(t, err)

	// insert lots of info and store the bytes
	allkeys := make([][]byte, 200)
	for i := 0; i < 200; i++ {
		key := iavlrand.RandStr(20)
		value := "value_for_" + key
		tree.Set([]byte(key), []byte(value))
		allkeys[i] = []byte(key)
	}
	sortByteSlices(allkeys) // Sort all keys
	root, err = tree.WorkingHash()
	require.NoError(t, err)

	// query random key fails
	proof, _, _, err = tree.getRangeProof([]byte("foo"), nil, 2)
	assert.Nil(t, err)
	assert.Nil(t, proof.Verify(root))
	assert.Nil(t, proof.VerifyAbsence([]byte("foo")), proof.String())

	// query min key fails
	proof, _, _, err = tree.getRangeProof([]byte{0x00}, []byte{0x01}, 2)
	assert.Nil(t, err)
	assert.Nil(t, proof.Verify(root))
	assert.Nil(t, proof.VerifyAbsence([]byte{0x00}))

	// valid proof for real keys
	for i, key := range allkeys {
		var keys, values [][]byte
		proof, keys, values, err = tree.getRangeProof(key, nil, 2)
		require.Nil(t, err)

		require.Equal(t,
			append([]byte("value_for_"), key...),
			values[0],
		)
		require.Equal(t, key, keys[0])
		require.Nil(t, proof.Verify(root))
		require.Nil(t, proof.VerifyAbsence(cpIncr(key)))
		require.Equal(t, 1, len(keys), proof.String())
		require.Equal(t, 1, len(values), proof.String())
		if i < len(allkeys)-1 {
			if i < len(allkeys)-2 {
				// No last item... not a proof of absence of large key.
				require.NotNil(t, proof.VerifyAbsence(bytes.Repeat([]byte{0xFF}, 20)), proof.String())
			} else {
				// Last item is included.
				require.Nil(t, proof.VerifyAbsence(bytes.Repeat([]byte{0xFF}, 20)))
			}
		} else {
			// last item of tree... valid proof of absence of large key.
			require.Nil(t, proof.VerifyAbsence(bytes.Repeat([]byte{0xFF}, 20)))
		}
	}
	// TODO: Test with single value in tree.
}

func TestTreeKeyInRangeProofs(t *testing.T) {
	tree, err := getTestTree(0)
	require.NoError(t, err)
	require := require.New(t)
	keys := []byte{0x0a, 0x11, 0x2e, 0x32, 0x50, 0x72, 0x99, 0xa1, 0xe4, 0xf7} // 10 total.
	for _, ikey := range keys {
		key := []byte{ikey}
		tree.Set(key, key)
	}
	root, err := tree.WorkingHash()
	require.NoError(err)

	// For spacing:
	T := 10
	// disable: don't use underscores in Go names; var nil______ should be nil (golint)
	// nolint
	nil______ := []byte(nil)

	cases := []struct { // nolint:maligned
		start byte
		end   byte
		pkeys []byte // proof keys, one byte per key.
		vals  []byte // keys and values, one byte per key.
		lidx  int64  // proof left index (index of first proof key).
		err   bool   // does error
	}{
		{start: 0x0a, end: 0xf7, pkeys: keys[0:T], vals: keys[0:9], lidx: 0}, // #0
		{start: 0x0a, end: 0xf8, pkeys: keys[0:T], vals: keys[0:T], lidx: 0}, // #1
		{start: 0x00, end: 0xff, pkeys: keys[0:T], vals: keys[0:T], lidx: 0}, // #2
		{start: 0x14, end: 0xe4, pkeys: keys[1:9], vals: keys[2:8], lidx: 1}, // #3
		{start: 0x14, end: 0xe5, pkeys: keys[1:9], vals: keys[2:9], lidx: 1}, // #4
		{start: 0x14, end: 0xe6, pkeys: keys[1:T], vals: keys[2:9], lidx: 1}, // #5
		{start: 0x14, end: 0xf1, pkeys: keys[1:T], vals: keys[2:9], lidx: 1}, // #6
		{start: 0x14, end: 0xf7, pkeys: keys[1:T], vals: keys[2:9], lidx: 1}, // #7
		{start: 0x14, end: 0xff, pkeys: keys[1:T], vals: keys[2:T], lidx: 1}, // #8
		{start: 0x2e, end: 0x31, pkeys: keys[2:4], vals: keys[2:3], lidx: 2}, // #9
		{start: 0x2e, end: 0x32, pkeys: keys[2:4], vals: keys[2:3], lidx: 2}, // #10
		{start: 0x2f, end: 0x32, pkeys: keys[2:4], vals: nil______, lidx: 2}, // #11
		{start: 0x2e, end: 0x31, pkeys: keys[2:4], vals: keys[2:3], lidx: 2}, // #12
		{start: 0x2e, end: 0x2f, pkeys: keys[2:3], vals: keys[2:3], lidx: 2}, // #13
		{start: 0x12, end: 0x31, pkeys: keys[1:4], vals: keys[2:3], lidx: 1}, // #14
		{start: 0xf8, end: 0xff, pkeys: keys[9:T], vals: nil______, lidx: 9}, // #15
		{start: 0x12, end: 0x20, pkeys: keys[1:3], vals: nil______, lidx: 1}, // #16
		{start: 0x00, end: 0x09, pkeys: keys[0:1], vals: nil______, lidx: 0}, // #17
		{start: 0xf7, end: 0x00, err: true},                                  // #18
		{start: 0xf8, end: 0x00, err: true},                                  // #19
		{start: 0x10, end: 0x10, err: true},                                  // #20
		{start: 0x12, end: 0x12, err: true},                                  // #21
		{start: 0xff, end: 0xf7, err: true},                                  // #22
	}

	// fmt.Println("PRINT TREE")
	// printNode(tree.ndb, tree.root, 0)
	// fmt.Println("PRINT TREE END")

	for i, c := range cases {
		t.Logf("case %v", i)
		start := []byte{c.start}
		end := []byte{c.end}

		// Compute range proof.
		keys, values, proof, err := tree.GetRangeWithProof(start, end, 0)

		if c.err {
			require.Error(err, "%+v", err)
		} else {
			require.NoError(err, "%+v", err)
			require.Equal(c.pkeys, flatten(proof.Keys()))
			require.Equal(c.vals, flatten(keys))
			require.Equal(c.vals, flatten(values))
			require.Equal(c.lidx, proof.LeftIndex())

			// Verify that proof is valid.
			err = proof.Verify(root)
			require.NoError(err, "%+v", err)
			verifyProof(t, proof, root)

			// Verify each value of pkeys.
			for _, key := range c.pkeys {
				err := proof.VerifyItem([]byte{key}, []byte{key})
				require.NoError(err)
			}

			// Verify each value of vals.
			for _, key := range c.vals {
				err := proof.VerifyItem([]byte{key}, []byte{key})
				require.NoError(err)
			}
		}

	}
}

func encodeProof(proof *RangeProof) ([]byte, error) {
	return proto.Marshal(proof.ToProto())
}

func decodeProof(bz []byte) (*RangeProof, error) {
	proofOp := &iavlproto.RangeProof{}
	err := proto.Unmarshal(bz, proofOp)
	if err != nil {
		return nil, err
	}
	proof, err := RangeProofFromProto(proofOp)
	return &proof, err
}

func verifyProof(t *testing.T, proof *RangeProof, root []byte) {
	// Proof must verify.
	require.NoError(t, proof.Verify(root))

	// Write/Read then verify.
	proofBytes, err := encodeProof(proof)
	require.NoError(t, err)
	_, err = decodeProof(proofBytes)
	require.NoError(t, err)

	// Random mutations must not verify
	for i := 0; i < 1e4; i++ {
		badProofBytes := MutateByteSlice(proofBytes)
		badProof, err := decodeProof(badProofBytes)
		if err != nil {
			continue // couldn't even decode.
		}
		// re-encode to make sure it's actually different.
		badProofBytes2, err := encodeProof(badProof)
		if bytes.Equal(proofBytes, badProofBytes2) {
			continue // didn't mutate successfully.
		}
		// may be invalid... errors are okay
		if err == nil {
			assert.Errorf(t, badProof.Verify(root),
				"Proof was still valid after a random mutation:\n%X\n%X",
				proofBytes, badProofBytes)
		}
	}
}

//----------------------------------------

func flatten(bzz [][]byte) (res []byte) {
	for _, bz := range bzz {
		res = append(res, bz...)
	}
	return res
}

// Contract: !bytes.Equal(input, output) && len(input) >= len(output)
func MutateByteSlice(bytez []byte) []byte {
	// If bytez is empty, panic
	if len(bytez) == 0 {
		panic("Cannot mutate an empty bytez")
	}

	// Copy bytez
	mBytez := make([]byte, len(bytez))
	copy(mBytez, bytez)
	bytez = mBytez

	// Try a random mutation
	switch iavlrand.RandInt() % 2 {
	case 0: // Mutate a single byte
		bytez[iavlrand.RandInt()%len(bytez)] += byte(iavlrand.RandInt()%255 + 1)
	case 1: // Remove an arbitrary byte
		pos := iavlrand.RandInt() % len(bytez)
		bytez = append(bytez[:pos], bytez[pos+1:]...)
	}
	return bytez
}

func sortByteSlices(src [][]byte) [][]byte {
	bzz := byteslices(src)
	sort.Sort(bzz)
	return bzz
}

type byteslices [][]byte

func (bz byteslices) Len() int {
	return len(bz)
}

func (bz byteslices) Less(i, j int) bool {
	switch bytes.Compare(bz[i], bz[j]) {
	case -1:
		return true
	case 0, 1:
		return false
	default:
		panic("should not happen")
	}
}

//nolint:unused
func (bz byteslices) Swap(i, j int) {
	bz[j], bz[i] = bz[i], bz[j]
}
