package keeper_test

import (
	"encoding/binary"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	"github.com/stretchr/testify/require"
)

func TestBlockedAddr(t *testing.T) {
	k := keeper.NewBaseSendKeeper(nil, nil, nil, paramtypes.Subspace{}, map[string]bool{})

	// A coinbase address is the CoinbaseAddressPrefix followed by an 8-byte
	// big-endian tx index. Such addresses must be blocked from receiving funds.
	coinbaseAddr := func(txIndex uint64) sdk.AccAddress {
		idx := make([]byte, 8)
		binary.BigEndian.PutUint64(idx, txIndex)
		return sdk.AccAddress(append(keeper.CoinbaseAddressPrefix, idx...))
	}

	addr := coinbaseAddr(5)
	require.True(t, k.BlockedAddr(addr), "coinbase address should be blocked")

	// Mutating any prefix byte breaks the coinbase pattern, so the address
	// should no longer be blocked.
	addr[0] = 'q'
	require.False(t, k.BlockedAddr(addr), "non-coinbase address should not be blocked")
}
