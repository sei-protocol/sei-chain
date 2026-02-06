package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
)

func TestABCIPubKey(t *testing.T) {
	pkEd := ed25519.GenerateSecretKey().Public()
	err := testABCIPubKey(t, pkEd)
	assert.NoError(t, err)
}

func testABCIPubKey(t *testing.T, pk crypto.PubKey) error {
	abciPubKey := crypto.PubKeyToProto(pk)
	pk2, err := crypto.PubKeyFromProto(abciPubKey)
	require.NoError(t, err)
	require.Equal(t, pk, pk2)
	return nil
}

func TestABCIValidators(t *testing.T) {
	pkEd := ed25519.GenerateSecretKey().Public()

	// correct validator
	tmValExpected := NewValidator(pkEd, 10)

	tmVal := NewValidator(pkEd, 10)

	abciVal := TM2PB.ValidatorUpdate(tmVal)
	tmVals, err := PB2TM.ValidatorUpdates([]abci.ValidatorUpdate{abciVal})
	assert.NoError(t, err)
	assert.Equal(t, tmValExpected, tmVals[0])

	abciVals := TM2PB.ValidatorUpdates(NewValidatorSet(tmVals))
	assert.Equal(t, []abci.ValidatorUpdate{abciVal}, abciVals)

	// val with address
	tmVal.Address = pkEd.Address()

	abciVal = TM2PB.ValidatorUpdate(tmVal)
	tmVals, err = PB2TM.ValidatorUpdates([]abci.ValidatorUpdate{abciVal})
	assert.NoError(t, err)
	assert.Equal(t, tmValExpected, tmVals[0])
}

func TestABCIValidatorWithoutPubKey(t *testing.T) {
	pkEd := ed25519.GenerateSecretKey().Public()

	abciVal := TM2PB.Validator(NewValidator(pkEd, 10))

	// pubkey must be nil
	tmValExpected := abci.Validator{
		Address: pkEd.Address(),
		Power:   10,
	}

	assert.Equal(t, tmValExpected, abciVal)
}
