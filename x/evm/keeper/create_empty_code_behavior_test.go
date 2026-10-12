package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// requireEmptyCodeStored asserts empty code keys and the Sei address association exist for addr.
func requireEmptyCodeStored(t *testing.T, e *behaviorEnv, addr common.Address) {
	t.Helper()
	require.True(t, e.k.PrefixStore(e.ctx, types.CodeKeyPrefix).Has(addr[:]), "code key")
	require.True(t, e.k.PrefixStore(e.ctx, types.CodeSizeKeyPrefix).Has(addr[:]), "code size key")
	require.True(t, e.k.PrefixStore(e.ctx, types.CodeHashKeyPrefix).Has(addr[:]), "code hash key")
	require.Equal(t, ethtypes.EmptyCodeHash, e.k.GetCodeHash(e.ctx, addr))
	require.Equal(t, 0, e.k.GetCodeSize(e.ctx, addr))
	_, associated := e.k.GetSeiAddress(e.ctx, addr)
	require.True(t, associated, "sei address association")
	require.Equal(t, uint64(1), e.k.GetNonce(e.ctx, addr))
}

// TestCreateEmptyCodeBehavior pins that a CREATE returning empty code still stores code keys and the address association.
func TestCreateEmptyCodeBehavior(t *testing.T) {
	key := mustKey(t, anvilKey0Hex)

	t.Run("tx_level_create", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		sender := e.associateAndFund(key, 1_000_000)
		created := crypto.CreateAddress(sender, 0)
		_, associated := e.k.GetSeiAddress(e.ctx, created)
		require.False(t, associated)

		r := e.runTx(key, e.legacyTx(0, nil, nil, 200_000, bigGwei(1), []byte{byte(vm.STOP)}), 0)
		require.Empty(t, r.res.VmError)
		require.Equal(t, created.Hex(), r.receipt.ContractAddress)
		requireEmptyCodeStored(t, e, created)
	})

	t.Run("create_opcode", func(t *testing.T) {
		e := newBehaviorEnv(t, false)
		e.associateAndFund(key, 1_000_000)
		e.k.SetCode(e.ctx, sdFactory, factoryCode([]byte{byte(vm.STOP)}))
		child := crypto.CreateAddress(sdFactory, 0)

		r := e.runTx(key, e.legacyTx(0, &sdFactory, nil, 500_000, bigGwei(1), nil), 0)
		require.Empty(t, r.res.VmError)
		require.Equal(t, child, common.BigToAddress(e.slot(sdFactory, 0)))
		requireEmptyCodeStored(t, e, child)
	})
}
