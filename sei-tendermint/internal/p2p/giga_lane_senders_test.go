package p2p

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// senderOf is a stand-in for signature recovery: each transaction's sender is its first byte.
func senderOf(tx []byte) common.Address {
	return common.BytesToAddress(tx[:1])
}

func fakeRecoverSenders(_ context.Context, txs [][]byte) []utils.Option[common.Address] {
	out := make([]utils.Option[common.Address], len(txs))
	for i, tx := range txs {
		out[i] = utils.Some(senderOf(tx))
	}
	return out
}

func globalBlockForTesting(rng utils.Rng) *atypes.GlobalBlock {
	b := atypes.GenBlock(rng)
	return &atypes.GlobalBlock{Header: b.Header(), Payload: b.Payload()}
}

func TestSuperblockSendersAlignsRecoveredAndMissingBlocks(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	store := newLaneSenderStore()
	go store.run(ctx, fakeRecoverSenders)
	r := &gigaRouterCommon{}
	r.laneSenders.Store(store)

	recovered := globalBlockForTesting(rng)
	missing := globalBlockForTesting(rng)
	store.add(recovered.Header.Hash(), recovered.Payload.Txs())

	got, err := r.superblockSenders(ctx, []*atypes.GlobalBlock{recovered, missing})
	require.NoError(t, err)
	require.Len(t, got, len(recovered.Payload.Txs())+len(missing.Payload.Txs()))
	for i, tx := range recovered.Payload.Txs() {
		require.Equal(t, utils.Some(senderOf(tx)), got[i])
	}
	for i := range missing.Payload.Txs() {
		require.False(t, got[len(recovered.Payload.Txs())+i].IsPresent())
	}

	// A block's senders are handed out once.
	_, ok := store.take(recovered.Header.Hash())
	require.False(t, ok)
}

func TestSuperblockSendersIsNilWithoutRecovery(t *testing.T) {
	rng := utils.TestRng()
	blocks := []*atypes.GlobalBlock{globalBlockForTesting(rng)}

	got, err := (&gigaRouterCommon{}).superblockSenders(t.Context(), blocks)
	require.NoError(t, err)
	require.Nil(t, got)

	r := &gigaRouterCommon{}
	r.laneSenders.Store(newLaneSenderStore())
	got, err = r.superblockSenders(t.Context(), blocks)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestLaneSenderStoreKeepsTheNewestGenerations(t *testing.T) {
	rng := utils.TestRng()
	store := newLaneSenderStore()
	first := atypes.GenBlockHeaderHash(rng)
	store.add(first, nil)
	for range 2 * laneSenderStoreCap {
		store.add(atypes.GenBlockHeaderHash(rng), nil)
	}
	_, ok := store.take(first)
	require.False(t, ok)

	last := atypes.GenBlockHeaderHash(rng)
	store.add(last, nil)
	_, ok = store.take(last)
	require.True(t, ok)
}
