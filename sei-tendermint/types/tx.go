package types

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/merkle"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// Tx is an arbitrary byte array.
// NOTE: Tx has no types at this level, so when wire encoded it's just length-prefixed.
// Might we want types here ?
type Tx []byte

// Key produces a fixed-length key for use in indexing.
func (tx Tx) Key() TxKey { return sha256.Sum256(tx) }

// Hash computes the TMHASH hash of the wire encoded transaction.
func (tx Tx) Hash() []byte { return crypto.Checksum(tx) }

// String returns the hex-encoded transaction as a string.
func (tx Tx) String() string { return fmt.Sprintf("Tx{%X}", []byte(tx)) }

// Txs is a slice of Tx.
type Txs []Tx

// Hash returns the Merkle root hash of the transaction hashes.
// i.e. the leaves of the tree are the hashes of the txs.
func (txs Txs) Hash() []byte {
	hl := txs.hashList()
	return merkle.HashFromByteSlices(hl)
}

// Index returns the index of this transaction in the list, or -1 if not found
func (txs Txs) Index(tx Tx) int {
	for i := range txs {
		if bytes.Equal(txs[i], tx) {
			return i
		}
	}
	return -1
}

// IndexByHash returns the index of this transaction hash in the list, or -1 if not found
func (txs Txs) IndexByHash(hash []byte) int {
	for i := range txs {
		if bytes.Equal(txs[i].Hash(), hash) {
			return i
		}
	}
	return -1
}

func (txs Txs) Proof(i int) TxProof {
	hl := txs.hashList()
	root, proofs := merkle.ProofsFromByteSlices(hl)

	return TxProof{
		RootHash: root,
		Data:     txs[i],
		Proof:    *proofs[i],
	}
}

func (txs Txs) hashList() [][]byte {
	hl := make([][]byte, len(txs))
	for i := 0; i < len(txs); i++ {
		hl[i] = txs[i].Hash()
	}
	return hl
}

// Txs is a slice of transactions. Sorting a Txs value orders the transactions
// lexicographically.
func (txs Txs) Len() int      { return len(txs) }
func (txs Txs) Swap(i, j int) { txs[i], txs[j] = txs[j], txs[i] }
func (txs Txs) Less(i, j int) bool {
	return bytes.Compare(txs[i], txs[j]) == -1
}

// ToSliceOfBytes converts a Txs to slice of byte slices.
func (txs Txs) ToSliceOfBytes() [][]byte {
	txBzs := make([][]byte, len(txs))
	for i := 0; i < len(txs); i++ {
		txBzs[i] = txs[i]
	}
	return txBzs
}

func sortedCopy(txs Txs) Txs {
	cp := make(Txs, len(txs))
	copy(cp, txs)
	sort.Sort(cp)
	return cp
}

// containsAny checks that list a contains one of the transactions in list
// b. If a match is found, the index in b of the matching transaction is returned.
// Both lists must be sorted.
func containsAny(a, b []Tx) (int, bool) {
	for i, cur := range b {
		if _, ok := contains(a, cur); ok {
			return i, true
		}
	}
	return -1, false
}

// containsAll checks that super contains all of the transactions in the sub
// list. If not all values in sub are present in super, the index in sub of the
// first Tx absent from super is returned.
func containsAll(super, sub Txs) (int, bool) {
	for i, cur := range sub {
		if _, ok := contains(super, cur); !ok {
			return i, false
		}
	}
	return -1, true
}

// contains checks that the sorted list, set contains elem. If set does contain elem, then the
// index in set of elem is returned.
func contains(set []Tx, elem Tx) (int, bool) {
	n := sort.Search(len(set), func(i int) bool {
		return bytes.Compare(elem, set[i]) <= 0
	})
	if n == len(set) || !bytes.Equal(elem, set[n]) {
		return -1, false
	}
	return n, true
}

// TxProof represents a Merkle proof of the presence of a transaction in the Merkle tree.
type TxProof struct {
	RootHash tmbytes.HexBytes `json:"root_hash"`
	Data     Tx               `json:"data"`
	Proof    merkle.Proof     `json:"proof"`
}

// Leaf returns the hash(tx), which is the leaf in the merkle tree which this proof refers to.
func (tp TxProof) Leaf() []byte {
	return tp.Data.Hash()
}

// Validate verifies the proof. It returns nil if the RootHash matches the dataHash argument,
// and if the proof is internally consistent. Otherwise, it returns a sensible error.
func (tp TxProof) Validate(dataHash []byte) error {
	if !bytes.Equal(dataHash, tp.RootHash) {
		return errors.New("proof matches different data hash")
	}
	if tp.Proof.Index < 0 {
		return errors.New("proof index cannot be negative")
	}
	if tp.Proof.Total <= 0 {
		return errors.New("proof total must be positive")
	}
	valid := tp.Proof.Verify(tp.RootHash, tp.Leaf())
	if valid != nil {
		return errors.New("proof is not internally consistent")
	}
	return nil
}

func (tp TxProof) ToProto() tmproto.TxProof {

	pbProof := tp.Proof.ToProto()

	pbtp := tmproto.TxProof{
		RootHash: tp.RootHash,
		Data:     tp.Data,
		Proof:    pbProof,
	}

	return pbtp
}
func TxProofFromProto(pb tmproto.TxProof) (TxProof, error) {

	pbProof, err := merkle.ProofFromProto(pb.Proof)
	if err != nil {
		return TxProof{}, err
	}

	pbtp := TxProof{
		RootHash: pb.RootHash,
		Data:     pb.Data,
		Proof:    *pbProof,
	}

	return pbtp, nil
}

// ComputeProtoSizeForTxs wraps the transactions in tmproto.Data{} and calculates the size.
// https://developers.google.com/protocol-buffers/docs/encoding
func ComputeProtoSizeForTxs(txs []Tx) int64 {
	data := Data{Txs: txs}
	pdData := data.ToProto()
	return int64(pdData.Size())
}
