package common_test

import (
	"math/big"
	"testing"

	"github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/stretchr/testify/require"
)

func TestValidateArgsLength(t *testing.T) {
	err := common.ValidateArgsLength(nil, 0)
	require.Nil(t, err)
	err = common.ValidateArgsLength([]interface{}{1, ""}, 2)
	require.Nil(t, err)
	err = common.ValidateArgsLength([]interface{}{""}, 2)
	require.NotNil(t, err)
}

func TestValidteNonPayable(t *testing.T) {
	err := common.ValidateNonPayable(nil)
	require.Nil(t, err)
	err = common.ValidateNonPayable(big.NewInt(0))
	require.Nil(t, err)
	err = common.ValidateNonPayable(big.NewInt(1))
	require.NotNil(t, err)
}
