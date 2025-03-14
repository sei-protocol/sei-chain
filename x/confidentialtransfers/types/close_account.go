package types

import (
	"crypto/ecdsa"

	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
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

func NewCloseAccount(privateKey ecdsa.PrivateKey, address, denom string, currPendingBalanceLo, currPendingBalanceHi, currAvailableBalance *elgamal.Ciphertext) (*CloseAccount, error) {
	keyPair, err := utils.GetElGamalKeyPair(privateKey, denom)
	if err != nil {
		return nil, err
	}
	zeroPendingBalanceLoProof, err := zkproofs.NewZeroBalanceProof(keyPair, currPendingBalanceLo)
	if err != nil {
		return nil, err
	}

	zeroPendingBalanceHiProof, err := zkproofs.NewZeroBalanceProof(keyPair, currPendingBalanceHi)
	if err != nil {
		return nil, err
	}

	zeroAvailableBalanceProof, err := zkproofs.NewZeroBalanceProof(keyPair, currAvailableBalance)
	if err != nil {
		return nil, err
	}

	return &CloseAccount{
		Address: address,
		Denom:   denom,
		Proofs: &CloseAccountProofs{
			ZeroAvailableBalanceProof: zeroAvailableBalanceProof,
			ZeroPendingBalanceLoProof: zeroPendingBalanceLoProof,
			ZeroPendingBalanceHiProof: zeroPendingBalanceHiProof,
		},
	}, nil
}
