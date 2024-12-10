package types

import (
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/stretchr/testify/assert"
	"math/big"
	"testing"
)

func TestNewTransferErrorIfSenderEqualsRecipient(t *testing.T) {
	senderPk, _ := encryption.GenerateKey()

	_, err := NewTransfer(
		senderPk,
		"sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w",
		"sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w",
		"testDenom",
		"",
		nil,
		0,
		nil,
		nil,
	)
	assert.EqualError(t, err, "sender and recipient addresses cannot be the same")
}

func TestNewTransferErrorIfInvalidDenom(t *testing.T) {
	senderPk, _ := encryption.GenerateKey()
	_, err := NewTransfer(
		senderPk,
		"sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w",
		"sei12nqhfjuurt90p6yqkk2txnptrmuta40dl8mk3d",
		"",
		"",
		nil,
		0,
		nil,
		nil,
	)
	assert.EqualError(t, err, "denom is empty")
}

func TestNewTransferErrorIfInvalidCiphertext(t *testing.T) {
	senderPk, _ := encryption.GenerateKey()
	_, err := NewTransfer(
		senderPk,
		"sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w",
		"sei12nqhfjuurt90p6yqkk2txnptrmuta40dl8mk3d",
		"test",
		"invalid",
		nil,
		0,
		nil,
		nil,
	)
	assert.EqualError(t, err, "illegal base64 data at input byte 4")
}

func TestNewTransferFailsIfInsufficientBalance(t *testing.T) {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	senderPk, _ := encryption.GenerateKey()
	aesKey, _ := encryption.GetAESKey(*senderPk, testDenom)
	decryptableBalance, err := encryption.EncryptAESGCM(big.NewInt(10), aesKey)
	transferAmount := uint64(100)

	_, err = NewTransfer(
		senderPk,
		"sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w",
		"sei12nqhfjuurt90p6yqkk2txnptrmuta40dl8mk3d",
		testDenom,
		decryptableBalance,
		nil,
		transferAmount,
		nil,
		nil,
	)
	assert.EqualError(t, err, "insufficient balance")
}

func TestNewTransfer(t *testing.T) {
	testDenom := "factory/sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w/TEST"
	senderPk, _ := encryption.GenerateKey()
	receiverPk, _ := encryption.GenerateKey()
	aesKey, _ := encryption.GetAESKey(*senderPk, testDenom)
	teg := elgamal.NewTwistedElgamal()
	decryptableBalance, err := encryption.EncryptAESGCM(big.NewInt(100), aesKey)
	senderKeyPair, _ := teg.KeyGen(*senderPk, testDenom)
	receiverKeyPair, _ := teg.KeyGen(*receiverPk, testDenom)
	transferAmount := uint64(100)
	ct, _, _ := teg.Encrypt(senderKeyPair.PublicKey, big.NewInt(0))

	transfer, err := NewTransfer(
		senderPk,
		"sei1ft98au55a24vnu9tvd92cz09pzcfqkm5vlx99w",
		"sei12nqhfjuurt90p6yqkk2txnptrmuta40dl8mk3d",
		testDenom,
		decryptableBalance,
		ct,
		transferAmount,
		&receiverKeyPair.PublicKey,
		nil,
	)
	assert.NoError(t, err)
	assert.NotNil(t, transfer)
}
