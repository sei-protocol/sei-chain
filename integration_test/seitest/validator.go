package seitest

import (
	"testing"

	"github.com/sei-protocol/sei-chain/integration_test/seitest/internal/chain"
)

// Validator is one node of a Validators network.
type Validator struct {
	t       *testing.T
	inner   *chain.Validator
	chainID string
}

// Moniker is the validator's name (node0, node1, ...).
func (v *Validator) Moniker() string { return v.inner.Moniker() }

// ChainID is the genesis chain id the network is running under.
func (v *Validator) ChainID() string { return v.chainID }

// Tendermint is the read facade over this validator's consensus and node
// state.
func (v *Validator) Tendermint() Tendermint { return Tendermint{t: v.t, v: v.inner} }
