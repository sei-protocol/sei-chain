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

const ProofOpIAVLAbsence = "iavl:a"

// IAVLAbsenceOp takes a key as its only argument
//
// If the produced root hash matches the expected hash, the proof
// is good.
type AbsenceOp struct {
	// Encoded in ProofOp.Key.
	key []byte

	// To encode in ProofOp.Data.
	// Proof is nil for an empty tree.
	// The hash of an empty tree is nil.
	Proof *RangeProof `json:"proof"`
}

var _ merkle.ProofOperator = AbsenceOp{}

func NewAbsenceOp(key []byte, proof *RangeProof) AbsenceOp {
	return AbsenceOp{
		key:   key,
		Proof: proof,
	}
}

func AbsenceOpDecoder(pop tmmerkle.ProofOp) (merkle.ProofOperator, error) {
	if pop.Type != ProofOpIAVLAbsence {
		return nil, errors.Errorf("unexpected ProofOp.Type; got %v, want %v", pop.Type, ProofOpIAVLAbsence)
	}
	// Strip the varint length prefix, used for backwards compatibility with Amino.
	bz, n, err := encoding.DecodeBytes(pop.Data)
	if err != nil {
		return nil, err
	}

	if n != len(pop.Data) {
		return nil, fmt.Errorf("unexpected bytes, expected %v got %v", n, len(pop.Data))
	}

	pbProofOp := &iavlproto.AbsenceOp{}
	err = proto.Unmarshal(bz, pbProofOp)
	if err != nil {
		return nil, err
	}

	proof, err := RangeProofFromProto(pbProofOp.Proof)
	if err != nil {
		return nil, err
	}

	return NewAbsenceOp(pop.Key, &proof), nil
}

func (op AbsenceOp) ProofOp() tmmerkle.ProofOp {
	pbProof := iavlproto.AbsenceOp{Proof: op.Proof.ToProto()}
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
		Type: ProofOpIAVLAbsence,
		Key:  op.key,
		Data: bz,
	}
}

func (op AbsenceOp) String() string {
	return fmt.Sprintf("IAVLAbsenceOp{%v}", op.GetKey())
}

func (op AbsenceOp) Run(args [][]byte) ([][]byte, error) {
	if len(args) != 0 {
		return nil, errors.Errorf("expected 0 args, got %v", len(args))
	}

	// If the tree is nil, the proof is nil, and all keys are absent.
	if op.Proof == nil {
		return [][]byte{[]byte(nil)}, nil
	}

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
	err = op.Proof.VerifyAbsence(op.key)
	if err != nil {
		return nil, errors.Wrap(err, "verifying absence")
	}

	return [][]byte{root}, nil
}

func (op AbsenceOp) GetKey() []byte {
	return op.key
}
