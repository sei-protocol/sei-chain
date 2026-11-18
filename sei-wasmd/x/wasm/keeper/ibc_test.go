package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNonIBCContractHasNoPort(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	example := InstantiateHackatomExampleContract(t, ctx, keepers)
	require.Empty(t, keepers.WasmKeeper.GetContractInfo(ctx, example.Contract).IBCPortID)
}

func TestIBCContractPortOnInstantiate(t *testing.T) {
	ctx, keepers := CreateTestInput(t, false, SupportedFeatures)
	example := InstantiateIBCReflectContract(t, ctx, keepers)
	require.Equal(t, PortIDForContract(example.Contract), keepers.WasmKeeper.GetContractInfo(ctx, example.Contract).IBCPortID)

	initMsgBz := IBCReflectInitMsg{
		ReflectCodeID: example.ReflectCodeID,
	}.GetBytes(t)

	// create a second contract should give yet another portID (and different address)
	creator := RandomAccountAddress(t)
	addr, _, err := keepers.ContractKeeper.Instantiate(ctx, example.CodeID, creator, nil, initMsgBz, "ibc-reflect-2", nil)
	require.NoError(t, err)
	require.NotEqual(t, example.Contract, addr)

	portID2 := PortIDForContract(addr)
	require.Equal(t, portID2, keepers.WasmKeeper.GetContractInfo(ctx, addr).IBCPortID)
}
