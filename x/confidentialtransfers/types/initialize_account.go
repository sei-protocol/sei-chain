package types

import (
	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

type InitializeAccount struct {
	FromAddress        string                   `json:"from_address"`
	Denom              string                   `json:"denom"`
	Pubkey             *curves.Point            `json:"pubkey"`
	PendingAmountLo    *elgamal.Ciphertext      `json:"pending_amount_lo"`
	PendingAmountHi    *elgamal.Ciphertext      `json:"pending_amount_hi"`
	AvailableBalance   *elgamal.Ciphertext      `json:"available_balance"`
	DecryptableBalance string                   `json:"decryptable_balance"`
	Proofs             *InitializeAccountProofs `json:"proofs"`
}

type InitializeAccountProofs struct {
	PubkeyValidityProof       *zkproofs.PubKeyValidityProof `json:"pubkey_validity_proof"`
	ZeroPendingBalanceLoProof *zkproofs.ZeroBalanceProof    `json:"zero_pending_balance_lo_proof"`
	ZeroPendingBalanceHiProof *zkproofs.ZeroBalanceProof    `json:"zero_pending_balance_hi_proof"`
	ZeroAvailableBalanceProof *zkproofs.ZeroBalanceProof    `json:"zero_available_balance_proof"`
}
