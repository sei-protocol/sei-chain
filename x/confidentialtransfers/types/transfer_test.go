package types

import (
	"crypto/ecdsa"
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/utils"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/sei-protocol/sei-cryptography/pkg/zkproofs"
	"github.com/stretchr/testify/assert"
)

func TestNewTransfer(t *testing.T) {
	testAddress1 := "sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w"
	testAddress2 := "sei12nqhfjuurt90p6yqkk2txnptrmuta40dl8mk3d"
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	senderPk, _ := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	wrongPk, _ := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	receiverPk, _ := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	aesKey, _ := utils.GetAESKey(*senderPk, testDenom)
	teg := elgamal.NewTwistedElgamal()
	decryptableBalance, _ := encryption.EncryptAESGCM(big.NewInt(100), aesKey)
	senderKeyPair, _ := utils.GetElGamalKeyPair(*senderPk, testDenom)
	receiverKeyPair, _ := utils.GetElGamalKeyPair(*receiverPk, testDenom)
	transferAmount := uint64(100)
	ct, _, _ := teg.Encrypt(senderKeyPair.PublicKey, big.NewInt(0))
	type args struct {
		privateKey                      *ecdsa.PrivateKey
		senderAddr                      string
		recipientAddr                   string
		denom                           string
		senderCurrentDecryptableBalance string
		senderCurrentAvailableBalance   *elgamal.Ciphertext
		amount                          uint64
		recipientPubkey                 *curves.Point
		auditors                        []AuditorInput
	}
	tests := []struct {
		name       string
		args       args
		wantError  bool
		wantErrMsg string
	}{
		{
			name: "transfer object created successfully",
			args: args{
				privateKey:                      senderPk,
				senderAddr:                      testAddress1,
				recipientAddr:                   testAddress2,
				denom:                           testDenom,
				senderCurrentDecryptableBalance: decryptableBalance,
				senderCurrentAvailableBalance:   ct,
				amount:                          transferAmount,
				recipientPubkey:                 &receiverKeyPair.PublicKey,
			},
			wantError: false,
		},
		{
			name: "transfer object creation fails with wrong private key",
			args: args{
				privateKey:                      wrongPk,
				senderAddr:                      testAddress1,
				recipientAddr:                   testAddress2,
				denom:                           testDenom,
				senderCurrentDecryptableBalance: decryptableBalance,
				senderCurrentAvailableBalance:   ct,
				amount:                          transferAmount,
				recipientPubkey:                 &receiverKeyPair.PublicKey,
			},
			wantError:  true,
			wantErrMsg: "cipher: message authentication failed",
		},
		{
			name: "transfer object creation fails with insufficient balance",
			args: args{
				privateKey:                      senderPk,
				senderAddr:                      testAddress1,
				recipientAddr:                   testAddress2,
				denom:                           testDenom,
				senderCurrentDecryptableBalance: decryptableBalance,
				senderCurrentAvailableBalance:   ct,
				amount:                          1000,
				recipientPubkey:                 &receiverKeyPair.PublicKey,
			},
			wantError:  true,
			wantErrMsg: "insufficient balance",
		},
		{
			name: "transfer object creation fails with invalid ciphertext",
			args: args{
				privateKey:                      senderPk,
				senderAddr:                      testAddress1,
				recipientAddr:                   testAddress2,
				denom:                           testDenom,
				senderCurrentDecryptableBalance: "invalid",
				senderCurrentAvailableBalance:   ct,
				recipientPubkey:                 &receiverKeyPair.PublicKey,
			},
			wantError:  true,
			wantErrMsg: "illegal base64 data at input byte 4",
		},
		{
			name: "transfer object creation fails with invalid denom",
			args: args{
				privateKey:    senderPk,
				senderAddr:    testAddress1,
				recipientAddr: testAddress2,
				denom:         "",
			},
			wantError:  true,
			wantErrMsg: "denom is required",
		},
		{
			name: "transfer object creation fails if sender and recipient are the same",
			args: args{
				privateKey:    senderPk,
				senderAddr:    testAddress1,
				recipientAddr: testAddress1,
			},
			wantError:  true,
			wantErrMsg: "sender and recipient addresses cannot be the same",
		},
		{
			name:       "transfer object creation fails if private key is nil",
			args:       args{},
			wantError:  true,
			wantErrMsg: "private key is required",
		},
		{
			name: "transfer object creation fails if sender address is empty",
			args: args{
				privateKey: senderPk,
			},
			wantError:  true,
			wantErrMsg: "sender address is required",
		},
		{
			name: "transfer object creation fails if recipient address is empty",
			args: args{
				privateKey: senderPk,
				senderAddr: testAddress1,
			},
			wantError:  true,
			wantErrMsg: "recipient address is required",
		},
		{
			name: "transfer object creation fails if available balance is nil",
			args: args{
				privateKey:                      senderPk,
				senderAddr:                      testAddress1,
				recipientAddr:                   testAddress2,
				denom:                           testDenom,
				senderCurrentDecryptableBalance: decryptableBalance,
			},
			wantError:  true,
			wantErrMsg: "available balance is required",
		},
		{
			name: "transfer object creation fails if recipientPubkey is nil",
			args: args{
				privateKey:                      senderPk,
				senderAddr:                      testAddress1,
				recipientAddr:                   testAddress2,
				denom:                           testDenom,
				senderCurrentDecryptableBalance: decryptableBalance,
				senderCurrentAvailableBalance:   ct,
				amount:                          transferAmount,
				recipientPubkey:                 nil,
			},
			wantError:  true,
			wantErrMsg: "recipient public key is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewTransfer(
				tt.args.privateKey,
				tt.args.senderAddr,
				tt.args.recipientAddr,
				tt.args.denom,
				tt.args.senderCurrentDecryptableBalance,
				tt.args.senderCurrentAvailableBalance,
				tt.args.amount,
				tt.args.recipientPubkey,
				tt.args.auditors)
			if tt.wantError {
				assert.EqualError(t, err, tt.wantErrMsg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, got)
			}
		})
	}
}

