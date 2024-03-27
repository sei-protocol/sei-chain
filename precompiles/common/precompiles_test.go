package common_test

import (
	"math/big"
	"testing"

	"github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/stretchr/testify/require"
)

func TestAssertArgsLength(t *testing.T) {
	require.NotPanics(t, func() { common.AssertArgsLength(nil, 0) })
	require.NotPanics(t, func() { common.AssertArgsLength([]interface{}{1, ""}, 2) })
	require.Panics(t, func() { common.AssertArgsLength([]interface{}{""}, 2) })
}

func TestAssertNonPayable(t *testing.T) {
	require.NotPanics(t, func() { common.AssertNonPayable(nil) })
	require.NotPanics(t, func() { common.AssertNonPayable(big.NewInt(0)) })
	require.Panics(t, func() { common.AssertNonPayable(big.NewInt(1)) })
}
