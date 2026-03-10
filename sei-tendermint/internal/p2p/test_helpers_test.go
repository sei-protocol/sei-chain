package p2p

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func makeKey(rng utils.Rng) NodeSecretKey {
	return NodeSecretKey(ed25519.TestSecretKey(utils.GenBytes(rng, 32)))
}

func makeNodeID(rng utils.Rng) types.NodeID {
	return makeKey(rng).Public().NodeID()
}

func makeAddrFor(rng utils.Rng, id types.NodeID) NodeAddress {
	return NodeAddress{
		NodeID:   id,
		Hostname: fmt.Sprintf("%s.example.com", utils.GenString(rng, 10)),
		Port:     uint16(rng.Int()),
	}
}

func makeAddr(rng utils.Rng) NodeAddress {
	return makeAddrFor(rng, makeNodeID(rng))
}
