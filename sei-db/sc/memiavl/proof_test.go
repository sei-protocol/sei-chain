package memiavl

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProofs(t *testing.T) {
	// do a round test for each version in ChangeSets
	testCases := []struct {
		existKey    []byte
		nonExistKey []byte
	}{
		{[]byte("hello"), []byte("hello1")},
		{[]byte("hello1"), []byte("hello2")},
		{[]byte("hello2"), []byte("hell")},
		{[]byte("hello00"), []byte("hell")},
		{[]byte("hello00"), []byte("hello")},
		{[]byte("aello00"), []byte("hello")},
		{[]byte("hello1"), []byte("aello00")},
	}

	tmpDir := t.TempDir()
	tree := New(0)

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			changes := ChangeSets[i]
			tree.ApplyChangeSet(changes)
			_, _, err := tree.SaveVersion(true)
			require.NoError(t, err)

			proof, err := tree.GetMembershipProof(tc.existKey)
			require.NoError(t, err)
			require.True(t, tree.VerifyMembership(proof, tc.existKey))

			proof, err = tree.GetNonMembershipProof(tc.nonExistKey)
			require.NoError(t, err)
			require.True(t, tree.VerifyNonMembership(proof, tc.nonExistKey))

			// test persisted tree
			require.NoError(t, tree.WriteSnapshot(tmpDir))
			snapshot, err := OpenSnapshot(tmpDir)
			require.NoError(t, err)
			ptree := NewFromSnapshot(snapshot, true, 0)
			defer ptree.Close()

			proof, err = ptree.GetMembershipProof(tc.existKey)
			require.NoError(t, err)
			require.True(t, ptree.VerifyMembership(proof, tc.existKey))

			proof, err = ptree.GetNonMembershipProof(tc.nonExistKey)
			require.NoError(t, err)
			require.True(t, ptree.VerifyNonMembership(proof, tc.nonExistKey))
		})
	}
}
