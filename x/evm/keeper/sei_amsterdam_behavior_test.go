package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

var (
	saRecipient = common.HexToAddress("0x00000000000000000000000000000000005a0001")
	saContract  = common.HexToAddress("0x00000000000000000000000000000000005a0002")
)

// TestSeiAmsterdamBehavior pins the Osaka and Sei Amsterdam EIPs the v6.9 binary activates.
func TestSeiAmsterdamBehavior(t *testing.T) {
	key := mustKey(t, anvilKey0Hex)

	t.Run("eip7708_value_transfer_logs", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		sender := e.associateAndFund(key, 1_000_000)
		value := bigUsei(3)

		r := e.runTx(key, e.legacyTx(0, &saRecipient, value, 21_000, bigGwei(1), nil), 0)
		require.Empty(t, r.res.VmError)
		require.Equal(t, uint64(21_000), r.res.GasUsed)
		want := ethtypes.EthTransferLog(sender, saRecipient, uint256.MustFromBig(value))
		require.Len(t, r.receipt.Logs, 1)
		l := r.receipt.Logs[0]
		require.Equal(t, want.Address.Hex(), l.Address)
		require.Equal(t, []string{want.Topics[0].Hex(), want.Topics[1].Hex(), want.Topics[2].Hex()}, l.Topics)
		require.Equal(t, want.Data, l.Data)
	})

	t.Run("eip7708_zero_value_no_log", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		e.associateAndFund(key, 1_000_000)

		r := e.runTx(key, e.legacyTx(0, &saRecipient, nil, 21_000, bigGwei(1), nil), 0)
		require.Empty(t, r.res.VmError)
		require.Empty(t, r.receipt.Logs)
	})

	t.Run("eip7939_clz", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		e.associateAndFund(key, 1_000_000)
		// SSTORE(0, CLZ(1))
		e.k.SetCode(e.ctx, saContract, (&bc{}).push1(1).op(vm.CLZ).push1(0).op(vm.SSTORE, vm.STOP).bytes())

		r := e.runTx(key, e.legacyTx(0, &saContract, nil, 100_000, bigGwei(1), nil), 0)
		require.Empty(t, r.res.VmError)
		require.Equal(t, int64(255), e.slot(saContract, 0).Int64())
	})

	t.Run("eip8024_dupn", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		e.associateAndFund(key, 1_000_000)
		// push 1..17, DUPN 17 (immediate 0x80), SSTORE(0, 1)
		code := &bc{}
		for i := byte(1); i <= 17; i++ {
			code.push1(i)
		}
		code.op(vm.DUPN).code = append(code.code, 0x80)
		code.push1(0).op(vm.SSTORE, vm.STOP)
		e.k.SetCode(e.ctx, saContract, code.bytes())

		r := e.runTx(key, e.legacyTx(0, &saContract, nil, 100_000, bigGwei(1), nil), 0)
		require.Empty(t, r.res.VmError)
		require.Equal(t, int64(1), e.slot(saContract, 0).Int64())
	})

	t.Run("eip7954_code_size_limit", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			size    uint16
			vmError string
		}{
			{name: "at_limit", size: 0xc000},
			{name: "over_limit", size: 0xc001, vmError: "max code size exceeded: code size 49153 limit 49152"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				e := newBehaviorEnv(t, false)
				sender := e.associateAndFund(key, 1_000_000)
				// initcode: RETURN(0, size)
				initcode := (&bc{}).push2(tc.size).push1(0).op(vm.RETURN).bytes()

				r := e.runTx(key, e.legacyTx(0, nil, nil, 12_000_000, bigGwei(1), initcode), 0)
				require.Equal(t, tc.vmError, r.res.VmError)
				code := e.k.GetCode(e.ctx, crypto.CreateAddress(sender, 0))
				if tc.vmError == "" {
					require.Len(t, code, int(tc.size))
				} else {
					require.Empty(t, code)
				}
			})
		}
	})

}
