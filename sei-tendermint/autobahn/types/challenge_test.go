package types_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestChallengeSignature(t *testing.T) {
	rng := utils.TestRng()
	key := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	validator := types.SecretKeyFromED25519(key)
	var challenge types.Challenge
	utils.OrPanic1(rng.Read(challenge[:]))
	sig := validator.SignChallenge(challenge)
	require.Equal(t, validator.Public(), sig.Key())
	require.NoError(t, sig.Verify(challenge))
	require.Equal(t, key.Sign(challenge[:]).Bytes(), sig.Bytes())

	decoded, err := types.ChallengeSigFromBytes(sig.Key(), sig.Bytes())
	require.NoError(t, err)
	require.Equal(t, sig.Key(), decoded.Key())
	require.NoError(t, decoded.Verify(challenge))
	_, err = types.ChallengeSigFromBytes(sig.Key(), sig.Bytes()[:63])
	require.Error(t, err)

	otherKey, err := types.ChallengeSigFromBytes(types.GenPublicKey(rng), sig.Bytes())
	require.NoError(t, err)
	require.Error(t, otherKey.Verify(challenge))
	challenge[0] ^= 1
	require.Error(t, sig.Verify(challenge))
}
