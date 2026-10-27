package seitest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/integration_test/seitest/internal/chain"
)

const bringUpTimeout = 60 * time.Second

// Validators is a network's validator set, in bring-up order (validators[0]
// is node0).
type Validators []*Validator

// NewValidators brings up an n-validator in-process network and returns once
// every validator is producing blocks. It registers the network's teardown
// with t.Cleanup and fails t outright on any bring-up error, so a test body
// never has to check one.
func NewValidators(t *testing.T, n int) Validators {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), bringUpTimeout)
	defer cancel()

	c, err := chain.Start(ctx, chain.Config{Validators: n})
	require.NoError(t, err, "seitest: start chain")
	t.Cleanup(func() {
		require.NoError(t, c.Close(), "seitest: close chain")
	})

	require.NoError(t, c.WaitReady(ctx), "seitest: wait ready")

	validators := make(Validators, c.Validators())
	for i := range validators {
		validators[i] = &Validator{t: t, inner: c.Validator(i), chainID: c.ChainID()}
	}
	return validators
}
