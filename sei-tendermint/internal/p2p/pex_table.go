package p2p

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

type pexTable struct {
	initial []NodeAddress
	bootstrap []NodeAddress
	bySender map[types.NodeID][]NodeAddress
	cleared [][]NodeAddress
}

func (t *pexTable) Empty() bool {
	return len(t.initial)==0 && len(t.bootstrap)==0 && len(t.bySender) == 0 && len(t.cleared) == 0
}

func (t *pexTable) Pop() [][]NodeAddress {
	// If there are initial addresses, then return just them so that they are prioritized.
	if len(t.initial)>0 {
		out := utils.Slice(t.initial)
		t.initial = nil
		return out
	}
	// Otherwise just aggregate everything
	out := t.cleared
	if len(t.bootstrap)>0 {
		out = append(out,t.bootstrap)
	}
	for _,addrs := range t.bySender {
		out = append(out, addrs)
	}
	// Clear the table.
	*t = pexTable{}
	return out
}
