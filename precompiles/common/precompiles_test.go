package common_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/stretchr/testify/require"
)

func TestAssertArgsLength(t *testing.T) {
	require.NotPanics(t, func() { common.AssertArgsLength(nil, 0) })
	require.NotPanics(t, func() { common.AssertArgsLength([]interface{}{1, ""}, 2) })
	require.Panics(t, func() { common.AssertArgsLength([]interface{}{""}, 2) })
}
