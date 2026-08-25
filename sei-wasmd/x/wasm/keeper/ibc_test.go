package keeper

import (
	"fmt"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/assert"

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

func TestContractFromPortID(t *testing.T) {
	contractAddr := BuildContractAddress(1, 100)
	specs := map[string]struct {
		srcPort string
		expAddr sdk.AccAddress
		expErr  bool
	}{
		"all good": {
			srcPort: fmt.Sprintf("wasm.%s", contractAddr.String()),
			expAddr: contractAddr,
		},
		"without prefix": {
			srcPort: contractAddr.String(),
			expErr:  true,
		},
		"invalid prefix": {
			srcPort: fmt.Sprintf("wasmx.%s", contractAddr.String()),
			expErr:  true,
		},
		"without separator char": {
			srcPort: fmt.Sprintf("wasm%s", contractAddr.String()),
			expErr:  true,
		},
		"invalid account": {
			srcPort: "wasm.foobar",
			expErr:  true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			gotAddr, gotErr := ContractFromPortID(spec.srcPort)
			if spec.expErr {
				require.Error(t, gotErr)
				return
			}
			require.NoError(t, gotErr)
			assert.Equal(t, spec.expAddr, gotAddr)
		})
	}
}
