package memblock_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore/db/blocktest"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore/db/memblock"
	blocktypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore/db/types"
)

// TestBlockDBContract runs the shared storage contract against memblock. One
// instance backs every reopen, so an in-memory "restart" preserves records
// exactly as a durable reopen would.
func TestBlockDBContract(t *testing.T) {
	blocktest.RunContract(t, func(t *testing.T) blocktest.Open {
		db := memblock.NewBlockDB()
		return func() (blocktypes.BlockDB, error) { return db, nil }
	})
}
