package types

import (
	"crypto/ecdsa"
	"github.com/coinbase/kryptology/pkg/core/curves"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/stretchr/testify/assert"
	"math/big"
	"testing"
)

func TestNewTransfer(t *testing.T) {
	testAddress1 := "sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w"
	testAddress2 := "sei12nqhfjuurt90p6yqkk2txnptrmuta40dl8mk3d"
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	senderPk, _ := encryption.GenerateKey()
	wrongPk, _ := encryption.GenerateKey()
	receiverPk, _ := encryption.GenerateKey()
	aesKey, _ := encryption.GetAESKey(*senderPk, testDenom)
	teg := elgamal.NewTwistedElgamal()
	decryptableBalance, _ := encryption.EncryptAESGCM(big.NewInt(100), aesKey)
	senderKeyPair, _ := teg.KeyGen(*senderPk, testDenom)
	receiverKeyPair, _ := teg.KeyGen(*receiverPk, testDenom)
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
				auditors:                        nil,
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
			wantErrMsg: "denom is empty",
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
