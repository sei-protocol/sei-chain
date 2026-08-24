package p2p

import (
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestAppEpochStake_CommitteeWeights(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	wantPK := utils.OrPanic1(atypes.PublicKeyFromBytes(sk.Public().Bytes()))
	app := newTestApp()
	for state := range app.state.Lock() {
		state.Validators = []abci.ValidatorUpdate{{
			PubKey: crypto.PubKeyToProto(sk.Public()),
			Power:  42,
		}}
	}
	got, err := appEpochStake{app: proxy.New(app)}.CommitteeWeights(1)
	require.NoError(t, err)
	require.Equal(t, map[atypes.PublicKey]uint64{wantPK: 42}, got)
}

func TestAppEpochStake_SkipsZeroPower(t *testing.T) {
	rng := utils.TestRng()
	sk := ed25519.TestSecretKey(utils.GenBytes(rng, 32))
	app := newTestApp()
	for state := range app.state.Lock() {
		state.Validators = []abci.ValidatorUpdate{{
			PubKey: crypto.PubKeyToProto(sk.Public()),
			Power:  0,
		}}
	}
	got, err := appEpochStake{app: proxy.New(app)}.CommitteeWeights(1)
	require.NoError(t, err)
	require.Empty(t, got)
}
