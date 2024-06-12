package common_test

import (
	"errors"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/common"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
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

func TestHandlePrecompileError(t *testing.T) {
	_, evmAddr := testkeeper.MockAddressPair()
	k, ctx := testkeeper.MockEVMKeeper()
	stateDB := state.NewDBImpl(ctx, k, false)
	evm := &vm.EVM{StateDB: stateDB}

	// assert no panic under various conditions
	common.HandlePrecompileError(nil, evm, "no_error")
	common.HandlePrecompileError(types.NewAssociationMissingErr(evmAddr.Hex()), evm, "association")
	common.HandlePrecompileError(errors.New("other error"), evm, "other")
}