func Test_createTransferPartyParams(t *testing.T) {
	partyAddress := "sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w"
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	partyPk, _ := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	senderPk, _ := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	teg := elgamal.NewTwistedElgamal()
	partyKeyPair, _ := utils.GetElGamalKeyPair(*partyPk, testDenom)
	senderKeyPair, _ := utils.GetElGamalKeyPair(*senderPk, testDenom)
	transferLoBits := big.NewInt(100)
	transferHiBits := big.NewInt(100)
	loBitsCt, _, _ := teg.Encrypt(senderKeyPair.PublicKey, transferLoBits)
	hiBitsCt, _, _ := teg.Encrypt(senderKeyPair.PublicKey, transferHiBits)
	type args struct {
		partyAddress                  string
		transferLoBits                *big.Int
		transferHiBits                *big.Int
		senderKeyPair                 *elgamal.KeyPair
		senderEncryptedTransferLoBits *elgamal.Ciphertext
		senderEncryptedTransferHiBits *elgamal.Ciphertext
		partyPubkey                   *curves.Point
	}
	tests := []struct {
		name       string
		args       args
		wantError  bool
		wantErrMsg string
	}{
		{
			name: "transfer party params created successfully",
			args: args{
				partyAddress:                  partyAddress,
				transferLoBits:                transferLoBits,
				transferHiBits:                transferHiBits,
				senderKeyPair:                 senderKeyPair,
				senderEncryptedTransferLoBits: loBitsCt,
				senderEncryptedTransferHiBits: hiBitsCt,
				partyPubkey:                   &partyKeyPair.PublicKey,
			},
			wantError: false,
		},
		{
			name: "transfer party params creation fails with invalid lo bits",
			args: args{
				partyAddress:                  partyAddress,
				transferLoBits:                nil,
				transferHiBits:                transferHiBits,
				senderKeyPair:                 senderKeyPair,
				senderEncryptedTransferLoBits: loBitsCt,
				senderEncryptedTransferHiBits: hiBitsCt,
				partyPubkey:                   &partyKeyPair.PublicKey,
			},
			wantError:  true,
			wantErrMsg: "invalid ciphertext",
		},
		{
			name: "transfer party params creation fails with invalid hi bits",
			args: args{
				partyAddress:                  partyAddress,
				transferLoBits:                transferLoBits,
				transferHiBits:                nil,
				senderKeyPair:                 senderKeyPair,
				senderEncryptedTransferLoBits: loBitsCt,
				senderEncryptedTransferHiBits: hiBitsCt,
				partyPubkey:                   &partyKeyPair.PublicKey,
			},
			wantError:  true,
			wantErrMsg: "invalid ciphertext",
		},
		{
			name: "transfer party params creation fails with invalid lo bits ciphertext",
			args: args{
				partyAddress:                  partyAddress,
				transferLoBits:                transferLoBits,
				transferHiBits:                transferHiBits,
				senderKeyPair:                 senderKeyPair,
				senderEncryptedTransferLoBits: nil,
				senderEncryptedTransferHiBits: hiBitsCt,
				partyPubkey:                   &partyKeyPair.PublicKey,
			},
			wantError:  true,
			wantErrMsg: "sourceCiphertext is invalid",
		},
		{
			name: "transfer party params creation fails with invalid hi bits ciphertext",
			args: args{
				partyAddress:                  partyAddress,
				transferLoBits:                transferLoBits,
				transferHiBits:                transferHiBits,
				senderKeyPair:                 senderKeyPair,
				senderEncryptedTransferLoBits: loBitsCt,
				senderEncryptedTransferHiBits: nil,
				partyPubkey:                   &partyKeyPair.PublicKey,
			},
			wantError:  true,
			wantErrMsg: "sourceCiphertext is invalid",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := createTransferPartyParams(
				tt.args.partyAddress,
				tt.args.transferLoBits,
				tt.args.transferHiBits,
				tt.args.senderKeyPair,
				tt.args.senderEncryptedTransferLoBits,
				tt.args.senderEncryptedTransferHiBits,
				tt.args.partyPubkey)
			if tt.wantError {
				assert.EqualError(t, err, tt.wantErrMsg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, got)
			}
		})
	}
}

