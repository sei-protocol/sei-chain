package p2p

import (
	"fmt"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// appEpochStake reads the bonded set from the ABCI app after Commit.
// data snapshots only at LastRoad tips, when local execution of end(E)
// has just committed, so the tip stake it reads is Committee(end(E)).
type appEpochStake struct {
	app *proxy.Proxy
}

var _ data.StakeSource = appEpochStake{}

func (s appEpochStake) CommitteeWeights(_ atypes.GlobalBlockNumber) (map[atypes.PublicKey]uint64, error) {
	vals := s.app.GetValidators()
	weights := make(map[atypes.PublicKey]uint64, len(vals))
	for _, v := range vals {
		if v.Power <= 0 {
			continue
		}
		pk, err := crypto.PubKeyFromProto(v.PubKey)
		if err != nil {
			return nil, fmt.Errorf("PubKeyFromProto: %w", err)
		}
		apk, err := atypes.PublicKeyFromBytes(pk.Bytes())
		if err != nil {
			return nil, fmt.Errorf("PublicKeyFromBytes: %w", err)
		}
		power, ok := utils.SafeCast[uint64](v.Power)
		if !ok {
			return nil, fmt.Errorf("validator power %d does not fit uint64", v.Power)
		}
		weights[apk] = power
	}
	return weights, nil
}
