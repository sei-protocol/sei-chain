package keeper_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

var (
	vmErrLoop      = common.HexToAddress("0x00000000000000000000000000000000000e7701")
	vmErrCaller    = common.HexToAddress("0x00000000000000000000000000000000000e7702")
	vmErrRecipient = common.HexToAddress("0x00000000000000000000000000000000000e7703")
)

// TestVMErrorTextBehavior pins the VmError text of failed txs; VmError is part of LastResultsHash.
func TestVMErrorTextBehavior(t *testing.T) {
	key := mustKey(t, anvilKey0Hex)
	// runtime: loop forever
	loop := (&bc{}).op(vm.JUMPDEST).push1(0).op(vm.JUMP).bytes()
	// runtime: CALL(GAS, recipient, 1 wei, 0, 0, 0, 0)
	callValue := (&bc{}).push1(0).push1(0).push1(0).push1(0).push1(1).push20(vmErrRecipient).op(vm.GAS, vm.CALL).bytes()
	// initcode: RETURN(0, 65537), one byte over the EIP-7954 code size limit
	tooLarge := (&bc{}).push32(big.NewInt(params.MaxCodeSizeAmsterdam + 1)).push1(0).op(vm.RETURN).bytes()
	// initcode: RETURN 0xEF
	efCode := (&bc{}).push1(0xef).push1(0).op(vm.MSTORE8).push1(1).push1(0).op(vm.RETURN).bytes()

	for _, tc := range []struct {
		name string
		to   *common.Address
		gas  uint64
		data []byte
		want string
	}{
		{name: "execution_out_of_gas", to: &vmErrLoop, gas: 30_000, want: "out of gas"},
		// go-ethereum v1.17 wraps a failed dynamic gas charge
		{name: "call_dynamic_gas_out_of_gas", to: &vmErrCaller, gas: 25_000, want: "out of gas: out of gas"},
		{name: "create_out_of_gas", gas: 80_000, data: loop, want: "out of gas"},
		// go-ethereum v1.17 adds the size suffix
		{name: "create_code_too_large", gas: 16_000_000, data: tooLarge, want: "max code size exceeded: code size 65537 limit 65536"},
		// code deposit gas is charged before the size check
		{name: "create_code_too_large_low_gas", gas: 200_000, data: tooLarge, want: "contract creation code storage out of gas"},
		{name: "create_ef_prefix", gas: 100_000, data: efCode, want: "invalid code: must not begin with 0xef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newBehaviorEnv(t, false)
			e.associateAndFund(key, 1_000_000)
			e.k.SetCode(e.ctx, vmErrLoop, loop)
			e.k.SetCode(e.ctx, vmErrCaller, callValue)
			e.fundEVM(vmErrCaller, 1)

			r := e.runTx(key, e.legacyTx(0, tc.to, nil, tc.gas, bigGwei(1), tc.data), 0)
			require.Equal(t, tc.want, r.res.VmError)
			require.Equal(t, tc.gas, r.res.GasUsed)
			require.Equal(t, uint32(0), r.receipt.Status)
		})
	}
}
