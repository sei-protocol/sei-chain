package types

import (
	"github.com/coinbase/kryptology/pkg/core/curves"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
)

func TestMsgTransfer_FromProto(t *testing.T) {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	sourcePrivateKey, _ := elgamal.GenerateKey()
	destPrivateKey, _ := elgamal.GenerateKey()
	auditorPrivateKey, _ := elgamal.GenerateKey()
	eg := elgamal.NewTwistedElgamal()
	sourceKeypair, _ := eg.KeyGen(*sourcePrivateKey, testDenom)
	destinationKeypair, _ := eg.KeyGen(*destPrivateKey, testDenom)
	auditorKeypair, _ := eg.KeyGen(*auditorPrivateKey, testDenom)
	aesPK, err := encryption.GetAESKey(*sourcePrivateKey, testDenom)
	require.NoError(t, err)

	amountLo := uint64(100)
	amountHi := uint64(0)

	remainingBalance := uint64(200)

	decryptableBalance, err := encryption.EncryptAESGCM(remainingBalance, aesPK)

	// Encrypt the amount using source and destination public keys
	sourceCiphertextAmountLo, sourceCiphertextAmountLoR, _ := eg.Encrypt(sourceKeypair.PublicKey, amountLo)
	sourceCiphertextAmountLoValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&sourceCiphertextAmountLoR, sourceKeypair.PublicKey, sourceCiphertextAmountLo, amountLo)
	sourceCiphertextAmountHi, sourceCiphertextAmountHiR, _ := eg.Encrypt(sourceKeypair.PublicKey, amountHi)
	sourceCiphertextAmountHiValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&sourceCiphertextAmountHiR, sourceKeypair.PublicKey, sourceCiphertextAmountHi, amountHi)

	ciphertext := &Ciphertext{}
	fromAmountLo := ciphertext.ToProto(sourceCiphertextAmountLo)
	fromAmountHi := ciphertext.ToProto(sourceCiphertextAmountHi)

	destinationCipherAmountLo, destinationCipherAmountLoR, _ := eg.Encrypt(destinationKeypair.PublicKey, amountLo)
	destinationCipherAmountLoValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&destinationCipherAmountLoR, destinationKeypair.PublicKey, destinationCipherAmountLo, amountLo)
	destinationCipherAmountHi, destinationCipherAmountHiR, _ := eg.Encrypt(destinationKeypair.PublicKey, amountHi)
	destinationCipherAmountHiValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&destinationCipherAmountHiR, destinationKeypair.PublicKey, destinationCipherAmountHi, amountHi)

	destinationAmountLo := ciphertext.ToProto(destinationCipherAmountLo)
	destinationAmountHi := ciphertext.ToProto(destinationCipherAmountHi)

	remainingBalanceCiphertext, remainingBalanceRandomness, _ := eg.Encrypt(sourceKeypair.PublicKey, remainingBalance)
	remainingBalanceProto := ciphertext.ToProto(remainingBalanceCiphertext)

	remainingBalanceCommitmentValidityProof, _ := zkproofs.NewCiphertextValidityProof(&remainingBalanceRandomness, sourceKeypair.PublicKey, remainingBalanceCiphertext, remainingBalance)

	remainingBalanceRangeProof, _ := zkproofs.NewRangeProof(64, int(remainingBalance), remainingBalanceRandomness)

	ed25519Curve := curves.ED25519()

	scalarAmtValue := new(big.Int).SetUint64(remainingBalance)
	scalarAmount, _ := ed25519Curve.Scalar.SetBigInt(scalarAmtValue)
	remainingBalanceEqualityProof, _ := zkproofs.NewCiphertextCommitmentEqualityProof(
		sourceKeypair, remainingBalanceCiphertext, &remainingBalanceRandomness, &scalarAmount)

	scalarAmountValueLo := new(big.Int).SetUint64(amountLo)
	scalarAmountLo, _ := ed25519Curve.Scalar.SetBigInt(scalarAmountValueLo)

	transferAmountLoEqualityProof, _ := zkproofs.NewCiphertextCiphertextEqualityProof(
		sourceKeypair,
		&destinationKeypair.PublicKey,
		sourceCiphertextAmountLo,
		&destinationCipherAmountLoR,
		&scalarAmountLo)

	scalarAmountValueHi := new(big.Int).SetUint64(amountHi)
	scalarAmountHi, _ := ed25519Curve.Scalar.SetBigInt(scalarAmountValueHi)

	transferAmountHiEqualityProof, _ := zkproofs.NewCiphertextCiphertextEqualityProof(
		sourceKeypair,
		&destinationKeypair.PublicKey,
		sourceCiphertextAmountHi,
		&destinationCipherAmountHiR,
		&scalarAmountHi)

	proofs := &Proofs{
		RemainingBalanceCommitmentValidityProof: remainingBalanceCommitmentValidityProof,
		SenderTransferAmountLoValidityProof:     sourceCiphertextAmountLoValidityProof,
		SenderTransferAmountHiValidityProof:     sourceCiphertextAmountHiValidityProof,
		RecipientTransferAmountLoValidityProof:  destinationCipherAmountLoValidityProof,
		RecipientTransferAmountHiValidityProof:  destinationCipherAmountHiValidityProof,
		RemainingBalanceRangeProof:              remainingBalanceRangeProof,
		RemainingBalanceEqualityProof:           remainingBalanceEqualityProof,
		TransferAmountLoEqualityProof:           transferAmountLoEqualityProof,
		TransferAmountHiEqualityProof:           transferAmountHiEqualityProof,
	}

	transferProofs := &TransferProofs{}
	proofsProto := transferProofs.ToProto(proofs)
	address1 := sdk.AccAddress("address1")
	address2 := sdk.AccAddress("address2")
	auditorAddress := sdk.AccAddress("auditor")

	// Auditor data
	auditorCipherAmountLo, auditorCipherAmountLoR, _ := eg.Encrypt(auditorKeypair.PublicKey, amountLo)
	auditorCipherAmountLoValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&auditorCipherAmountLoR, auditorKeypair.PublicKey, auditorCipherAmountLo, amountLo)
	auditorCipherAmountHi, auditorCipherAmountHiR, _ := eg.Encrypt(auditorKeypair.PublicKey, amountHi)
	auditorCipherAmountHiValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&auditorCipherAmountHiR, auditorKeypair.PublicKey, auditorCipherAmountHi, amountHi)

	auditorTransferAmountLoEqualityProof, _ := zkproofs.NewCiphertextCiphertextEqualityProof(
		sourceKeypair,
		&auditorKeypair.PublicKey,
		sourceCiphertextAmountLo,
		&auditorCipherAmountLoR,
		&scalarAmountLo)

	auditorTransferAmountHiEqualityProof, _ := zkproofs.NewCiphertextCiphertextEqualityProof(
		sourceKeypair,
		&auditorKeypair.PublicKey,
		sourceCiphertextAmountHi,
		&auditorCipherAmountHiR,
		&scalarAmountHi)

	auditor := Auditor{}
	transferAuditor := &TransferAuditor{
		Address:                       auditorAddress.String(),
		EncryptedTransferAmountLo:     auditorCipherAmountLo,
		EncryptedTransferAmountHi:     auditorCipherAmountHi,
		TransferAmountLoValidityProof: auditorCipherAmountLoValidityProof,
		TransferAmountHiValidityProof: auditorCipherAmountHiValidityProof,
		TransferAmountLoEqualityProof: auditorTransferAmountLoEqualityProof,
		TransferAmountHiEqualityProof: auditorTransferAmountHiEqualityProof,
	}
	auditorProto := auditor.ToProto(transferAuditor)

	m := &MsgTransfer{
		FromAddress:        address1.String(),
		ToAddress:          address2.String(),
		Denom:              testDenom,
		FromAmountLo:       fromAmountLo,
		FromAmountHi:       fromAmountHi,
		ToAmountLo:         destinationAmountLo,
		ToAmountHi:         destinationAmountHi,
		RemainingBalance:   remainingBalanceProto,
		DecryptableBalance: decryptableBalance,
		Proofs:             proofsProto,
		Auditors:           []*Auditor{auditorProto},
	}

	assert.NoError(t, m.ValidateBasic())

	result, err := m.FromProto()

	assert.NoError(t, err)
	assert.Equal(t, m.ToAddress, result.ToAddress)
	assert.Equal(t, m.FromAddress, result.FromAddress)
	assert.Equal(t, m.Denom, result.Denom)
	assert.Equal(t, m.DecryptableBalance, result.DecryptableBalance)
	assert.True(t, sourceCiphertextAmountLo.C.Equal(result.SenderTransferAmountLo.C))
	assert.True(t, sourceCiphertextAmountLo.D.Equal(result.SenderTransferAmountLo.D))
	assert.True(t, sourceCiphertextAmountHi.C.Equal(result.SenderTransferAmountHi.C))
	assert.True(t, sourceCiphertextAmountHi.D.Equal(result.SenderTransferAmountHi.D))
	assert.True(t, destinationCipherAmountLo.C.Equal(result.RecipientTransferAmountLo.C))
	assert.True(t, destinationCipherAmountLo.D.Equal(result.RecipientTransferAmountLo.D))
	assert.True(t, destinationCipherAmountHi.C.Equal(result.RecipientTransferAmountHi.C))
	assert.True(t, destinationCipherAmountHi.D.Equal(result.RecipientTransferAmountHi.D))
	assert.True(t, remainingBalanceCiphertext.C.Equal(result.RemainingBalanceCommitment.C))
	assert.True(t, remainingBalanceCiphertext.D.Equal(result.RemainingBalanceCommitment.D))
	assert.Equal(t, transferAuditor.Address, result.Auditors[0].Address)
	assert.True(t, transferAuditor.EncryptedTransferAmountLo.C.Equal(result.Auditors[0].EncryptedTransferAmountLo.C))
	assert.True(t, transferAuditor.EncryptedTransferAmountLo.D.Equal(result.Auditors[0].EncryptedTransferAmountLo.D))
	assert.True(t, transferAuditor.EncryptedTransferAmountHi.C.Equal(result.Auditors[0].EncryptedTransferAmountHi.C))
	assert.True(t, transferAuditor.EncryptedTransferAmountHi.D.Equal(result.Auditors[0].EncryptedTransferAmountHi.D))

	decryptedRemainingBalance, err := encryption.DecryptAESGCM(result.DecryptableBalance, aesPK)
	assert.NoError(t, err)

	assert.Equal(t, remainingBalance, decryptedRemainingBalance)

	// Make sure the proofs are valid
	assert.True(t, zkproofs.VerifyCiphertextValidity(
		result.Proofs.SenderTransferAmountLoValidityProof,
		sourceKeypair.PublicKey,
		result.SenderTransferAmountLo))

	assert.True(t, zkproofs.VerifyCiphertextValidity(
		result.Proofs.SenderTransferAmountHiValidityProof,
		sourceKeypair.PublicKey,
		result.SenderTransferAmountHi))

	assert.True(t, zkproofs.VerifyCiphertextValidity(
		result.Proofs.RecipientTransferAmountLoValidityProof,
		destinationKeypair.PublicKey,
		result.RecipientTransferAmountLo))

	assert.True(t, zkproofs.VerifyCiphertextValidity(
		result.Proofs.RecipientTransferAmountHiValidityProof,
		destinationKeypair.PublicKey,
		result.RecipientTransferAmountHi))

	valid, err := zkproofs.VerifyRangeProof(
		result.Proofs.RemainingBalanceRangeProof,
		result.RemainingBalanceCommitment, 64)

	assert.NoError(t, err)
	assert.True(t, valid)

	assert.True(t, zkproofs.VerifyCiphertextCommitmentEquality(
		result.Proofs.RemainingBalanceEqualityProof,
		&sourceKeypair.PublicKey,
		remainingBalanceCiphertext,
		&remainingBalanceCiphertext.C))

	assert.True(t, zkproofs.VerifyCiphertextCiphertextEquality(
		result.Proofs.TransferAmountLoEqualityProof,
		&sourceKeypair.PublicKey,
		&destinationKeypair.PublicKey,
		result.SenderTransferAmountLo,
		result.RecipientTransferAmountLo))

	assert.True(t, zkproofs.VerifyCiphertextCiphertextEquality(
		result.Proofs.TransferAmountHiEqualityProof,
		&sourceKeypair.PublicKey,
		&destinationKeypair.PublicKey,
		result.SenderTransferAmountHi,
		result.RecipientTransferAmountHi))

	assert.True(t, zkproofs.VerifyCiphertextValidity(
		result.Auditors[0].TransferAmountLoValidityProof,
		auditorKeypair.PublicKey,
		result.Auditors[0].EncryptedTransferAmountLo))

	assert.True(t, zkproofs.VerifyCiphertextValidity(
		result.Auditors[0].TransferAmountHiValidityProof,
		auditorKeypair.PublicKey,
		result.Auditors[0].EncryptedTransferAmountHi))

	assert.True(t, zkproofs.VerifyCiphertextCiphertextEquality(
		result.Auditors[0].TransferAmountLoEqualityProof,
		&sourceKeypair.PublicKey,
		&auditorKeypair.PublicKey,
		result.SenderTransferAmountLo,
		result.Auditors[0].EncryptedTransferAmountLo))

	assert.True(t, zkproofs.VerifyCiphertextCiphertextEquality(
		result.Auditors[0].TransferAmountHiEqualityProof,
		&sourceKeypair.PublicKey,
		&auditorKeypair.PublicKey,
		result.SenderTransferAmountLo,
		result.Auditors[0].EncryptedTransferAmountLo))
}
