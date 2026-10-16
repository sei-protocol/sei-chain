package keeper_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

var (
	sdFactory     = common.HexToAddress("0x00000000000000000000000000000000005d0001")
	sdPreexisting = common.HexToAddress("0x00000000000000000000000000000000005d0002")
)

func sdBeneficiary(t *testing.T) common.Address {
	_, a := keyAddrs(mustKey(t, anvilKey2Hex))
	return a
}

// runtime: SELFDESTRUCT(ben)
func selfdestructToCode(ben common.Address) []byte {
	return (&bc{}).push20(ben).op(vm.SELFDESTRUCT).bytes()
}

// runtime: SELFDESTRUCT(ADDRESS)
func selfdestructToSelfCode() []byte {
	return (&bc{}).op(vm.ADDRESS, vm.SELFDESTRUCT).bytes()
}

// factoryCode CREATEs childInit with CALLVALUE, stores its address in slot 0, CALLs it, stores success in slot 1.
func factoryCode(childInit []byte) []byte {
	build := func(off byte) *bc {
		b := &bc{}
		b.push1(byte(len(childInit))).push1(off).push1(0).op(vm.CODECOPY)
		b.push1(byte(len(childInit))).push1(0).op(vm.CALLVALUE, vm.CREATE)
		b.op(vm.DUP1).push1(0).op(vm.SSTORE)
		b.push1(0).push1(0).push1(0).push1(0).push1(0).op(vm.DUP6, vm.GAS, vm.CALL)
		b.push1(1).op(vm.SSTORE, vm.STOP)
		return b
	}
	off := build(0).pc()
	return append(build(byte(off)).bytes(), childInit...)
}

func (e *behaviorEnv) supply() sdk.Int {
	return e.k.BankKeeper().GetSupply(e.ctx, e.k.GetBaseDenom(e.ctx)).Amount
}

