package state_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

func benchAddrs(n int) []common.Address {
	addrs := make([]common.Address, n)
	for i := range addrs {
		_, addrs[i] = testkeeper.MockAddressPair()
	}
	return addrs
}

func BenchmarkMarkAccount(b *testing.B) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	statedb := state.NewDBImpl(ctx, k, false)
	addrs := benchAddrs(64)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		statedb.MarkAccount(addrs[i%len(addrs)], state.AccountCreated)
		i++
	}
}

func BenchmarkHasSelfDestructed(b *testing.B) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	statedb := state.NewDBImpl(ctx, k, false)
	addrs := benchAddrs(64)
	for _, a := range addrs[:32] {
		statedb.MarkAccount(a, state.AccountDeleted)
	}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		statedb.HasSelfDestructed(addrs[i%len(addrs)])
		i++
	}
}
