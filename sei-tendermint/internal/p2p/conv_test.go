package p2p

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/p2p/conn"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"testing"
)

func TestHandshakeMsgConv(t *testing.T) {
	rng := utils.TestRng()
	for range 5 {
		var challenge conn.Challenge
		utils.OrPanic1(rng.Read(challenge[:]))
		key := makeKey(rng)
		msg := &handshakeMsg{
			NodeAuth: key.SignChallenge(challenge),
			handshakeSpec: handshakeSpec{
				SelfAddr:          utils.Some(makeAddrFor(rng, key.Public().NodeID())),
				PexAddrs:          utils.GenSlice(rng, makeAddr),
				SeiGigaConnection: utils.GenBool(rng),
			},
		}
		require.NoError(t, handshakeMsgConv.Test(msg))
	}
}