// TestSelfDestructBehavior pins EIP-6780 SELFDESTRUCT semantics through DeliverTx.
func TestSelfDestructBehavior(t *testing.T) {
	key := mustKey(t, anvilKey0Hex)

	t.Run("a_create_and_destroy_same_tx_via_factory", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		sender := e.associateAndFund(key, 1_000_000)
		ben := sdBeneficiary(t)
		childInit := initcodeFor(selfdestructToCode(ben), [2]byte{0, 0x2a})
		e.k.SetCode(e.ctx, sdFactory, factoryCode(childInit))
		child := crypto.CreateAddress(sdFactory, 0)
		supplyBefore := e.supply()

		value := bigUsei(5)
		r := e.runTx(key, e.legacyTx(0, &sdFactory, value, 500_000, bigGwei(1), nil), 0)
		require.Empty(t, r.res.VmError)
		require.Equal(t, uint64(1), e.slot(sdFactory, 1).Uint64(), "child call succeeded")
		require.Equal(t, child, common.BigToAddress(e.slot(sdFactory, 0)))

		require.Equal(t, value, e.balanceWei(ben), "beneficiary credited")
		require.Equal(t, int64(0), e.balanceWei(child).Int64())
		require.Empty(t, e.k.GetCode(e.ctx, child), "code cleared")
		require.Equal(t, common.Hash{}, e.k.GetCodeHash(e.ctx, child))
		require.Equal(t, int64(0), e.slot(child, 0).Int64(), "storage cleared")
		require.Equal(t, uint64(0), e.k.GetNonce(e.ctx, child), "nonce cleared")
		require.Equal(t, uint64(1), e.k.GetNonce(e.ctx, sdFactory), "factory nonce bumped by CREATE")
		require.Equal(t, uint64(1), e.k.GetNonce(e.ctx, sender))

		require.True(t, r.totalSurplus().IsZero(), "surplus %s", r.totalSurplus())
		require.True(t, supplyBefore.Equal(e.supply()))
		// 21000 + CREATE 32004 + child init 22130 + deposit 4400 + SELFDESTRUCT path 32603 + 2 SSTOREs + misc
		require.Equal(t, uint64(156498), r.res.GasUsed)
	})

	t.Run("a_constructor_selfdestruct_tx_level_create", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		sender := e.associateAndFund(key, 1_000_000)
		ben := sdBeneficiary(t)
		// initcode: SSTORE(0, 0x2a); SELFDESTRUCT(ben)
		initcode := (&bc{}).push1(0x2a).push1(0).op(vm.SSTORE).push20(ben).op(vm.SELFDESTRUCT).bytes()
		created := crypto.CreateAddress(sender, 0)

		value := bigUsei(5)
		r := e.runTx(key, e.legacyTx(0, nil, value, 500_000, bigGwei(1), initcode), 0)
		require.Empty(t, r.res.VmError)
		require.Equal(t, created.Hex(), r.receipt.ContractAddress)
		require.Equal(t, value, e.balanceWei(ben))
		require.Equal(t, int64(0), e.balanceWei(created).Int64())
		require.Empty(t, e.k.GetCode(e.ctx, created))
		require.Equal(t, common.Hash{}, e.k.GetCodeHash(e.ctx, created))
		require.Equal(t, int64(0), e.slot(created, 0).Int64(), "constructor storage cleared")
		require.Equal(t, uint64(0), e.k.GetNonce(e.ctx, created), "created-account nonce cleared")
		require.True(t, r.totalSurplus().IsZero())
		require.Equal(t, uint64(53000+408+2+6+22100+3+5000+2600+25000), r.res.GasUsed)
	})

	t.Run("b_preexisting_contract_only_moves_balance", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		e.associateAndFund(key, 1_000_000)
		ben := sdBeneficiary(t)
		code := selfdestructToCode(ben)
		e.k.SetCode(e.ctx, sdPreexisting, code)
		e.k.SetState(e.ctx, sdPreexisting, slotHash(0), common.BigToHash(big.NewInt(0x2a)))
		e.fundEVM(sdPreexisting, 7)

		r := e.runTx(key, e.legacyTx(0, &sdPreexisting, nil, 200_000, bigGwei(1), nil), 0)
		require.Empty(t, r.res.VmError)
		require.Equal(t, bigUsei(7), e.balanceWei(ben))
		require.Equal(t, int64(0), e.balanceWei(sdPreexisting).Int64())
		require.Equal(t, code, e.k.GetCode(e.ctx, sdPreexisting), "code retained")
		require.Equal(t, int64(0x2a), e.slot(sdPreexisting, 0).Int64(), "storage retained")
		require.True(t, r.totalSurplus().IsZero())
		require.Equal(t, uint64(21000+3+5000+2600+25000), r.res.GasUsed)
	})

	t.Run("c_new_contract_selfdestruct_to_self_burns_into_surplus", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		e.associateAndFund(key, 1_000_000)
		childInit := initcodeFor(selfdestructToSelfCode(), [2]byte{0, 0x2a})
		e.k.SetCode(e.ctx, sdFactory, factoryCode(childInit))
		child := crypto.CreateAddress(sdFactory, 0)
		supplyBefore := e.supply()

		value := bigUsei(5)
		r := e.runTx(key, e.legacyTx(0, &sdFactory, value, 500_000, bigGwei(1), nil), 0)
		require.Empty(t, r.res.VmError)
		require.Equal(t, uint64(1), e.slot(sdFactory, 1).Uint64())
		require.Equal(t, int64(0), e.balanceWei(child).Int64(), "balance burned")
		require.Empty(t, e.k.GetCode(e.ctx, child))
		require.Equal(t, int64(0), e.slot(child, 0).Int64())
		// burned value becomes surplus, not removed from bank supply
		require.Equal(t, value.String(), r.totalSurplus().String())
		require.Equal(t, mulU(500_000, bigGwei(1)).String(), r.anteSurplus.String())
		require.Equal(t, new(big.Int).Sub(value, mulU(500_000, bigGwei(1))).String(), r.execSurplus.String())
		require.True(t, supplyBefore.Equal(e.supply()), "bank supply unchanged by the burn")
		// factory case with 2-byte runtime and SELFDESTRUCT to self
		require.Equal(t, uint64(124889), r.res.GasUsed)
	})

	t.Run("d_prefunded_address_then_create_then_selfdestruct", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		e.associateAndFund(key, 1_000_000)
		ben := sdBeneficiary(t)
		childInit := initcodeFor(selfdestructToCode(ben), [2]byte{0, 0x2a})
		e.k.SetCode(e.ctx, sdFactory, factoryCode(childInit))
		child := crypto.CreateAddress(sdFactory, 0)
		e.fundEVM(child, 3) // prefund future CREATE address

		value := bigUsei(5)
		r := e.runTx(key, e.legacyTx(0, &sdFactory, value, 500_000, bigGwei(1), nil), 0)
		require.Empty(t, r.res.VmError)
		require.Equal(t, uint64(1), e.slot(sdFactory, 1).Uint64())
		require.Equal(t, bigUsei(8), e.balanceWei(ben), "prefund + endowment both go to beneficiary")
		require.Equal(t, int64(0), e.balanceWei(child).Int64())
		require.Empty(t, e.k.GetCode(e.ctx, child))
		require.Equal(t, int64(0), e.slot(child, 0).Int64())
		require.True(t, r.totalSurplus().IsZero())
		// same gas as the factory case
		require.Equal(t, uint64(156498), r.res.GasUsed)
	})
}
