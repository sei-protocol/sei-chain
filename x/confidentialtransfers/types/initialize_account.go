package types

import (
	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

type InitializeAccount struct {
	FromAddress        string                   `json:"from_address"`
	Denom              string                   `json:"denom"`
	Pubkey             *curves.Point            `json:"pubkey"`
	DecryptableBalance string                   `json:"decryptable_balance"`
	Proofs             *InitializeAccountProofs `json:"proofs"`
}

type InitializeAccountProofs struct {
	PubkeyValidityProof *zkproofs.PubKeyValidityProof `json:"pubkey_validity_proof"`
}