func Test_VerifyTransferProofs(t *testing.T) {
	testAddress1 := "sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w"
	testAddress2 := "sei12nqhfjuurt90p6yqkk2txnptrmuta40dl8mk3d"
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	senderPk, _ := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	receiverPk, _ := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	aesKey, _ := utils.GetAESKey(*senderPk, testDenom)
	teg := elgamal.NewTwistedElgamal()
	decryptableBalance, _ := encryption.EncryptAESGCM(big.NewInt(200), aesKey)
	senderKeyPair, _ := utils.GetElGamalKeyPair(*senderPk, testDenom)
	receiverKeyPair, _ := utils.GetElGamalKeyPair(*receiverPk, testDenom)
	transferAmount := uint64(100)
	ct, _, _ := teg.Encrypt(senderKeyPair.PublicKey, big.NewInt(200))
	ed25519RangeVerifierFactory := zkproofs.Ed25519RangeVerifierFactory{}
	rangeVerifierFactory := zkproofs.NewCachedRangeVerifierFactory(&ed25519RangeVerifierFactory)

	type args struct {
		params               *Transfer
		senderPubkey         *curves.Point
		recipientPubkey      *curves.Point
		rangeVerifierFactory *zkproofs.CachedRangeVerifierFactory
		setup                func(params *Transfer) *Transfer
		setupNewBalance      func() *elgamal.Ciphertext
	}
	tests := []struct {
		name           string
		args           args
		wantErr        bool
		wantErrMessage string
	}{
		{
			name: "transfer proofs verification error if params are nil",
			args: args{
				setup: func(params *Transfer) *Transfer {
					return nil
				},
			},
			wantErr:        true,
			wantErrMessage: "transfer params are required",
		},
		{
			name:           "transfer proofs verification error if sender public key is nil",
			args:           args{},
			wantErr:        true,
			wantErrMessage: "sender public key is required",
		},
		{
			name: "transfer proofs verification error if recipient public key is nil",
			args: args{
				senderPubkey: &senderKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "recipient public key is required",
		},
		{
			name: "transfer proofs verification error if new balance ciphertext is nil",
			args: args{
				setupNewBalance: func() *elgamal.Ciphertext { return nil },
				senderPubkey:    &senderKeyPair.PublicKey,
				recipientPubkey: &receiverKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "new balance ciphertext is required",
		},
		{
			name: "transfer proofs verification error if range verifier factory is nil",
			args: args{
				senderPubkey:    &senderKeyPair.PublicKey,
				recipientPubkey: &receiverKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "range verifier factory is required",
		},
		{
			name: "transfer proofs verification error if transfer remaining balance commitment validity proof is invalid",
			args: args{
				setup: func(params *Transfer) *Transfer {
					params.Proofs.RemainingBalanceCommitmentValidityProof = &zkproofs.CiphertextValidityProof{}
					return params
				},
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr:        true,
			wantErrMessage: "failed to verify remaining balance commitment",
		},
		{
			name: "transfer proofs verification error if transfer sender transfer amount lo validity proof is invalid",
			args: args{
				setup: func(params *Transfer) *Transfer {
					params.Proofs.SenderTransferAmountLoValidityProof = &zkproofs.CiphertextValidityProof{}
					return params
				},
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr:        true,
			wantErrMessage: "failed to verify sender transfer amount lo",
		},
		{
			name: "transfer proofs verification error if transfer sender transfer amount hi validity proof is invalid",
			args: args{
				setup: func(params *Transfer) *Transfer {
					params.Proofs.SenderTransferAmountHiValidityProof = &zkproofs.CiphertextValidityProof{}
					return params
				},
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr:        true,
			wantErrMessage: "failed to verify sender transfer amount hi",
		},
		{
			name: "transfer proofs verification error if transfer transfer amount lo range proof is invalid",
			args: args{
				setup: func(params *Transfer) *Transfer {
					params.Proofs.TransferAmountLoRangeProof = &zkproofs.RangeProof{}
					return params
				},
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr:        true,
			wantErrMessage: "failed to verify transfer amount lo range proof",
		},
		{
			name: "transfer proofs verification error if transfer transfer amount hi range proof is invalid",
			args: args{
				setup: func(params *Transfer) *Transfer {
					params.Proofs.TransferAmountLoRangeProof = &zkproofs.RangeProof{}
					return params
				},
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr:        true,
			wantErrMessage: "failed to verify transfer amount hi range proof",
		},
		{
			name: "transfer proofs verification error if transfer recipient transfer amount lo validity proof is invalid",
			args: args{
				setup: func(params *Transfer) *Transfer {
					params.Proofs.RecipientTransferAmountLoValidityProof = &zkproofs.CiphertextValidityProof{}
					return params
				},
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr:        true,
			wantErrMessage: "failed to verify recipient transfer amount lo",
		},
		{
			name: "transfer proofs verification error if transfer recipient transfer amount hi validity proof is invalid",
			args: args{
				setup: func(params *Transfer) *Transfer {
					params.Proofs.RecipientTransferAmountHiValidityProof = &zkproofs.CiphertextValidityProof{}
					return params
				},
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr:        true,
			wantErrMessage: "failed to verify recipient transfer amount hi",
		},
		{
			name: "transfer proofs verification error if remaining balance range proof is invalid",
			args: args{
				setup: func(params *Transfer) *Transfer {
					params.Proofs.RemainingBalanceRangeProof = &zkproofs.RangeProof{}
					return params
				},
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr:        true,
			wantErrMessage: "invalid proof",
		},
		{
			name: "transfer proofs verification error if transfer remaining balance equality proof is invalid",
			args: args{
				setup: func(params *Transfer) *Transfer {
					params.Proofs.RemainingBalanceEqualityProof = &zkproofs.CiphertextCommitmentEqualityProof{}
					return params
				},
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr:        true,
			wantErrMessage: "ciphertext commitment equality verification failed",
		},
		{
			name: "transfer proofs verification error if transfer amount lo equality proof is invalid",
			args: args{
				setup: func(params *Transfer) *Transfer {
					params.Proofs.TransferAmountLoEqualityProof = &zkproofs.CiphertextCiphertextEqualityProof{}
					return params
				},
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr:        true,
			wantErrMessage: "ciphertext ciphertext equality verification on transfer amount lo failed",
		},
		{
			name: "transfer proofs verification error if transfer amount hi equality proof is invalid",
			args: args{
				setup: func(params *Transfer) *Transfer {
					params.Proofs.TransferAmountHiEqualityProof = &zkproofs.CiphertextCiphertextEqualityProof{}
					return params
				},
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr:        true,
			wantErrMessage: "ciphertext ciphertext equality verification on transfer amount hi failed",
		},
		{
			name: "transfer proofs verification succeeds if all proofs are valid",
			args: args{
				senderPubkey:         &senderKeyPair.PublicKey,
				recipientPubkey:      &receiverKeyPair.PublicKey,
				rangeVerifierFactory: rangeVerifierFactory,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		transfer, _ := NewTransfer(
			senderPk,
			testAddress1,
			testAddress2,
			testDenom,
			decryptableBalance,
			ct,
			transferAmount,
			&receiverKeyPair.PublicKey,
			nil)
		newSenderBalanceCiphertext, _ := teg.SubWithLoHi(ct, transfer.SenderTransferAmountLo, transfer.SenderTransferAmountHi)
		if tt.args.setup != nil {
			transfer = tt.args.setup(transfer)
		}
		if tt.args.setupNewBalance != nil {
			newSenderBalanceCiphertext = tt.args.setupNewBalance()
		}
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyTransferProofs(
				transfer,
				tt.args.senderPubkey,
				tt.args.recipientPubkey,
				newSenderBalanceCiphertext,
				tt.args.rangeVerifierFactory)
			if tt.wantErr {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.wantErrMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVerifyAuditorProof(t *testing.T) {
	// Common setup for all test cases
	senderPk, err := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	assert.NoError(t, err, "failed to generate sender private key")

	auditorPk, err := ecdsa.GenerateKey(secp256k1.S256(), rand.Reader)
	assert.NoError(t, err, "failed to generate auditor private key")

	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	teg := elgamal.NewTwistedElgamal()

	senderKeyPair, err := utils.GetElGamalKeyPair(*senderPk, testDenom)
	assert.NoError(t, err, "failed to generate sender key pair")

	auditorKeyPair, err := utils.GetElGamalKeyPair(*auditorPk, testDenom)
	assert.NoError(t, err, "failed to generate auditor key pair")

	// Placeholder values for sender and auditor transfer amounts
	senderTransferAmountLo, _, _ := teg.Encrypt(senderKeyPair.PublicKey, big.NewInt(100))
	senderTransferAmountHi, _, _ := teg.Encrypt(senderKeyPair.PublicKey, big.NewInt(0))

	auditorParams, _ := createTransferPartyParams(
		"sei196wyjlvma8zxpz5u2r5h4lstkmyjc7knuakpws",
		big.NewInt(100),
		big.NewInt(0),
		senderKeyPair,
		senderTransferAmountLo,
		senderTransferAmountHi,
		&auditorKeyPair.PublicKey)

	type args struct {
		senderTransferAmountLo *elgamal.Ciphertext
		senderTransferAmountHi *elgamal.Ciphertext
		auditorParams          *TransferAuditor
		senderPubkey           *curves.Point
		auditorPubkey          *curves.Point
	}

	tests := []struct {
		name           string
		args           args
		setup          func(*args) // Additional setup if needed
		wantErr        bool
		wantErrMessage string
	}{
		{
			name: "valid auditor proof",
			args: args{
				senderTransferAmountLo: senderTransferAmountLo,
				senderTransferAmountHi: senderTransferAmountHi,
				auditorParams:          auditorParams,
				senderPubkey:           &senderKeyPair.PublicKey,
				auditorPubkey:          &auditorKeyPair.PublicKey,
			},
			wantErr: false,
		},
		{
			name: "auditorParams is nil",
			args: args{
				senderTransferAmountLo: senderTransferAmountLo,
				senderTransferAmountHi: senderTransferAmountHi,
				senderPubkey:           &senderKeyPair.PublicKey,
				auditorPubkey:          &auditorKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "auditor params are required",
		},
		{
			name: "sender public key is nil",
			args: args{
				senderTransferAmountLo: senderTransferAmountLo,
				senderTransferAmountHi: senderTransferAmountHi,
				auditorParams:          auditorParams,
				auditorPubkey:          &auditorKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "sender public key is required",
		},
		{
			name: "auditor public key is nil",
			args: args{
				senderTransferAmountLo: senderTransferAmountLo,
				senderTransferAmountHi: senderTransferAmountHi,
				auditorParams:          auditorParams,
				senderPubkey:           &senderKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "auditor public key is required",
		},
		{
			name: "sender transfer amount is nil",
			args: args{
				senderTransferAmountHi: senderTransferAmountHi,
				auditorParams:          auditorParams,
				senderPubkey:           &senderKeyPair.PublicKey,
				auditorPubkey:          &auditorKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "sender transfer amount lo is required",
		},
		{
			name: "invalid sender transfer amount lo",
			args: args{
				senderTransferAmountLo: &elgamal.Ciphertext{}, // Assuming 0 is invalid
				senderTransferAmountHi: senderTransferAmountHi,
				auditorParams:          auditorParams,
				senderPubkey:           &senderKeyPair.PublicKey,
				auditorPubkey:          &auditorKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "ciphertext ciphertext equality verification on auditor transfer amount lo failed",
		},
		{
			name: "sender transfer amount hi is nil",
			args: args{
				senderTransferAmountLo: senderTransferAmountLo,
				auditorParams:          auditorParams,
				senderPubkey:           &senderKeyPair.PublicKey,
				auditorPubkey:          &auditorKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "sender transfer amount hi is required",
		},
		{
			name: "invalid sender transfer amount hi",
			args: args{
				senderTransferAmountLo: senderTransferAmountLo,
				senderTransferAmountHi: &elgamal.Ciphertext{}, // Assuming 0 is invalid
				auditorParams:          auditorParams,
				senderPubkey:           &senderKeyPair.PublicKey,
				auditorPubkey:          &auditorKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "ciphertext ciphertext equality verification on auditor transfer amount hi failed",
		},
		{
			name: "invalid auditorParams - invalid encrypted transfer amount lo",
			args: args{
				senderTransferAmountLo: senderTransferAmountLo,
				senderTransferAmountHi: senderTransferAmountHi,
				auditorParams:          &TransferAuditor{},
				senderPubkey:           &senderKeyPair.PublicKey,
				auditorPubkey:          &auditorKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "failed to verify auditor transfer amount lo",
		},
		{
			name: "invalid auditorParams - invalid transfer amount hi validity proof",
			args: args{
				senderTransferAmountLo: senderTransferAmountLo,
				senderTransferAmountHi: senderTransferAmountHi,
				auditorParams: &TransferAuditor{
					EncryptedTransferAmountLo:     auditorParams.EncryptedTransferAmountLo,
					TransferAmountLoValidityProof: auditorParams.TransferAmountLoValidityProof,
				},
				senderPubkey:  &senderKeyPair.PublicKey,
				auditorPubkey: &auditorKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "failed to verify auditor transfer amount hi",
		},
		{
			name: "invalid auditorParams - invalid transfer ciphertext equality verification amount lo",
			args: args{
				senderTransferAmountLo: senderTransferAmountLo,
				senderTransferAmountHi: senderTransferAmountHi,
				auditorParams: &TransferAuditor{
					EncryptedTransferAmountLo:     auditorParams.EncryptedTransferAmountLo,
					TransferAmountLoValidityProof: auditorParams.TransferAmountLoValidityProof,
					EncryptedTransferAmountHi:     auditorParams.EncryptedTransferAmountHi,
					TransferAmountHiValidityProof: auditorParams.TransferAmountHiValidityProof,
				},
				senderPubkey:  &senderKeyPair.PublicKey,
				auditorPubkey: &auditorKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "ciphertext ciphertext equality verification on auditor transfer amount lo failed",
		},
		{
			name: "invalid auditorParams - invalid transfer ciphertext equality verification amount hi",
			args: args{
				senderTransferAmountLo: senderTransferAmountLo,
				senderTransferAmountHi: senderTransferAmountHi,
				auditorParams: &TransferAuditor{
					EncryptedTransferAmountLo:     auditorParams.EncryptedTransferAmountLo,
					TransferAmountLoValidityProof: auditorParams.TransferAmountLoValidityProof,
					EncryptedTransferAmountHi:     auditorParams.EncryptedTransferAmountHi,
					TransferAmountHiValidityProof: auditorParams.TransferAmountHiValidityProof,
					TransferAmountLoEqualityProof: auditorParams.TransferAmountLoEqualityProof,
				},
				senderPubkey:  &senderKeyPair.PublicKey,
				auditorPubkey: &auditorKeyPair.PublicKey,
			},
			wantErr:        true,
			wantErrMessage: "ciphertext ciphertext equality verification on auditor transfer amount hi failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(&tt.args)
			}

			err = VerifyAuditorProof(
				tt.args.senderTransferAmountLo,
				tt.args.senderTransferAmountHi,
				tt.args.auditorParams,
				tt.args.senderPubkey,
				tt.args.auditorPubkey,
			)

			if tt.wantErr {
				assert.Error(t, err, "expected an error but got none")
				assert.EqualError(t, err, tt.wantErrMessage, "error message mismatch")
			} else {
				assert.NoError(t, err, "did not expect an error but got one")
			}
		})
	}
}
