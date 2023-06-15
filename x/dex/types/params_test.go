package types_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestParamsValidate(t *testing.T) {
	p := types.Params{PriceSnapshotRetention: 0}
	require.Error(t, p.Validate())
}
