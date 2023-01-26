package iavl_test

import (
	"encoding/hex"
	"math/rand"
	"strings"
	"testing"

	"github.com/cosmos/iavl"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/crypto/tmhash"
	db "github.com/tendermint/tm-db"
)

func TestProofFogery(t *testing.T) {
	source := rand.NewSource(0)
	r := rand.New(source)
	cacheSize := 0
	tree, err := iavl.NewMutableTreeWithOpts(db.NewMemDB(), cacheSize, nil, false)
	require.NoError(t, err)

	// two keys only
	keys := []byte{0x11, 0x32}
	values := make([][]byte, len(keys))
	// make random values and insert into tree
	for i, ikey := range keys {
		key := []byte{ikey}
		v := r.Intn(255)
		values[i] = []byte{byte(v)}
		tree.Set(key, values[i])
	}

	// get root
	root, err := tree.WorkingHash()
	require.NoError(t, err)
	// use the rightmost kv pair in the tree so the inner nodes will populate left
	k := []byte{keys[1]}
	v := values[1]

	val, proof, err := tree.GetWithProof(k)
	require.NoError(t, err)

	err = proof.Verify(root)
	require.NoError(t, err)
	err = proof.VerifyItem(k, val)
	require.NoError(t, err)

	// ------------------- FORGE PROOF -------------------

	forgedPayloadBytes := mustDecode("0xabcd")
	forgedValueHash := tmhash.Sum(forgedPayloadBytes)
	// make a forgery of the proof by adding:
	// - a new leaf node to the right
	// - an empty inner node
	// - a right entry in the path
	_, proof2, _ := tree.GetWithProof(k)
	forgedNode := proof2.Leaves[0]
	forgedNode.Key = []byte{0xFF}
	forgedNode.ValueHash = forgedValueHash
	proof2.Leaves = append(proof2.Leaves, forgedNode)
	proof2.InnerNodes = append(proof2.InnerNodes, iavl.PathToLeaf{})
	// figure out what hash we need via https://twitter.com/samczsun/status/1578181160345034752
	proof2.LeftPath[0].Right = mustDecode("82C36CED85E914DAE8FDF6DD11FD5833121AA425711EB126C470CE28FF6623D5")

	rootHashValid := proof.ComputeRootHash()
	verifyErr := proof.Verify(rootHashValid)
	require.NoError(t, verifyErr, "should verify")
	// forgery gives empty root hash (previously it returned the same one!)
	rootHashForged := proof2.ComputeRootHash()
	require.Empty(t, rootHashForged, "roothash must be empty if both left and right are set")
	verifyErr = proof2.Verify(rootHashForged)
	require.Error(t, verifyErr, "should not verify")

	// verify proof two fails with valid proof
	err = proof2.Verify(rootHashValid)
	require.Error(t, err, "should not verify different root hash")

	{
		// legit node verifies against legit proof (expected)
		verifyErr = proof.VerifyItem(k, v)
		require.NoError(t, verifyErr, "valid proof should verify")
		// forged node fails to verify against legit proof (expected)
		verifyErr = proof.VerifyItem(forgedNode.Key, forgedPayloadBytes)
		require.Error(t, verifyErr, "forged proof should fail to verify")
	}
	{
		// legit node fails to verify against forged proof (expected)
		verifyErr = proof2.VerifyItem(k, v)
		require.Error(t, verifyErr, "valid proof should verify, but has a forged sister node")

		// forged node fails to verify against forged proof (previously this succeeded!)
		verifyErr = proof2.VerifyItem(forgedNode.Key, forgedPayloadBytes)
		require.Error(t, verifyErr, "forged proof should fail verify")
	}
}

func mustDecode(str string) []byte {
	if strings.HasPrefix(str, "0x") {
		str = str[2:]
	}
	b, err := hex.DecodeString(str)
	if err != nil {
		panic(err)
	}
	return b
}
