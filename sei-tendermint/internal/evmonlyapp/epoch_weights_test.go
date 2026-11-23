package evmonlyapp

import (
	"testing"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEpochWeights(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	pk := utils.OrPanic1(atypes.PublicKeyFromBytes(sk.Public().Bytes()))

	got, err := epochWeights(abevm.CommitteeWeights{pk: 42})
	require.NoError(t, err)
	require.Equal(t, map[atypes.PublicKey]uint64{pk: 42}, got)

	got, err = epochWeights(abevm.CommitteeWeights{pk: 0})
	require.NoError(t, err)
	require.Empty(t, got)

}
