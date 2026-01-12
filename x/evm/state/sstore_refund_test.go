package state_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/program"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

func TestSstoreRefundResetToOriginalInexistentSlot(t *testing.T) {
	code := program.New().
		Push(1).Push(0).Op(vm.SSTORE). // 0 -> 1
		Push(0).Push(0).Op(vm.SSTORE). // 1 -> 0 (reset to original)
		Op(vm.STOP).
		Bytes()

	t.Run("default-sstore-gas", func(t *testing.T) {
		cfg := &runtime.Config{
			ChainConfig: params.AllEthashProtocolChanges,
			GasLimit:    10_000_000,
		}
		_, statedb, err := runtime.Execute(code, nil, cfg)
		require.NoError(t, err)
		want := params.SstoreSetGasEIP2200 - params.WarmStorageReadCostEIP2929
		require.Equal(t, want, statedb.GetRefund())
	})

	t.Run("custom-sstore-gas", func(t *testing.T) {
		custom := uint64(72000)
		chainCfg := *params.AllEthashProtocolChanges
		chainCfg.SeiSstoreSetGasEIP2200 = &custom
		cfg := &runtime.Config{
			ChainConfig: &chainCfg,
			GasLimit:    10_000_000,
		}
		_, statedb, err := runtime.Execute(code, nil, cfg)
		require.NoError(t, err)
		want := custom - params.WarmStorageReadCostEIP2929
		require.Equal(t, want, statedb.GetRefund())
	})
}
