package types

import (
	"crypto/ecdsa"
	crand "crypto/rand"
	"math/big"
	"testing"

	"github.com/coinbase/kryptology/pkg/core/curves"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMsgTransfer_FromProto(t *testing.T) {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	sourcePrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	destPrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	auditorPrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	eg := elgamal.NewTwistedElgamal()
	sourceKeypair, _ := utils.GetElGamalKeyPair(*sourcePrivateKey, testDenom)
	destinationKeypair, _ := utils.GetElGamalKeyPair(*destPrivateKey, testDenom)
	auditorKeypair, _ := utils.GetElGamalKeyPair(*auditorPrivateKey, testDenom)
	aesPK, err := utils.GetAESKey(*sourcePrivateKey, testDenom)
	require.NoError(t, err)

	amountLo := big.NewInt(100)
	amountHi := big.NewInt(0)

	remainingBalance := big.NewInt(200)

	decryptableBalance, err := encryption.EncryptAESGCM(remainingBalance, aesPK)

	// Encrypt the amount using source and destination public keys
	sourceCiphertextAmountLo, sourceCiphertextAmountLoR, _ := eg.Encrypt(sourceKeypair.PublicKey, amountLo)
	sourceCiphertextAmountLoValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&sourceCiphertextAmountLoR, sourceKeypair.PublicKey, sourceCiphertextAmountLo, amountLo)
	sourceCiphertextAmountHi, sourceCiphertextAmountHiR, _ := eg.Encrypt(sourceKeypair.PublicKey, amountHi)
	sourceCiphertextAmountHiValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&sourceCiphertextAmountHiR, sourceKeypair.PublicKey, sourceCiphertextAmountHi, amountHi)

	fromAmountLo := NewCiphertextProto(sourceCiphertextAmountLo)
	fromAmountHi := NewCiphertextProto(sourceCiphertextAmountHi)

	destinationCipherAmountLo, destinationCipherAmountLoR, _ := eg.Encrypt(destinationKeypair.PublicKey, amountLo)
	destinationCipherAmountLoValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&destinationCipherAmountLoR, destinationKeypair.PublicKey, destinationCipherAmountLo, amountLo)
	destinationCipherAmountHi, destinationCipherAmountHiR, _ := eg.Encrypt(destinationKeypair.PublicKey, amountHi)
	destinationCipherAmountHiValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&destinationCipherAmountHiR, destinationKeypair.PublicKey, destinationCipherAmountHi, amountHi)

	destinationAmountLo := NewCiphertextProto(destinationCipherAmountLo)
	destinationAmountHi := NewCiphertextProto(destinationCipherAmountHi)

	remainingBalanceCiphertext, remainingBalanceRandomness, _ := eg.Encrypt(sourceKeypair.PublicKey, remainingBalance)
	remainingBalanceProto := NewCiphertextProto(remainingBalanceCiphertext)

	remainingBalanceCommitmentValidityProof, _ := zkproofs.NewCiphertextValidityProof(&remainingBalanceRandomness, sourceKeypair.PublicKey, remainingBalanceCiphertext, remainingBalance)

	remainingBalanceRangeProof, _ := zkproofs.NewRangeProof(128, remainingBalance, remainingBalanceRandomness)

	ed25519Curve := curves.ED25519()

	scalarAmount, _ := ed25519Curve.Scalar.SetBigInt(remainingBalance)
	remainingBalanceEqualityProof, _ := zkproofs.NewCiphertextCommitmentEqualityProof(
		sourceKeypair, remainingBalanceCiphertext, &remainingBalanceRandomness, &scalarAmount)

	scalarAmountLo, _ := ed25519Curve.Scalar.SetBigInt(amountLo)

	transferAmountLoEqualityProof, _ := zkproofs.NewCiphertextCiphertextEqualityProof(
		sourceKeypair,
		&destinationKeypair.PublicKey,
		sourceCiphertextAmountLo,
		&destinationCipherAmountLoR,
		&scalarAmountLo)

	scalarAmountHi, _ := ed25519Curve.Scalar.SetBigInt(amountHi)

	transferAmountHiEqualityProof, _ := zkproofs.NewCiphertextCiphertextEqualityProof(
		sourceKeypair,
		&destinationKeypair.PublicKey,
		sourceCiphertextAmountHi,
		&destinationCipherAmountHiR,
		&scalarAmountHi)

	transferAmountLoRangeProof, _ := zkproofs.NewRangeProof(16, amountLo, sourceCiphertextAmountLoR)
	transferAmountHiRangeProof, _ := zkproofs.NewRangeProof(32, amountHi, sourceCiphertextAmountHiR)
	proofs := &TransferProofs{
		RemainingBalanceCommitmentValidityProof: remainingBalanceCommitmentValidityProof,
		SenderTransferAmountLoValidityProof:     sourceCiphertextAmountLoValidityProof,
		SenderTransferAmountHiValidityProof:     sourceCiphertextAmountHiValidityProof,
		TransferAmountLoRangeProof:              transferAmountLoRangeProof,
		TransferAmountHiRangeProof:              transferAmountHiRangeProof,
		RecipientTransferAmountLoValidityProof:  destinationCipherAmountLoValidityProof,
		RecipientTransferAmountHiValidityProof:  destinationCipherAmountHiValidityProof,
		RemainingBalanceRangeProof:              remainingBalanceRangeProof,
		RemainingBalanceEqualityProof:           remainingBalanceEqualityProof,
		TransferAmountLoEqualityProof:           transferAmountLoEqualityProof,
		TransferAmountHiEqualityProof:           transferAmountHiEqualityProof,
	}

	proofsProto := NewTransferMsgProofs(proofs)
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

	transferAuditor := &TransferAuditor{
		Address:                       auditorAddress.String(),
		EncryptedTransferAmountLo:     auditorCipherAmountLo,
		EncryptedTransferAmountHi:     auditorCipherAmountHi,
		TransferAmountLoValidityProof: auditorCipherAmountLoValidityProof,
		TransferAmountHiValidityProof: auditorCipherAmountHiValidityProof,
		TransferAmountLoEqualityProof: auditorTransferAmountLoEqualityProof,
		TransferAmountHiEqualityProof: auditorTransferAmountHiEqualityProof,
	}
	auditorProto := NewAuditorProto(transferAuditor)

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

	marshalled, err := m.Marshal()
	require.NoError(t, err)

	// Reset the message
	m = &MsgTransfer{}
	err = m.Unmarshal(marshalled)
	require.NoError(t, err)

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
	ed25519RangeVerifierFactory := zkproofs.Ed25519RangeVerifierFactory{}
	rangeVerifierFactory := zkproofs.NewCachedRangeVerifierFactory(&ed25519RangeVerifierFactory)
	valid, err := zkproofs.VerifyRangeProof(
		result.Proofs.RemainingBalanceRangeProof,
		result.RemainingBalanceCommitment,
		128,
		rangeVerifierFactory)

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
		result.SenderTransferAmountHi,
		result.Auditors[0].EncryptedTransferAmountHi))

}

