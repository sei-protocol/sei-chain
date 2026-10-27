// Package seitest is the test-authoring DSL over the in-process multi-validator
// engine in internal/chain. It is the only public contract; internal/chain stays
// unreachable from outside this package tree.
//
// # Usage
//
//	validators := seitest.NewValidators(t, 4)
//	val1 := validators[0]
//	require.Len(t, val1.Tendermint().ValidatorSet(), 4)
package seitest
