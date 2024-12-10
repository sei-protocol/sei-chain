package types

import (
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/stretchr/testify/assert"
	"math/big"
	"testing"
)

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