func TestMsgTransfer_ValidateBasic(t *testing.T) {
	validAddress := sdk.AccAddress("address1").String()
	invalidAddress := "invalid_address"
	validDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"

	tests := []struct {
		name    string
		msg     MsgTransfer
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid from address",
			msg: MsgTransfer{
				FromAddress: invalidAddress,
				ToAddress:   validAddress,
				Denom:       validDenom,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidAddress.Error(),
		},
		{
			name: "invalid to address",
			msg: MsgTransfer{
				FromAddress: validAddress,
				ToAddress:   invalidAddress,
				Denom:       validDenom,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidAddress.Error(),
		},
		{
			name: "invalid denom",
			msg: MsgTransfer{
				FromAddress: validAddress,
				ToAddress:   validAddress,
				Denom:       "",
			},
			wantErr: true,
			errMsg:  "invalid denom",
		},
		{
			name: "missing from amount lo",
			msg: MsgTransfer{
				FromAddress: validAddress,
				ToAddress:   validAddress,
				Denom:       validDenom,
			},
			wantErr: true,
			errMsg:  "from amount lo is required",
		},
		{
			name: "missing from amount hi",
			msg: MsgTransfer{
				FromAddress:  validAddress,
				ToAddress:    validAddress,
				Denom:        validDenom,
				FromAmountLo: &Ciphertext{},
			},
			wantErr: true,
			errMsg:  "from amount hi is required",
		},
		{
			name: "missing to amount lo",
			msg: MsgTransfer{
				FromAddress:  validAddress,
				ToAddress:    validAddress,
				Denom:        validDenom,
				FromAmountLo: &Ciphertext{},
				FromAmountHi: &Ciphertext{},
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "missing to amount hi",
			msg: MsgTransfer{
				FromAddress:  validAddress,
				ToAddress:    validAddress,
				Denom:        validDenom,
				FromAmountLo: &Ciphertext{},
				FromAmountHi: &Ciphertext{},
				ToAmountLo:   &Ciphertext{},
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "missing remaining balance",
			msg: MsgTransfer{
				FromAddress:  validAddress,
				ToAddress:    validAddress,
				Denom:        validDenom,
				FromAmountLo: &Ciphertext{},
				FromAmountHi: &Ciphertext{},
				ToAmountLo:   &Ciphertext{},
				ToAmountHi:   &Ciphertext{},
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "missing proofs",
			msg: MsgTransfer{
				FromAddress:      validAddress,
				ToAddress:        validAddress,
				Denom:            validDenom,
				FromAmountLo:     &Ciphertext{},
				FromAmountHi:     &Ciphertext{},
				ToAmountLo:       &Ciphertext{},
				ToAmountHi:       &Ciphertext{},
				RemainingBalance: &Ciphertext{},
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "missing proofs",
			msg: MsgTransfer{
				FromAddress:        validAddress,
				ToAddress:          validAddress,
				Denom:              validDenom,
				FromAmountLo:       &Ciphertext{},
				FromAmountHi:       &Ciphertext{},
				ToAmountLo:         &Ciphertext{},
				ToAmountHi:         &Ciphertext{},
				RemainingBalance:   &Ciphertext{},
				DecryptableBalance: "decryptable_balance",
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgInitializeAccount_FromProto(t *testing.T) {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	sourcePrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	eg := elgamal.NewTwistedElgamal()
	sourceKeypair, _ := utils.GetElGamalKeyPair(*sourcePrivateKey, testDenom)
	aesPK, err := utils.GetAESKey(*sourcePrivateKey, testDenom)
	require.NoError(t, err)
	bigIntZero := big.NewInt(0)

	decryptableBalance, err := encryption.EncryptAESGCM(bigIntZero, aesPK)
	encryptedZero, _, err := eg.Encrypt(sourceKeypair.PublicKey, bigIntZero)

	// Generate the proof
	pubkeyValidityProof, _ := zkproofs.NewPubKeyValidityProof(
		sourceKeypair.PublicKey,
		sourceKeypair.PrivateKey)

	zeroBalProof, _ := zkproofs.NewZeroBalanceProof(
		sourceKeypair,
		encryptedZero)

	proofs := &InitializeAccountProofs{
		pubkeyValidityProof,
		zeroBalProof,
		zeroBalProof,
		zeroBalProof,
	}
	address1 := sdk.AccAddress("address1")

	encryptedZeroProto := NewCiphertextProto(encryptedZero)
	proofsProto := NewInitializeAccountMsgProofs(proofs)
	m := &MsgInitializeAccount{
		FromAddress:        address1.String(),
		Denom:              testDenom,
		PublicKey:          sourceKeypair.PublicKey.ToAffineCompressed(),
		DecryptableBalance: decryptableBalance,
		PendingBalanceLo:   encryptedZeroProto,
		PendingBalanceHi:   encryptedZeroProto,
		AvailableBalance:   encryptedZeroProto,
		Proofs:             proofsProto,
	}

	assert.NoError(t, m.ValidateBasic())
	marshalled, err := m.Marshal()
	require.NoError(t, err)

	// Reset the message
	m = &MsgInitializeAccount{}
	err = m.Unmarshal(marshalled)
	require.NoError(t, err)

	assert.NoError(t, m.ValidateBasic())

	result, err := m.FromProto()

	assert.NoError(t, err)

	assert.Equal(t, m.FromAddress, result.FromAddress)
	assert.Equal(t, m.Denom, result.Denom)
	assert.Equal(t, m.DecryptableBalance, result.DecryptableBalance)
	assert.True(t, sourceKeypair.PublicKey.Equal(*result.Pubkey))

	decryptedRemainingBalance, err := encryption.DecryptAESGCM(result.DecryptableBalance, aesPK)
	assert.NoError(t, err)

	assert.Equal(t, new(big.Int).SetUint64(0), decryptedRemainingBalance)

	// Make sure the proofs are valid
	assert.True(t, zkproofs.VerifyPubKeyValidity(
		*result.Pubkey,
		result.Proofs.PubkeyValidityProof))
}

func TestMsgInitializeAccount_ValidateBasic(t *testing.T) {
	validAddress := sdk.AccAddress("address1").String()
	invalidAddress := "invalid_address"
	validDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"

	tests := []struct {
		name    string
		msg     MsgInitializeAccount
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid from address",
			msg: MsgInitializeAccount{
				FromAddress: invalidAddress,
				Denom:       validDenom,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidAddress.Error(),
		},
		{
			name: "invalid denom",
			msg: MsgInitializeAccount{
				FromAddress: validAddress,
				Denom:       "",
			},
			wantErr: true,
			errMsg:  "invalid denom",
		},
		{
			name: "missing pubkey",
			msg: MsgInitializeAccount{
				FromAddress: validAddress,
				Denom:       validDenom,
				PublicKey:   nil,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "missing proofs",
			msg: MsgInitializeAccount{
				FromAddress: validAddress,
				Denom:       validDenom,
				PublicKey:   curves.ED25519().Point.Random(crand.Reader).ToAffineCompressed(),
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgWithdraw_FromProto(t *testing.T) {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	sourcePrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	eg := elgamal.NewTwistedElgamal()
	sourceKeypair, _ := utils.GetElGamalKeyPair(*sourcePrivateKey, testDenom)
	aesPK, err := utils.GetAESKey(*sourcePrivateKey, testDenom)
	require.NoError(t, err)

	currentBalance := big.NewInt(500000000)
	currentBalanceCt, _, _ := eg.Encrypt(sourceKeypair.PublicKey, currentBalance)
	withdrawAmount := big.NewInt(10000)
	withdrawAmountCt, _ := eg.SubScalar(currentBalanceCt, withdrawAmount)
	newBalance := new(big.Int).Sub(currentBalance, withdrawAmount)
	newBalanceScalar, _ := curves.ED25519().Scalar.SetBigInt(newBalance)
	decryptableBalance, err := encryption.EncryptAESGCM(newBalance, aesPK)
	newBalanceCommitment, randomness, err := eg.Encrypt(sourceKeypair.PublicKey, newBalance)
	require.NoError(t, err)

	// Generate the proofs
	rangeProof, _ := zkproofs.NewRangeProof(128, newBalance, randomness)
	ciphertextCommitmentEqualityProof, _ := zkproofs.NewCiphertextCommitmentEqualityProof(
		sourceKeypair,
		withdrawAmountCt,
		&randomness,
		&newBalanceScalar)

	proofs := &WithdrawProofs{
		rangeProof,
		ciphertextCommitmentEqualityProof,
	}
	address1 := sdk.AccAddress("address1")

	newBalanceProto := NewCiphertextProto(newBalanceCommitment)

	proofsProto := NewWithdrawMsgProofs(proofs)
	m := &MsgWithdraw{
		FromAddress:                address1.String(),
		Denom:                      testDenom,
		Amount:                     withdrawAmount.String(),
		RemainingBalanceCommitment: newBalanceProto,
		DecryptableBalance:         decryptableBalance,
		Proofs:                     proofsProto,
	}

	assert.NoError(t, m.ValidateBasic())

	marshalled, err := m.Marshal()
	require.NoError(t, err)

	// Reset the message
	m = &MsgWithdraw{}
	err = m.Unmarshal(marshalled)
	require.NoError(t, err)

	assert.NoError(t, m.ValidateBasic())

	result, err := m.FromProto()

	assert.NoError(t, err)
	assert.Equal(t, m.FromAddress, result.FromAddress)
	assert.Equal(t, m.Denom, result.Denom)
	assert.Equal(t, m.Amount, result.Amount.String())
	assert.Equal(t, m.DecryptableBalance, result.DecryptableBalance)
	assert.True(t, newBalanceCommitment.C.Equal(result.RemainingBalanceCommitment.C))
	assert.True(t, newBalanceCommitment.D.Equal(result.RemainingBalanceCommitment.D))

	decryptedRemainingBalance, err := encryption.DecryptAESGCM(result.DecryptableBalance, aesPK)
	assert.NoError(t, err)
	assert.Equal(t, newBalance, decryptedRemainingBalance)

	decryptedCommitment, err := eg.DecryptLargeNumber(sourceKeypair.PrivateKey, result.RemainingBalanceCommitment, 32)
	assert.NoError(t, err)
	assert.Equal(t, newBalance, decryptedCommitment)

	// Make sure the proofs are valid
	ed25519RangeVerifierFactory := zkproofs.Ed25519RangeVerifierFactory{}
	rangeVerifierFactory := zkproofs.NewCachedRangeVerifierFactory(&ed25519RangeVerifierFactory)
	verified, err := zkproofs.VerifyRangeProof(result.Proofs.RemainingBalanceRangeProof, result.RemainingBalanceCommitment, 128, rangeVerifierFactory)
	assert.NoError(t, err)
	assert.True(t, verified)

	assert.True(t, zkproofs.VerifyCiphertextCommitmentEquality(
		result.Proofs.RemainingBalanceEqualityProof,
		&sourceKeypair.PublicKey,
		withdrawAmountCt,
		&result.RemainingBalanceCommitment.C))
}

func TestMsgWithdraw_ValidateBasic(t *testing.T) {
	validAddress := sdk.AccAddress("address1").String()
	invalidAddress := "invalid_address"
	validDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"

	tests := []struct {
		name    string
		msg     MsgWithdraw
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid from address",
			msg: MsgWithdraw{
				FromAddress: invalidAddress,
				Denom:       validDenom,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidAddress.Error(),
		},
		{
			name: "invalid denom",
			msg: MsgWithdraw{
				FromAddress: validAddress,
				Denom:       "",
			},
			wantErr: true,
			errMsg:  "invalid denom",
		},
		{
			name: "missing remaining balance commitment",
			msg: MsgWithdraw{
				FromAddress:                validAddress,
				Denom:                      validDenom,
				RemainingBalanceCommitment: nil,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "missing amount",
			msg: MsgWithdraw{
				FromAddress:                validAddress,
				Denom:                      validDenom,
				RemainingBalanceCommitment: &Ciphertext{},
				Amount:                     "0",
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "missing decryptable balance",
			msg: MsgWithdraw{
				FromAddress:                validAddress,
				Denom:                      validDenom,
				RemainingBalanceCommitment: &Ciphertext{},
				Amount:                     "100",
				DecryptableBalance:         "",
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "missing proofs",
			msg: MsgWithdraw{
				FromAddress:                validAddress,
				Denom:                      validDenom,
				RemainingBalanceCommitment: &Ciphertext{},
				Amount:                     "100",
				DecryptableBalance:         "notnil",
				Proofs:                     nil,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "happy path",
			msg: MsgWithdraw{
				FromAddress:                validAddress,
				Denom:                      validDenom,
				Amount:                     "100",
				RemainingBalanceCommitment: &Ciphertext{},
				DecryptableBalance:         "notnil",
				Proofs: &WithdrawMsgProofs{
					&RangeProof{},
					&CiphertextCommitmentEqualityProof{},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgCloseAccount_FromProto(t *testing.T) {
	address := sdk.AccAddress("address1")
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	privateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	zeroBigInt := big.NewInt(0)
	eg := elgamal.NewTwistedElgamal()
	keypair, _ := utils.GetElGamalKeyPair(*privateKey, testDenom)
	availableBalanceCiphertext, _, _ := eg.Encrypt(keypair.PublicKey, zeroBigInt)
	pendingBalanceLoCiphertext, _, _ := eg.Encrypt(keypair.PublicKey, zeroBigInt)
	pendingBalanceHiCiphertext, _, _ := eg.Encrypt(keypair.PublicKey, zeroBigInt)

	availableBalanceProof, err := zkproofs.NewZeroBalanceProof(keypair, availableBalanceCiphertext)
	require.NoError(t, err)
	pendingBalanceProofLo, err := zkproofs.NewZeroBalanceProof(keypair, pendingBalanceLoCiphertext)
	require.NoError(t, err)
	pendingBalanceProofHi, err := zkproofs.NewZeroBalanceProof(keypair, pendingBalanceHiCiphertext)
	require.NoError(t, err)

	closeAccountProofs := &CloseAccountProofs{
		ZeroAvailableBalanceProof: availableBalanceProof,
		ZeroPendingBalanceLoProof: pendingBalanceProofLo,
		ZeroPendingBalanceHiProof: pendingBalanceProofHi,
	}

	proof := NewCloseAccountMsgProofs(closeAccountProofs)

	m := &MsgCloseAccount{
		Address: address.String(),
		Denom:   testDenom,
		Proofs:  proof,
	}

	marshalled, err := m.Marshal()
	require.NoError(t, err)

	result := &MsgCloseAccount{}
	err = result.Unmarshal(marshalled)
	require.NoError(t, err)

	assert.Equal(t, m.Address, result.Address)
	assert.Equal(t, m.Denom, result.Denom)
	resultProof, err := result.Proofs.FromProto()
	require.NoError(t, err)

	assert.NoError(t, result.ValidateBasic())

	assert.True(t, closeAccountProofs.ZeroAvailableBalanceProof.Yd.Equal(resultProof.ZeroAvailableBalanceProof.Yd))
	assert.True(t, closeAccountProofs.ZeroAvailableBalanceProof.Yp.Equal(resultProof.ZeroAvailableBalanceProof.Yp))
	assert.Equal(t, closeAccountProofs.ZeroAvailableBalanceProof.Z, resultProof.ZeroAvailableBalanceProof.Z)

	assert.True(t, closeAccountProofs.ZeroPendingBalanceLoProof.Yd.Equal(resultProof.ZeroPendingBalanceLoProof.Yd))
	assert.True(t, closeAccountProofs.ZeroPendingBalanceLoProof.Yp.Equal(resultProof.ZeroPendingBalanceLoProof.Yp))
	assert.Equal(t, closeAccountProofs.ZeroPendingBalanceLoProof.Z, resultProof.ZeroPendingBalanceLoProof.Z)

	assert.True(t, closeAccountProofs.ZeroPendingBalanceHiProof.Yd.Equal(resultProof.ZeroPendingBalanceHiProof.Yd))
	assert.True(t, closeAccountProofs.ZeroPendingBalanceHiProof.Yp.Equal(resultProof.ZeroPendingBalanceHiProof.Yp))
	assert.Equal(t, closeAccountProofs.ZeroPendingBalanceHiProof.Z, resultProof.ZeroPendingBalanceHiProof.Z)

	// Make sure the proofs are valid
	assert.True(t, zkproofs.VerifyZeroBalance(
		resultProof.ZeroAvailableBalanceProof, &keypair.PublicKey, availableBalanceCiphertext))

	assert.True(t, zkproofs.VerifyZeroBalance(
		resultProof.ZeroPendingBalanceLoProof, &keypair.PublicKey, pendingBalanceLoCiphertext))

	assert.True(t, zkproofs.VerifyZeroBalance(
		resultProof.ZeroPendingBalanceHiProof, &keypair.PublicKey, pendingBalanceHiCiphertext))
}

func TestMsgCloseAccount_ValidateBasic(t *testing.T) {
	validAddress := sdk.AccAddress("address1").String()
	invalidAddress := "invalid_address"
	validDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"

	tests := []struct {
		name    string
		msg     MsgCloseAccount
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid address",
			msg: MsgCloseAccount{
				Address: invalidAddress,
				Denom:   validDenom,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidAddress.Error(),
		},
		{
			name: "invalid denom",
			msg: MsgCloseAccount{
				Address: validAddress,
				Denom:   "",
			},
			wantErr: true,
			errMsg:  "invalid denom",
		},
		{
			name: "missing proofs",
			msg: MsgCloseAccount{
				Address: validAddress,
				Denom:   validDenom,
				Proofs:  nil,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "happy path",
			msg: MsgCloseAccount{
				Address: validAddress,
				Denom:   validDenom,
				Proofs: &CloseAccountMsgProofs{
					&ZeroBalanceProof{},
					&ZeroBalanceProof{},
					&ZeroBalanceProof{},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgDeposit_ValidateBasic(t *testing.T) {
	validAddress := sdk.AccAddress("address1").String()
	invalidAddress := "invalid_address"
	validDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"

	tests := []struct {
		name    string
		msg     MsgDeposit
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid from address",
			msg: MsgDeposit{
				FromAddress: invalidAddress,
				Denom:       validDenom,
				Amount:      100,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidAddress.Error(),
		},
		{
			name: "invalid denom",
			msg: MsgDeposit{
				FromAddress: validAddress,
				Denom:       "",
				Amount:      100,
			},
			wantErr: true,
			errMsg:  "invalid denom",
		},
		{
			name: "zero amount",
			msg: MsgDeposit{
				FromAddress: validAddress,
				Denom:       validDenom,
				Amount:      0,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "valid message",
			msg: MsgDeposit{
				FromAddress: validAddress,
				Denom:       validDenom,
				Amount:      100,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgApplyPendingBalance_ValidateBasic(t *testing.T) {
	validAddress := sdk.AccAddress("address1").String()
	invalidAddress := "invalid_address"
	validDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"

	tests := []struct {
		name    string
		msg     MsgApplyPendingBalance
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid address",
			msg: MsgApplyPendingBalance{
				Address: invalidAddress,
				Denom:   validDenom,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidAddress.Error(),
		},
		{
			name: "invalid denom",
			msg: MsgApplyPendingBalance{
				Address: validAddress,
				Denom:   "",
			},
			wantErr: true,
			errMsg:  "invalid denom",
		},
		{
			name: "missing new decryptable available balance",
			msg: MsgApplyPendingBalance{
				Address: validAddress,
				Denom:   validDenom,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "missing current available balance",
			msg: MsgApplyPendingBalance{
				Address:                        validAddress,
				Denom:                          validDenom,
				NewDecryptableAvailableBalance: "some_balance",
				CurrentPendingBalanceCounter:   1,
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "missing current pending balance counter",
			msg: MsgApplyPendingBalance{
				Address:                        validAddress,
				Denom:                          validDenom,
				NewDecryptableAvailableBalance: "some_balance",
				CurrentAvailableBalance:        &Ciphertext{},
			},
			wantErr: true,
			errMsg:  sdkerrors.ErrInvalidRequest.Error(),
		},
		{
			name: "valid message",
			msg: MsgApplyPendingBalance{
				Address:                        validAddress,
				Denom:                          validDenom,
				NewDecryptableAvailableBalance: "some_balance",
				CurrentPendingBalanceCounter:   1,
				CurrentAvailableBalance:        &Ciphertext{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgInitializeAccount_ValidateBasic1(t *testing.T) {
	validAddress := sdk.AccAddress{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}.String()
	type fields struct {
		FromAddress        string
		Denom              string
		PublicKey          []byte
		DecryptableBalance string
		PendingBalanceLo   *Ciphertext
		PendingBalanceHi   *Ciphertext
		AvailableBalance   *Ciphertext
		Proofs             *InitializeAccountMsgProofs
	}
	tests := []struct {
		name       string
		fields     fields
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "invalid from address",
			fields: fields{
				FromAddress: "invalid_address",
			},
			wantErr:    true,
			wantErrMsg: "invalid sender address (decoding bech32 failed: invalid separator index -1): invalid address",
		},
		{
			name: "invalid denom",
			fields: fields{
				FromAddress: validAddress,
				Denom:       "",
			},
			wantErr:    true,
			wantErrMsg: "invalid denom: ",
		},
		{
			name: "missing pubkey",
			fields: fields{
				FromAddress: validAddress,
				Denom:       "denom",
			},
			wantErr:    true,
			wantErrMsg: "public key is required: invalid request",
		},
		{
			name: "missing DecryptableBalance",
			fields: fields{
				FromAddress: validAddress,
				Denom:       "denom",
				PublicKey:   []byte{1, 2, 3},
			},
			wantErr:    true,
			wantErrMsg: "decryptable balance is required: invalid request",
		},
		{
			name: "missing PendingBalanceLo",
			fields: fields{
				FromAddress:        validAddress,
				Denom:              "denom",
				PublicKey:          []byte{1, 2, 3},
				DecryptableBalance: "decryptable_balance",
			},
			wantErr:    true,
			wantErrMsg: "pending amount lo is required: invalid request",
		},
		{
			name: "missing PendingBalanceHi",
			fields: fields{
				FromAddress:        validAddress,
				Denom:              "denom",
				PublicKey:          []byte{1, 2, 3},
				DecryptableBalance: "decryptable_balance",
				PendingBalanceLo:   &Ciphertext{},
			},
			wantErr:    true,
			wantErrMsg: "pending amount hi is required: invalid request",
		},
		{
			name: "missing AvailableBalance",
			fields: fields{
				FromAddress:        validAddress,
				Denom:              "denom",
				PublicKey:          []byte{1, 2, 3},
				DecryptableBalance: "decryptable_balance",
				PendingBalanceLo:   &Ciphertext{},
				PendingBalanceHi:   &Ciphertext{},
			},
			wantErr:    true,
			wantErrMsg: "available balance is required: invalid request",
		},
		{
			name: "missing Proofs",
			fields: fields{
				FromAddress:        validAddress,
				Denom:              "denom",
				PublicKey:          []byte{1, 2, 3},
				DecryptableBalance: "decryptable_balance",
				PendingBalanceLo:   &Ciphertext{},
				PendingBalanceHi:   &Ciphertext{},
				AvailableBalance:   &Ciphertext{},
			},
			wantErr:    true,
			wantErrMsg: "proofs is required: invalid request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MsgInitializeAccount{
				FromAddress:        tt.fields.FromAddress,
				Denom:              tt.fields.Denom,
				PublicKey:          tt.fields.PublicKey,
				DecryptableBalance: tt.fields.DecryptableBalance,
				PendingBalanceLo:   tt.fields.PendingBalanceLo,
				PendingBalanceHi:   tt.fields.PendingBalanceHi,
				AvailableBalance:   tt.fields.AvailableBalance,
				Proofs:             tt.fields.Proofs,
			}
			err := m.ValidateBasic()
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.wantErrMsg, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgTransfer_FromProtoInvalidInputs(t *testing.T) {
	type fields struct {
		f func(m *MsgTransfer) *MsgTransfer
	}
	tests := []struct {
		name       string
		fields     fields
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "ValidateBasic fails",
			fields: fields{
				f: func(m *MsgTransfer) *MsgTransfer {
					m = &MsgTransfer{}
					return m
				},
			},
			wantErr:    true,
			wantErrMsg: "invalid sender address (empty address string is not allowed): invalid address",
		},
		{
			name: "invalid FromAmountLo",
			fields: fields{
				f: func(m *MsgTransfer) *MsgTransfer {
					m.FromAmountLo = &Ciphertext{}
					return m
				},
			},
			wantErr:    true,
			wantErrMsg: "edwards25519: invalid point encoding length",
		},
		{
			name: "invalid FromAmountHi",
			fields: fields{
				f: func(m *MsgTransfer) *MsgTransfer {
					m.FromAmountHi = &Ciphertext{}
					return m
				},
			},
			wantErr:    true,
			wantErrMsg: "edwards25519: invalid point encoding length",
		},
		{
			name: "invalid ToAmountLo",
			fields: fields{
				f: func(m *MsgTransfer) *MsgTransfer {
					m.ToAmountLo = &Ciphertext{}
					return m
				},
			},
			wantErr:    true,
			wantErrMsg: "edwards25519: invalid point encoding length",
		},
		{
			name: "invalid ToAmountHi",
			fields: fields{
				f: func(m *MsgTransfer) *MsgTransfer {
					m.ToAmountHi = &Ciphertext{}
					return m
				},
			},
			wantErr:    true,
			wantErrMsg: "edwards25519: invalid point encoding length",
		},
		{
			name: "invalid RemainingBalance",
			fields: fields{
				f: func(m *MsgTransfer) *MsgTransfer {
					m.RemainingBalance = &Ciphertext{}
					return m
				},
			},
			wantErr:    true,
			wantErrMsg: "edwards25519: invalid point encoding length",
		},
		{
			name: "invalid Proofs",
			fields: fields{
				f: func(m *MsgTransfer) *MsgTransfer {
					m.Proofs.RemainingBalanceCommitmentValidityProof = &CiphertextValidityProof{}
					return m
				},
			},
			wantErr:    true,
			wantErrMsg: "ciphertext validity proof is invalid: invalid request",
		},
		{
			name: "invalid Auditor Proofs",
			fields: fields{
				f: func(m *MsgTransfer) *MsgTransfer {
					m.Auditors[0].TransferAmountHiValidityProof = &CiphertextValidityProof{}
					return m
				},
			},
			wantErr:    true,
			wantErrMsg: "ciphertext validity proof is invalid: invalid request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := getValidTransferMsg()
			if tt.fields.f != nil {
				m = tt.fields.f(m)
			}
			got, err := m.FromProto()
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.wantErrMsg, err.Error())
			} else {
				require.NoError(t, err)
				require.NotNil(t, got)
			}
		})
	}
}

func getValidTransferMsg() *MsgTransfer {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	sourcePrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	destPrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	auditorPrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	eg := elgamal.NewTwistedElgamal()
	sourceKeypair, _ := utils.GetElGamalKeyPair(*sourcePrivateKey, testDenom)
	destinationKeypair, _ := utils.GetElGamalKeyPair(*destPrivateKey, testDenom)
	auditorKeypair, _ := utils.GetElGamalKeyPair(*auditorPrivateKey, testDenom)
	aesPK, _ := utils.GetAESKey(*sourcePrivateKey, testDenom)

	amountLo := big.NewInt(100)
	amountHi := big.NewInt(0)

	remainingBalance := big.NewInt(200)

	decryptableBalance, _ := encryption.EncryptAESGCM(remainingBalance, aesPK)

	// Encrypt the amount using source and destination public keys
	sourceCiphertextAmountLo, sourceCiphertextAmountLoR, _ := eg.Encrypt(sourceKeypair.PublicKey, amountLo)
	sourceCiphertextAmountLoValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&sourceCiphertextAmountLoR, sourceKeypair.PublicKey, sourceCiphertextAmountLo, amountLo)
	sourceCiphertextAmountHi, sourceCiphertextAmountHiR, _ := eg.Encrypt(sourceKeypair.PublicKey, amountHi)
	sourceCiphertextAmountHiValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&sourceCiphertextAmountHiR, sourceKeypair.PublicKey, sourceCiphertextAmountHi, amountHi)

	fromAmountLo := NewCiphertextProto(sourceCiphertextAmountLo)
	fromAmountHi := NewCiphertextProto(sourceCiphertextAmountHi)

	destinationCipherAmountLo, destinationCipherAmountLoR, _ := eg.Encrypt(destinationKeypair.PublicKey, amountLo)
	destinationCipherAmountLoValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&destinationCipherAmountLoR, destinationKeypair.PublicKey, destinationCipherAmountLo, amountLo)
	destinationCipherAmountHi, destinationCipherAmountHiR, _ := eg.Encrypt(destinationKeypair.PublicKey, amountHi)
	destinationCipherAmountHiValidityProof, _ :=
		zkproofs.NewCiphertextValidityProof(&destinationCipherAmountHiR, destinationKeypair.PublicKey, destinationCipherAmountHi, amountHi)

	destinationAmountLo := NewCiphertextProto(destinationCipherAmountLo)
	destinationAmountHi := NewCiphertextProto(destinationCipherAmountHi)

	remainingBalanceCiphertext, remainingBalanceRandomness, _ := eg.Encrypt(sourceKeypair.PublicKey, remainingBalance)
	remainingBalanceProto := NewCiphertextProto(remainingBalanceCiphertext)

	remainingBalanceCommitmentValidityProof, _ := zkproofs.NewCiphertextValidityProof(&remainingBalanceRandomness, sourceKeypair.PublicKey, remainingBalanceCiphertext, remainingBalance)

	remainingBalanceRangeProof, _ := zkproofs.NewRangeProof(128, remainingBalance, remainingBalanceRandomness)

	ed25519Curve := curves.ED25519()

	scalarAmount, _ := ed25519Curve.Scalar.SetBigInt(remainingBalance)
	remainingBalanceEqualityProof, _ := zkproofs.NewCiphertextCommitmentEqualityProof(
		sourceKeypair, remainingBalanceCiphertext, &remainingBalanceRandomness, &scalarAmount)

	scalarAmountLo, _ := ed25519Curve.Scalar.SetBigInt(amountLo)

	transferAmountLoEqualityProof, _ := zkproofs.NewCiphertextCiphertextEqualityProof(
		sourceKeypair,
		&destinationKeypair.PublicKey,
		sourceCiphertextAmountLo,
		&destinationCipherAmountLoR,
		&scalarAmountLo)

	scalarAmountHi, _ := ed25519Curve.Scalar.SetBigInt(amountHi)

	transferAmountHiEqualityProof, _ := zkproofs.NewCiphertextCiphertextEqualityProof(
		sourceKeypair,
		&destinationKeypair.PublicKey,
		sourceCiphertextAmountHi,
		&destinationCipherAmountHiR,
		&scalarAmountHi)

	transferAmountLoRangeProof, _ := zkproofs.NewRangeProof(16, amountLo, sourceCiphertextAmountLoR)
	transferAmountHiRangeProof, _ := zkproofs.NewRangeProof(32, amountHi, sourceCiphertextAmountHiR)

	proofs := &TransferProofs{
		RemainingBalanceCommitmentValidityProof: remainingBalanceCommitmentValidityProof,
		SenderTransferAmountLoValidityProof:     sourceCiphertextAmountLoValidityProof,
		SenderTransferAmountHiValidityProof:     sourceCiphertextAmountHiValidityProof,
		TransferAmountLoRangeProof:              transferAmountLoRangeProof,
		TransferAmountHiRangeProof:              transferAmountHiRangeProof,
		RecipientTransferAmountLoValidityProof:  destinationCipherAmountLoValidityProof,
		RecipientTransferAmountHiValidityProof:  destinationCipherAmountHiValidityProof,
		RemainingBalanceRangeProof:              remainingBalanceRangeProof,
		RemainingBalanceEqualityProof:           remainingBalanceEqualityProof,
		TransferAmountLoEqualityProof:           transferAmountLoEqualityProof,
		TransferAmountHiEqualityProof:           transferAmountHiEqualityProof,
	}

	proofsProto := NewTransferMsgProofs(proofs)
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

	transferAuditor := &TransferAuditor{
		Address:                       auditorAddress.String(),
		EncryptedTransferAmountLo:     auditorCipherAmountLo,
		EncryptedTransferAmountHi:     auditorCipherAmountHi,
		TransferAmountLoValidityProof: auditorCipherAmountLoValidityProof,
		TransferAmountHiValidityProof: auditorCipherAmountHiValidityProof,
		TransferAmountLoEqualityProof: auditorTransferAmountLoEqualityProof,
		TransferAmountHiEqualityProof: auditorTransferAmountHiEqualityProof,
	}
	auditorProto := NewAuditorProto(transferAuditor)

	return &MsgTransfer{
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

}

func TestMsgTransfer_Decrypt(t *testing.T) {
	type args struct {
		decryptor               *elgamal.TwistedElGamal
		privKey                 ecdsa.PrivateKey
		decryptAvailableBalance bool
		address                 string
		msg                     *MsgTransfer
	}
	tests := []struct {
		name       string
		args       args
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "nil decryptor",
			args: args{
				decryptor: nil,
				msg:       getValidTransferMsg(),
			},
			wantErr:    true,
			wantErrMsg: "decryptor is required: invalid request",
		},
		{
			name: "invalid transfer message",
			args: args{
				decryptor: elgamal.NewTwistedElgamal(),
				msg:       &MsgTransfer{},
			},
			wantErr:    true,
			wantErrMsg: "invalid sender address (empty address string is not allowed): invalid address",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.args.msg.Decrypt(tt.args.decryptor, tt.args.privKey, tt.args.decryptAvailableBalance, tt.args.address)
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.wantErrMsg, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgInitializeAccount_FromProtoInvalidInputs(t *testing.T) {
	tests := []struct {
		name       string
		setUp      func(msg *MsgInitializeAccount) *MsgInitializeAccount
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "ValidateBasic fails",
			setUp: func(msg *MsgInitializeAccount) *MsgInitializeAccount {
				msg.FromAddress = ""
				return msg
			},
			wantErr:    true,
			wantErrMsg: "invalid sender address (empty address string is not allowed): invalid address",
		},
		{
			name: "invalid PublicKey",
			setUp: func(msg *MsgInitializeAccount) *MsgInitializeAccount {
				msg.PublicKey = []byte{1, 2, 3}
				return msg
			},
			wantErr:    true,
			wantErrMsg: "edwards25519: invalid point encoding length",
		},
		{
			name: "invalid PendingBalanceLo",
			setUp: func(msg *MsgInitializeAccount) *MsgInitializeAccount {
				msg.PendingBalanceLo = &Ciphertext{}
				return msg
			},
			wantErr:    true,
			wantErrMsg: "edwards25519: invalid point encoding length",
		},
		{
			name: "invalid PendingBalanceHi",
			setUp: func(msg *MsgInitializeAccount) *MsgInitializeAccount {
				msg.PendingBalanceHi = &Ciphertext{}
				return msg
			},
			wantErr:    true,
			wantErrMsg: "edwards25519: invalid point encoding length",
		},
		{
			name: "invalid AvailableBalance",
			setUp: func(msg *MsgInitializeAccount) *MsgInitializeAccount {
				msg.AvailableBalance = &Ciphertext{}
				return msg
			},
			wantErr:    true,
			wantErrMsg: "edwards25519: invalid point encoding length",
		},
		{
			name: "invalid Proofs",
			setUp: func(msg *MsgInitializeAccount) *MsgInitializeAccount {
				msg.Proofs.ZeroPendingBalanceHiProof = &ZeroBalanceProof{}
				return msg
			},
			wantErr:    true,
			wantErrMsg: "zero proof is invalid: invalid request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := getMsgInitializeAccount()
			if tt.setUp != nil {
				m = tt.setUp(m)
			}
			got, err := m.FromProto()
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.wantErrMsg, err.Error())
			} else {
				require.NoError(t, err)
				require.NotNil(t, got)
			}
		})
	}
}

func getMsgInitializeAccount() *MsgInitializeAccount {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	sourcePrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	eg := elgamal.NewTwistedElgamal()
	sourceKeypair, _ := utils.GetElGamalKeyPair(*sourcePrivateKey, testDenom)
	aesPK, _ := utils.GetAESKey(*sourcePrivateKey, testDenom)
	bigIntZero := big.NewInt(0)

	decryptableBalance, _ := encryption.EncryptAESGCM(bigIntZero, aesPK)
	encryptedZero, _, _ := eg.Encrypt(sourceKeypair.PublicKey, bigIntZero)

	// Generate the proof
	pubkeyValidityProof, _ := zkproofs.NewPubKeyValidityProof(
		sourceKeypair.PublicKey,
		sourceKeypair.PrivateKey)

	zeroBalProof, _ := zkproofs.NewZeroBalanceProof(
		sourceKeypair,
		encryptedZero)

	proofs := &InitializeAccountProofs{
		pubkeyValidityProof,
		zeroBalProof,
		zeroBalProof,
		zeroBalProof,
	}
	address1 := sdk.AccAddress("address1")

	encryptedZeroProto := NewCiphertextProto(encryptedZero)
	proofsProto := NewInitializeAccountMsgProofs(proofs)
	return &MsgInitializeAccount{
		FromAddress:        address1.String(),
		Denom:              testDenom,
		PublicKey:          sourceKeypair.PublicKey.ToAffineCompressed(),
		DecryptableBalance: decryptableBalance,
		PendingBalanceLo:   encryptedZeroProto,
		PendingBalanceHi:   encryptedZeroProto,
		AvailableBalance:   encryptedZeroProto,
		Proofs:             proofsProto,
	}
}

func TestMsgInitializeAccount_Decrypt(t *testing.T) {
	type fields struct {
		msg *MsgInitializeAccount
	}
	type args struct {
		decryptor               *elgamal.TwistedElGamal
		privKey                 ecdsa.PrivateKey
		decryptAvailableBalance bool
	}
	tests := []struct {
		name       string
		fields     fields
		args       args
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "nil decryptor",
			fields: fields{
				msg: getMsgInitializeAccount(),
			},
			args: args{
				decryptor: nil,
			},
			wantErr:    true,
			wantErrMsg: "decryptor is required: invalid request",
		},
		{
			name: "invalid message",
			fields: fields{
				msg: &MsgInitializeAccount{},
			},
			args: args{
				decryptor: elgamal.NewTwistedElgamal(),
			},
			wantErr:    true,
			wantErrMsg: "invalid sender address (empty address string is not allowed): invalid address",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.fields.msg.Decrypt(tt.args.decryptor, tt.args.privKey, tt.args.decryptAvailableBalance)
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.wantErrMsg, err.Error())
			} else {
				require.NoError(t, err)
				require.NotNil(t, got)
			}
		})
	}
}

func TestMsgApplyPendingBalance_FromProto(t *testing.T) {
	tests := []struct {
		name       string
		setUp      func(msg *MsgApplyPendingBalance) *MsgApplyPendingBalance
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:    "fromProto returns if message is valid",
			wantErr: false,
		},
		{
			name: "ValidateBasic fails",
			setUp: func(msg *MsgApplyPendingBalance) *MsgApplyPendingBalance {
				msg.Address = ""
				return msg
			},
			wantErr:    true,
			wantErrMsg: "invalid address (empty address string is not allowed): invalid address",
		},
		{
			name: "invalid CurrentAvailableBalance",
			setUp: func(msg *MsgApplyPendingBalance) *MsgApplyPendingBalance {
				msg.CurrentAvailableBalance = &Ciphertext{}
				return msg
			},
			wantErr:    true,
			wantErrMsg: "edwards25519: invalid point encoding length",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := getMsgApplyPendingBalance()
			if tt.setUp != nil {
				m = tt.setUp(m)
			}
			_, err := m.FromProto()
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.wantErrMsg, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func getMsgApplyPendingBalance() *MsgApplyPendingBalance {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	sourcePrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	eg := elgamal.NewTwistedElgamal()
	sourceKeypair, _ := utils.GetElGamalKeyPair(*sourcePrivateKey, testDenom)
	aesPK, _ := utils.GetAESKey(*sourcePrivateKey, testDenom)
	balance := big.NewInt(100)

	decryptableBalance, _ := encryption.EncryptAESGCM(balance, aesPK)
	encryptedBalance, _, _ := eg.Encrypt(sourceKeypair.PublicKey, balance)

	address1 := sdk.AccAddress("address1")

	encryptedProto := NewCiphertextProto(encryptedBalance)
	return &MsgApplyPendingBalance{
		Address:                        address1.String(),
		Denom:                          testDenom,
		NewDecryptableAvailableBalance: decryptableBalance,
		CurrentPendingBalanceCounter:   2,
		CurrentAvailableBalance:        encryptedProto,
	}
}

func TestMsgApplyPendingBalance_Decrypt(t *testing.T) {
	type fields struct {
		msg *MsgApplyPendingBalance
	}
	type args struct {
		decryptor               *elgamal.TwistedElGamal
		privKey                 ecdsa.PrivateKey
		decryptAvailableBalance bool
	}
	tests := []struct {
		name       string
		fields     fields
		args       args
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "nil decryptor",
			fields: fields{
				msg: getMsgApplyPendingBalance(),
			},
			args: args{
				decryptor: nil,
			},
			wantErr:    true,
			wantErrMsg: "decryptor is required: invalid request",
		},
		{
			name: "invalid message",
			fields: fields{
				msg: &MsgApplyPendingBalance{},
			},
			args: args{
				decryptor: elgamal.NewTwistedElgamal(),
			},
			wantErr:    true,
			wantErrMsg: "invalid address (empty address string is not allowed): invalid address",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.fields.msg.Decrypt(tt.args.decryptor, tt.args.privKey, tt.args.decryptAvailableBalance)
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.wantErrMsg, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgCloseAccount_FromProtoInvalidInputs(t *testing.T) {
	tests := []struct {
		name       string
		setUp      func(msg *MsgCloseAccount) *MsgCloseAccount
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:    "fromProto returns if message is valid",
			wantErr: false,
		},
		{
			name: "ValidateBasic fails",
			setUp: func(msg *MsgCloseAccount) *MsgCloseAccount {
				msg.Address = ""
				return msg
			},
			wantErr:    true,
			wantErrMsg: "invalid address (empty address string is not allowed): invalid address",
		},
		{
			name: "invalid CloseAccountMsgProofs",
			setUp: func(msg *MsgCloseAccount) *MsgCloseAccount {
				msg.Proofs.ZeroAvailableBalanceProof = &ZeroBalanceProof{}
				return msg
			},
			wantErr:    true,
			wantErrMsg: "zero proof is invalid: invalid request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := getMsgCloseAccount()
			if tt.setUp != nil {
				m = tt.setUp(m)
			}
			_, err := m.FromProto()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErrMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func getMsgCloseAccount() *MsgCloseAccount {
	address := sdk.AccAddress("address1")
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	privateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	zeroBigInt := big.NewInt(0)
	eg := elgamal.NewTwistedElgamal()
	keypair, _ := utils.GetElGamalKeyPair(*privateKey, testDenom)
	availableBalanceCiphertext, _, _ := eg.Encrypt(keypair.PublicKey, zeroBigInt)
	pendingBalanceLoCiphertext, _, _ := eg.Encrypt(keypair.PublicKey, zeroBigInt)
	pendingBalanceHiCiphertext, _, _ := eg.Encrypt(keypair.PublicKey, zeroBigInt)

	availableBalanceProof, _ := zkproofs.NewZeroBalanceProof(keypair, availableBalanceCiphertext)
	pendingBalanceProofLo, _ := zkproofs.NewZeroBalanceProof(keypair, pendingBalanceLoCiphertext)
	pendingBalanceProofHi, _ := zkproofs.NewZeroBalanceProof(keypair, pendingBalanceHiCiphertext)

	closeAccountProofs := &CloseAccountProofs{
		ZeroAvailableBalanceProof: availableBalanceProof,
		ZeroPendingBalanceLoProof: pendingBalanceProofLo,
		ZeroPendingBalanceHiProof: pendingBalanceProofHi,
	}

	proof := NewCloseAccountMsgProofs(closeAccountProofs)

	return &MsgCloseAccount{
		Address: address.String(),
		Denom:   testDenom,
		Proofs:  proof,
	}
}

func TestMsgWithdraw_FromProtoInvalidInputs(t *testing.T) {
	tests := []struct {
		name       string
		setUp      func(msg *MsgWithdraw) *MsgWithdraw
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:    "fromProto returns if message is valid",
			wantErr: false,
		},
		{
			name: "ValidateBasic fails",
			setUp: func(msg *MsgWithdraw) *MsgWithdraw {
				msg.FromAddress = ""
				return msg
			},
			wantErr:    true,
			wantErrMsg: "invalid sender address (empty address string is not allowed): invalid address",
		},
		{
			name: "invalid RemainingBalanceCommitment",
			setUp: func(msg *MsgWithdraw) *MsgWithdraw {
				msg.RemainingBalanceCommitment = &Ciphertext{}
				return msg
			},
			wantErr:    true,
			wantErrMsg: "edwards25519: invalid point encoding length",
		},
		{
			name: "invalid Proofs",
			setUp: func(msg *MsgWithdraw) *MsgWithdraw {
				msg.Proofs.RemainingBalanceEqualityProof = &CiphertextCommitmentEqualityProof{}
				return msg
			},
			wantErr:    true,
			wantErrMsg: "ciphertext commitment equality proof is invalid: invalid request",
		},
		{
			name: "invalid amount",
			setUp: func(msg *MsgWithdraw) *MsgWithdraw {
				msg.Amount = ""
				return msg
			},
			wantErr:    true,
			wantErrMsg: "amount is not valid: invalid request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := getMsgWithdraw()
			if tt.setUp != nil {
				m = tt.setUp(m)
			}
			_, err := m.FromProto()
			if tt.wantErr {
				require.Error(t, err)
				require.Equal(t, tt.wantErrMsg, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func getMsgWithdraw() *MsgWithdraw {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	sourcePrivateKey, _ := ecdsa.GenerateKey(secp256k1.S256(), crand.Reader)
	eg := elgamal.NewTwistedElgamal()
	sourceKeypair, _ := utils.GetElGamalKeyPair(*sourcePrivateKey, testDenom)
	aesPK, _ := utils.GetAESKey(*sourcePrivateKey, testDenom)

	currentBalance := big.NewInt(500000000)
	currentBalanceCt, _, _ := eg.Encrypt(sourceKeypair.PublicKey, currentBalance)
	withdrawAmount := big.NewInt(10000)
	withdrawAmountCt, _ := eg.SubScalar(currentBalanceCt, withdrawAmount)
	newBalance := new(big.Int).Sub(currentBalance, withdrawAmount)
	newBalanceScalar, _ := curves.ED25519().Scalar.SetBigInt(newBalance)
	decryptableBalance, _ := encryption.EncryptAESGCM(newBalance, aesPK)
	newBalanceCommitment, randomness, _ := eg.Encrypt(sourceKeypair.PublicKey, newBalance)

	// Generate the proofs
	rangeProof, _ := zkproofs.NewRangeProof(128, newBalance, randomness)
	ciphertextCommitmentEqualityProof, _ := zkproofs.NewCiphertextCommitmentEqualityProof(
		sourceKeypair,
		withdrawAmountCt,
		&randomness,
		&newBalanceScalar)

	proofs := &WithdrawProofs{
		rangeProof,
		ciphertextCommitmentEqualityProof,
	}
	address1 := sdk.AccAddress("address1")

	newBalanceProto := NewCiphertextProto(newBalanceCommitment)

	proofsProto := NewWithdrawMsgProofs(proofs)
	return &MsgWithdraw{
		FromAddress:                address1.String(),
		Denom:                      testDenom,
		Amount:                     withdrawAmount.String(),
		RemainingBalanceCommitment: newBalanceProto,
		DecryptableBalance:         decryptableBalance,
		Proofs:                     proofsProto,
	}
}

func TestMsgWithdraw_Decrypt(t *testing.T) {
	type fields struct {
		msg *MsgWithdraw
	}
	type args struct {
		decryptor               *elgamal.TwistedElGamal
		privKey                 ecdsa.PrivateKey
		decryptAvailableBalance bool
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
		wantMsg string
	}{
		{
			name: "nil decryptor",
			fields: fields{
				msg: getMsgWithdraw(),
			},
			args: args{
				decryptor: nil,
			},
			wantErr: true,
			wantMsg: "decryptor is required: invalid request",
		},
		{
			name: "invalid message",
			fields: fields{
				msg: &MsgWithdraw{},
			},
			args: args{
				decryptor: elgamal.NewTwistedElgamal(),
			},
			wantErr: true,
			wantMsg: "invalid sender address (empty address string is not allowed): invalid address",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.fields.msg.Decrypt(tt.args.decryptor, tt.args.privKey, tt.args.decryptAvailableBalance)
			if wantErr := tt.wantErr; wantErr {
				require.Error(t, err)
				require.Equal(t, tt.wantMsg, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgTransfer_Route(t *testing.T) {
	msg := &MsgTransfer{}
	require.Equal(t, "confidentialtransfers", msg.Route())
}

func TestMsgTransfer_Type(t *testing.T) {
	msg := &MsgTransfer{}
	require.Equal(t, "transfer", msg.Type())
}

func TestNewMsgInitializeAccount_Route(t *testing.T) {
	msg := &MsgInitializeAccount{}
	require.Equal(t, "confidentialtransfers", msg.Route())
}

func TestNewMsgInitializeAccount_Type(t *testing.T) {
	msg := &MsgInitializeAccount{}
	require.Equal(t, "initialize_account", msg.Type())
}

func TestNewMsgApplyPendingBalance_Route(t *testing.T) {
	msg := &MsgApplyPendingBalance{}
	require.Equal(t, "confidentialtransfers", msg.Route())
}

func TestNewMsgApplyPendingBalance_Type(t *testing.T) {
	msg := &MsgApplyPendingBalance{}
	require.Equal(t, "apply_pending_balance", msg.Type())
}

func TestNewMsgCloseAccount_Route(t *testing.T) {
	msg := &MsgCloseAccount{}
	require.Equal(t, "confidentialtransfers", msg.Route())
}

func TestNewMsgCloseAccount_Type(t *testing.T) {
	msg := &MsgCloseAccount{}
	require.Equal(t, "close_account", msg.Type())
}

func TestNewMsgWithdraw_Route(t *testing.T) {
	msg := &MsgWithdraw{}
	require.Equal(t, "confidentialtransfers", msg.Route())
}

func TestNewMsgWithdraw_Type(t *testing.T) {
	msg := &MsgWithdraw{}
	require.Equal(t, "withdraw", msg.Type())
}

func TestMsgTransfer_GetSignBytes(t *testing.T) {
	msg := &MsgTransfer{}
	res := msg.GetSignBytes()
	require.NotNil(t, res)
}

func TestMsgTransfer_GetSigners(t *testing.T) {
	msg := &MsgTransfer{}
	res := msg.GetSigners()
	require.NotNil(t, res)
}

func TestMsgInitializeAccount_GetSignBytes(t *testing.T) {
	msg := &MsgInitializeAccount{}
	res := msg.GetSignBytes()
	require.NotNil(t, res)
}

func TestMsgInitializeAccount_GetSigners(t *testing.T) {
	msg := &MsgInitializeAccount{}
	res := msg.GetSigners()
	require.NotNil(t, res)
}

func TestMsgApplyPendingBalance_GetSignBytes(t *testing.T) {
	msg := &MsgApplyPendingBalance{}
	res := msg.GetSignBytes()
	require.NotNil(t, res)
}

func TestMsgApplyPendingBalance_GetSigners(t *testing.T) {
	msg := &MsgApplyPendingBalance{}
	res := msg.GetSigners()
	require.NotNil(t, res)
}

func TestMsgCloseAccount_GetSignBytes(t *testing.T) {
	msg := &MsgCloseAccount{}
	res := msg.GetSignBytes()
	require.NotNil(t, res)
}

func TestMsgCloseAccount_GetSigners(t *testing.T) {
	msg := &MsgCloseAccount{}
	res := msg.GetSigners()
	require.NotNil(t, res)
}

func TestMsgWithdraw_GetSignBytes(t *testing.T) {
	msg := &MsgWithdraw{}
	res := msg.GetSignBytes()
	require.NotNil(t, res)
}

func TestMsgWithdraw_GetSigners(t *testing.T) {
	msg := &MsgWithdraw{}
	res := msg.GetSigners()
	require.NotNil(t, res)
}
