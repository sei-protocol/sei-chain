package seitest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/integration_test/seitest/internal/chain"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// Tendermint is the read facade over one validator's consensus and node
// state.
type Tendermint struct {
	t *testing.T
	v *chain.Validator
}

// ValidatorSet is the live consensus validator set this node knows about.
func (tm Tendermint) ValidatorSet() []*tmtypes.Validator {
	tm.t.Helper()
	res, err := tm.v.LocalClient().Validators(tm.t.Context(), nil, nil, nil)
	require.NoError(tm.t, err, "tendermint: validator set")
	return res.Validators
}

// NodeInfo is this node's CometBFT status, including the network (chain id)
// it reports.
func (tm Tendermint) NodeInfo() tmtypes.NodeInfo {
	tm.t.Helper()
	res, err := tm.v.LocalClient().Status(tm.t.Context())
	require.NoError(tm.t, err, "tendermint: status")
	return res.NodeInfo
}
