package types

import (
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
)

type CloseAccount struct {
	Address string `json:"from_address"`
	Denom   string `json:"denom"`

	Proofs *CloseAccountProofs `json:"proofs"`
}

type CloseAccountProofs struct {
	// Proof that the current available balance is zero.
	ZeroAvailableBalanceProof *zkproofs.ZeroBalanceProof `json:"zero_available_balance_proof"`

	// Proof that the current pending balance lo is zero.
	ZeroPendingBalanceLoProof *zkproofs.ZeroBalanceProof `json:"zero_pending_balance_lo_proof"`

	// Proof that the current pending balance hi is zero.
	ZeroPendingBalanceHiProof *zkproofs.ZeroBalanceProof `json:"zero_pending_balance_hi_proof"`
}
