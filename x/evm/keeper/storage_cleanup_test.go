package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestPruneZeroStorageSlots(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)

	addr1 := common.HexToAddress("0x0100000000000000000000000000000000000000")
	addr2 := common.HexToAddress("0x0200000000000000000000000000000000000000")

	zero := common.Hash{}
	nonZero := common.HexToHash("0x1234")
	slot1 := common.HexToHash("0x01")
	slot2 := common.HexToHash("0x02")
	slot3 := common.HexToHash("0x03")

	store1 := k.PrefixStore(ctx, types.StateKey(addr1))
	store1.Set(slot1[:], zero[:])
	store1.Set(slot2[:], nonZero[:])

	store2 := k.PrefixStore(ctx, types.StateKey(addr2))
	store2.Set(slot3[:], zero[:])

	processed, deleted := k.PruneZeroStorageSlots(ctx, 2)
	require.Equal(t, 2, processed)
	require.Equal(t, 1, deleted)

	store1 = k.PrefixStore(ctx, types.StateKey(addr1))
	require.False(t, store1.Has(slot1[:]))
	require.True(t, store1.Has(slot2[:]))

	expectedCheckpoint := append(addr1.Bytes(), slot2.Bytes()...)
	require.Equal(t, expectedCheckpoint, k.GetZeroStorageCleanupCheckpoint(ctx))

	processed, deleted = k.PruneZeroStorageSlots(ctx, 2)
	require.Equal(t, 1, processed)
	require.Equal(t, 1, deleted)

	store2 = k.PrefixStore(ctx, types.StateKey(addr2))
	require.False(t, store2.Has(slot3[:]))
	require.Nil(t, k.GetZeroStorageCleanupCheckpoint(ctx))

	processed, deleted = k.PruneZeroStorageSlots(ctx, evmkeeper.ZeroStorageCleanupBatchSize)
	require.Equal(t, 1, processed)
	require.Equal(t, 0, deleted)
}
