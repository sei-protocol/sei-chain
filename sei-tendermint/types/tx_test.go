package types

import (
	"bytes"
	"math/rand"
	"testing"

	ctest "github.com/sei-protocol/sei-chain/sei-tendermint/internal/libs/test"
	tmrand "github.com/sei-protocol/sei-chain/sei-tendermint/libs/rand"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func makeTxs(cnt, size int) Txs {
	txs := make(Txs, cnt)
	for i := 0; i < cnt; i++ {
		txs[i] = tmrand.Bytes(size)
	}
	return txs
}

func TestTxIndex(t *testing.T) {
	for i := 0; i < 20; i++ {
		txs := makeTxs(15, 60)
		for j := 0; j < len(txs); j++ {
			tx := txs[j]
			idx := txs.Index(tx)
			require.Equal(t, j, idx)
		}
		require.Equal(t, -1, txs.Index(nil))
		require.Equal(t, -1, txs.Index(Tx("foodnwkf")))
	}
}

func TestTxIndexByHash(t *testing.T) {
	for i := 0; i < 20; i++ {
		txs := makeTxs(15, 60)
		for j := 0; j < len(txs); j++ {
			tx := txs[j]
			idx := txs.IndexByHash(tx.Hash())
			require.Equal(t, j, idx)
		}
		require.Equal(t, -1, txs.IndexByHash(TxHash{}))
		require.Equal(t, -1, txs.IndexByHash(Tx("foodnwkf").Hash()))
	}
}

func TestValidTxProof(t *testing.T) {
	cases := []struct {
		txs Txs
	}{
		{Txs{{1, 4, 34, 87, 163, 1}}},
		{Txs{{5, 56, 165, 2}, {4, 77}}},
		{Txs{Tx("foo"), Tx("bar"), Tx("baz")}},
		{makeTxs(20, 5)},
		{makeTxs(7, 81)},
		{makeTxs(61, 15)},
	}

	for h, tc := range cases {
		txs := tc.txs
		root := txs.Hash()
		// make sure valid proof for every tx
		for i := range txs {
			tx := []byte(txs[i])
			proof := txs.Proof(i)
			require.Equal(t, int64(i), proof.Proof.Index, "%d: %d", h, i)
			require.Equal(t, int64(len(txs)), proof.Proof.Total, "%d: %d", h, i)
			require.Equal(t, root, proof.RootHash, "%d: %d", h, i)
			require.Equal(t, tx, []byte(proof.Data), "%d: %d", h, i)
			hash := txs[i].Hash()
			require.Equal(t, hash.Bytes().Bytes(), proof.Leaf(), "%d: %d", h, i)
			require.NoError(t, proof.Validate(root), "%d: %d", h, i)
			require.Error(t, proof.Validate([]byte("foobar")), "%d: %d", h, i)

			// read-write must also work
			var (
				p2  TxProof
				pb2 tmproto.TxProof
			)
			pbProof := proof.ToProto()
			bin, err := pbProof.Marshal()
			require.NoError(t, err)

			err = pb2.Unmarshal(bin)
			require.NoError(t, err)

			p2, err = TxProofFromProto(pb2)
			require.NoError(t, err, "%d: %d: %+v", h, i, err)
			require.NoError(t, p2.Validate(root), "%d: %d", h, i)
		}
	}
}

func TestTxProofUnchangable(t *testing.T) {
	// run the other test a bunch...
	for i := 0; i < 40; i++ {
		testTxProofUnchangable(t)
	}
}

func testTxProofUnchangable(t *testing.T) {
	// make some proof
	txs := makeTxs(randInt(2, 100), randInt(16, 128))
	root := txs.Hash()
	i := randInt(0, len(txs)-1)
	proof := txs.Proof(i)

	// make sure it is valid to start with
	require.NoError(t, proof.Validate(root))
	pbProof := proof.ToProto()
	bin, err := pbProof.Marshal()
	require.NoError(t, err)

	// try mutating the data and make sure nothing breaks
	for j := 0; j < 500; j++ {
		bad := ctest.MutateByteSlice(bin)
		if !bytes.Equal(bad, bin) {
			assertBadProof(t, root, bad, proof)
		}
	}
}

// This makes sure that the proof doesn't deserialize into something valid.
func assertBadProof(t *testing.T, root []byte, bad []byte, good TxProof) {

	var (
		proof   TxProof
		pbProof tmproto.TxProof
	)
	err := pbProof.Unmarshal(bad)
	if err == nil {
		proof, err = TxProofFromProto(pbProof)
		if err == nil {
			err = proof.Validate(root)
			if err == nil {
				// XXX Fix simple merkle proofs so the following is *not* OK.
				// This can happen if we have a slightly different total (where the
				// path ends up the same). If it is something else, we have a real
				// problem.
				require.NotEqual(t, proof.Proof.Total, good.Proof.Total, "bad: %#v\ngood: %#v", proof, good)
			}
		}
	}
}

func randInt(low, high int) int {
	return rand.Intn(high-low) + low
}
