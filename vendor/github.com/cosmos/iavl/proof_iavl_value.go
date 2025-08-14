package iavl

import (
	"fmt"

	proto "github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/tendermint/tendermint/crypto/merkle"
	tmmerkle "github.com/tendermint/tendermint/proto/tendermint/crypto"

	"github.com/cosmos/iavl/internal/encoding"
	iavlproto "github.com/cosmos/iavl/proto"
)

const ProofOpIAVLValue = "iavl:v"

// IAVLValueOp takes a key and a single value as argument and
// produces the root hash.
//
// If the produced root hash matches the expected hash, the proof
// is good.
type ValueOp struct {
	// Encoded in ProofOp.Key.
	key []byte

	// To encode in ProofOp.Data.
	// Proof is nil for an empty tree.
	// The hash of an empty tree is nil.
	Proof *RangeProof `json:"proof"`
}

var _ merkle.ProofOperator = ValueOp{}

func NewValueOp(key []byte, proof *RangeProof) ValueOp {
	return ValueOp{
		key:   key,
		Proof: proof,
	}
}

func ValueOpDecoder(pop tmmerkle.ProofOp) (merkle.ProofOperator, error) {
	if pop.Type != ProofOpIAVLValue {
		return nil, errors.Errorf("unexpected ProofOp.Type; got %v, want %v", pop.Type, ProofOpIAVLValue)
	}
	// Strip the varint length prefix, used for backwards compatibility with Amino.
	bz, n, err := encoding.DecodeBytes(pop.Data)
	if err != nil {
		return nil, err
	}
	if n != len(pop.Data) {
		return nil, fmt.Errorf("unexpected bytes, expected %v got %v", n, len(pop.Data))
	}
	pbProofOp := &iavlproto.ValueOp{}
	err = proto.Unmarshal(bz, pbProofOp)
	if err != nil {
		return nil, err
	}
	proof, err := RangeProofFromProto(pbProofOp.Proof)
	if err != nil {
		return nil, err
	}
	return NewValueOp(pop.Key, &proof), nil
}

func (op ValueOp) ProofOp() tmmerkle.ProofOp {
	pbProof := iavlproto.ValueOp{Proof: op.Proof.ToProto()}
	bz, err := proto.Marshal(&pbProof)
	if err != nil {
		panic(err)
	}
	// We length-prefix the byte slice to retain backwards compatibility with the Amino proofs.
	bz, err = encoding.EncodeBytesSlice(bz)
	if err != nil {
		panic(err)
	}
	return tmmerkle.ProofOp{
		Type: ProofOpIAVLValue,
		Key:  op.key,
		Data: bz,
	}
}

func (op ValueOp) String() string {
	return fmt.Sprintf("IAVLValueOp{%v}", op.GetKey())
}

func (op ValueOp) Run(args [][]byte) ([][]byte, error) {
	if len(args) != 1 {
		return nil, errors.New("value size is not 1")
	}
	value := args[0]

	// Compute the root hash and assume it is valid.
	// The caller checks the ultimate root later.
	root := op.Proof.ComputeRootHash()
	err := op.Proof.Verify(root)
	if err != nil {
		return nil, errors.Wrap(err, "computing root hash")
	}
	// XXX What is the encoding for keys?
	// We should decode the key depending on whether it's a string or hex,
	// maybe based on quotes and 0x prefix?
	err = op.Proof.VerifyItem(op.key, value)
	if err != nil {
		return nil, errors.Wrap(err, "verifying value")
	}
	return [][]byte{root}, nil
}

func (op ValueOp) GetKey() []byte {
	return op.key
}
