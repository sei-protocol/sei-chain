package scenarios

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSenderForKeepsABlockSendersDistinct pins the rule the pool size is validated against: a block
// reserves a contiguous range of indices, so no two of its transactions may draw one sender. A
// repeat would mean two transactions carrying a nonce only one of them can spend.
func TestSenderForKeepsABlockSendersDistinct(t *testing.T) {
	cfg := Config{TxsPerBlock: 64, Accounts: 64}
	for base := uint64(0); base < 500; base += uint64(cfg.TxsPerBlock) { //nolint:gosec // small test sizes
		seen := make(map[uint64]struct{}, cfg.TxsPerBlock)
		for i := range cfg.TxsPerBlock {
			slot := cfg.senderFor(base + uint64(i)) //nolint:gosec // small test indices
			_, repeated := seen[slot.account]
			require.False(t, repeated, "block at base %d draws account %d twice", base, slot.account)
			seen[slot.account] = struct{}{}
		}
	}
}

// TestSenderForAdvancesNoncesInOrder pins that an account's nonces rise by one each time it comes
// around, in the order blocks are built. A gap or a repeat is a transaction the chain rejects.
func TestSenderForAdvancesNoncesInOrder(t *testing.T) {
	cfg := Config{TxsPerBlock: 8, Accounts: 32}
	next := make(map[uint64]uint64)
	for index := uint64(0); index < 32*10; index++ {
		slot := cfg.senderFor(index)
		require.Equal(t, next[slot.account], slot.nonce,
			"account %d received nonce %d out of order", slot.account, slot.nonce)
		next[slot.account]++
	}
}

// TestSenderForWithoutAPoolMintsAFreshAccount pins the unpooled behaviour, where every transaction
// is the first and only one its sender ever makes.
func TestSenderForWithoutAPoolMintsAFreshAccount(t *testing.T) {
	cfg := Config{TxsPerBlock: 8}
	for index := uint64(0); index < 100; index++ {
		slot := cfg.senderFor(index)
		require.Equal(t, index, slot.account)
		require.Zero(t, slot.nonce)
	}
}
