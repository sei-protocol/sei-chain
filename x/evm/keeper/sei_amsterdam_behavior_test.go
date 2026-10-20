package keeper_test

import (
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

var (
	p256VerifyAddr = common.BytesToAddress([]byte{0x01, 0x00})
	modExpAddr     = common.BytesToAddress([]byte{0x05})
	saRecipient    = common.HexToAddress("0x00000000000000000000000000000000005a0001")
	saContract     = common.HexToAddress("0x00000000000000000000000000000000005a0002")
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
			size    int64
			vmError string
		}{
			{name: "at_limit", size: params.MaxCodeSizeAmsterdam},
			{name: "over_limit", size: params.MaxCodeSizeAmsterdam + 1, vmError: "max code size exceeded: code size 65537 limit 65536"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				e := newBehaviorEnv(t, false)
				sender := e.associateAndFund(key, 1_000_000)
				// initcode: RETURN(0, size)
				initcode := (&bc{}).push32(big.NewInt(tc.size)).push1(0).op(vm.RETURN).bytes()

				r := e.runTx(key, e.legacyTx(0, nil, nil, 16_000_000, bigGwei(1), initcode), 0)
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

	t.Run("eip7951_p256verify", func(t *testing.T) {
		// CallP256Verify from go-ethereum's core/vm/testdata/precompiles/p256Verify.json.
		valid := mustHex(t, "4cee90eb86eaa050036147a12d49004b6b9c72bd725d39d4785011fe190f0b4da73bd4903f0ce3b639bbbf6e8e80d16931ff4bcf5993d58468e8fb19086e8cac36dbcd03009df8c59286b162af3bd7fcc0450c9aa81be5d10d312af6c66b1d604aebd3099c618202fcfe16ae7770b0c49ab5eadf74b754204a3bb6060e44eff37618b065f9832de4ca6ca971a7a1adc826d0f7c00181a5fb2ddf79ae00b4e10e")
		tampered := append([]byte{}, valid...)
		tampered[0] ^= 0x01
		for _, tc := range []struct {
			name    string
			input   []byte
			retSize int64
			word    int64
		}{
			{name: "valid_signature", input: valid, retSize: 32, word: 1},
			{name: "invalid_signature", input: tampered},
		} {
			t.Run(tc.name, func(t *testing.T) {
				e := newBehaviorEnv(t, true)
				e.associateAndFund(key, 1_000_000)
				p := e.probePrecompile(key, p256VerifyAddr, tc.input)
				require.True(t, p.ok)
				require.Equal(t, uint64(params.P256VerifyGas), p.gas)
				require.Equal(t, tc.retSize, p.retSize)
				require.Equal(t, tc.word, p.word.Int64())
			})
		}
	})

	t.Run("eip7883_modexp_repricing", func(t *testing.T) {
		e := newBehaviorEnv(t, true)
		e.associateAndFund(key, 1_000_000)
		// 3^5 mod 7 = 5; Osaka charges the 500 minimum where Berlin charged 200.
		p := e.probePrecompile(key, modExpAddr, modExpInput([]byte{3}, []byte{5}, []byte{7}))
		require.True(t, p.ok)
		require.Equal(t, uint64(500), p.gas)
		require.Equal(t, int64(1), p.retSize)
		require.Equal(t, byte(5), p.word.FillBytes(make([]byte, 32))[0])
	})

	t.Run("eip7823_modexp_length_bound", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			baseLen int
			ok      bool
		}{
			{name: "at_limit", baseLen: 1024, ok: true},
			{name: "over_limit", baseLen: 1025},
		} {
			t.Run(tc.name, func(t *testing.T) {
				e := newBehaviorEnv(t, true)
				e.associateAndFund(key, 1_000_000)
				base := make([]byte, tc.baseLen)
				base[len(base)-1] = 3
				p := e.probePrecompile(key, modExpAddr, modExpInput(base, []byte{5}, []byte{7}))
				require.Equal(t, tc.ok, p.ok)
			})
		}
	})
}

// precompileProbe is what probeCode records about one STATICCALL to a precompile.
type precompileProbe struct {
	gas     uint64
	ok      bool
	word    *big.Int
	retSize int64
}

// probeCallOverhead is the gas probeCode spends between its two GAS readings besides the precompile itself.
const probeCallOverhead = 3 + 2 + 2 + 3 + 3 + 3 + params.WarmStorageReadCostEIP2929 + 2

// probeCallGas is the gas probeCode forwards to the precompile.
const probeCallGas = 200_000

// probeCode returns runtime code that STATICCALLs target with its calldata and stores the precompile's gas
// in slot 0, the call's success in slot 1, the first return word in slot 2 and the return size in slot 3.
func probeCode(target common.Address) []byte {
	return (&bc{}).
		// CALLDATACOPY(0x20, 0, CALLDATASIZE), so the call below expands no memory.
		op(vm.CALLDATASIZE, vm.PUSH0).push1(0x20).op(vm.CALLDATACOPY).
		op(vm.GAS).
		// STATICCALL(probeCallGas, target, 0x20, CALLDATASIZE, 0, 0x20)
		push1(0x20).op(vm.PUSH0, vm.CALLDATASIZE).push1(0x20).push20(target).push32(big.NewInt(probeCallGas)).op(vm.STATICCALL).
		op(vm.GAS).
		op(vm.SWAP1).push1(1).op(vm.SSTORE).
		op(vm.SWAP1, vm.SUB).push1(byte(probeCallOverhead)).op(vm.SWAP1, vm.SUB, vm.PUSH0, vm.SSTORE).
		op(vm.PUSH0, vm.MLOAD).push1(2).op(vm.SSTORE).
		op(vm.RETURNDATASIZE).push1(3).op(vm.SSTORE, vm.STOP).
		bytes()
}

// probePrecompile calls target with input from a probe contract in a delivered transaction.
func (e *behaviorEnv) probePrecompile(key *ecdsa.PrivateKey, target common.Address, input []byte) precompileProbe {
	e.k.SetCode(e.ctx, saContract, probeCode(target))
	r := e.runTx(key, e.legacyTx(0, &saContract, nil, 1_000_000, bigGwei(1), input), 0)
	require.Empty(e.t, r.res.VmError)
	return precompileProbe{
		gas:     e.slot(saContract, 0).Uint64(),
		ok:      e.slot(saContract, 1).Sign() != 0,
		word:    e.slot(saContract, 2),
		retSize: e.slot(saContract, 3).Int64(),
	}
}

// modExpInput encodes MODEXP calldata for base^exp mod mod.
func modExpInput(base, exp, mod []byte) []byte {
	var in []byte
	for _, b := range [][]byte{base, exp, mod} {
		in = append(in, common.BigToHash(big.NewInt(int64(len(b)))).Bytes()...)
	}
	in = append(in, base...)
	in = append(in, exp...)
	return append(in, mod...)
}

func mustHex(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}
