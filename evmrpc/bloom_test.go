package evmrpc_test

import (
	"encoding/binary"
	"encoding/hex"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestMatchBloom(t *testing.T) {
	log := ethtypes.Log{
		Address: common.HexToAddress("0x797C2dBE5736D0096914Cd1f9A7330403c71d301"),
		Topics:  []common.Hash{common.HexToHash("0x036285defb58e7bdfda894dd4f86e1c7c826522ae0755f0017a2155b4c58022e")},
	}
	bloom := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: []*ethtypes.Log{&log}}})
	require.Equal(t,
		"00000000001000000000000000000020000000000000000000000000000000000000000000000000000000000000000000000000000000000400000000000000000000000000000000000000000000000000000000000000000000000000000008000000000000000000000000000400000000000000000000000000000000000000000000000004000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		hex.EncodeToString(bloom[:]),
	)
	filters := evmrpc.EncodeFilters(
		[]common.Address{common.HexToAddress("0x797C2dBE5736D0096914Cd1f9A7330403c71d301")},
		[][]common.Hash{{common.HexToHash("0x036285defb58e7bdfda894dd4f86e1c7c826522ae0755f0017a2155b4c58022e")}},
	)
	require.True(t, evmrpc.MatchFilters(bloom, filters))

	filters = evmrpc.EncodeFilters(
		[]common.Address{common.HexToAddress("0x797C2dBE5736D0096914Cd1f9A7330403c71d301")},
		[][]common.Hash{},
	)
	require.True(t, evmrpc.MatchFilters(bloom, filters))

	filters = evmrpc.EncodeFilters(
		[]common.Address{},
		[][]common.Hash{{common.HexToHash("0x036285defb58e7bdfda894dd4f86e1c7c826522ae0755f0017a2155b4c58022e")}},
	)
	require.True(t, evmrpc.MatchFilters(bloom, filters))

	filters = evmrpc.EncodeFilters(
		[]common.Address{common.HexToAddress("0x797C2dBE5736D0096914Cd1f9A7330403c71d302")},
		[][]common.Hash{},
	)
	require.False(t, evmrpc.MatchFilters(bloom, filters))

	filters = evmrpc.EncodeFilters(
		[]common.Address{},
		[][]common.Hash{{common.HexToHash("0x036285defb58e7bdfda894dd4f86e1c7c826522ae0755f0017a2155b4c58022f")}},
	)
	require.False(t, evmrpc.MatchFilters(bloom, filters))
}

func TestMatchFiltersDeterministic(t *testing.T) {
	log := ethtypes.Log{
		Address: common.HexToAddress("0x797C2dBE5736D0096914Cd1f9A7330403c71d301"),
		Topics:  []common.Hash{common.HexToHash("0x036285defb58e7bdfda894dd4f86e1c7c826522ae0755f0017a2155b4c58022e")},
	}
	bloom := ethtypes.CreateBloom(ethtypes.Receipts{&ethtypes.Receipt{Logs: []*ethtypes.Log{&log}}})
	filters := evmrpc.EncodeFilters(
		[]common.Address{common.HexToAddress("0x797C2dBE5736D0096914Cd1f9A7330403c71d301")},
		[][]common.Hash{{common.HexToHash("0x036285defb58e7bdfda894dd4f86e1c7c826522ae0755f0017a2155b4c58022e")}},
	)
	expected := evmrpc.MatchFilters(bloom, filters)

	const runs = 100
	var wg sync.WaitGroup
	wg.Add(runs)
	for i := 0; i < runs; i++ {
		go func() {
			defer wg.Done()
			require.Equal(t, expected, evmrpc.MatchFilters(bloom, filters))
		}()
	}
	wg.Wait()
}

func BenchmarkMatchFilters(b *testing.B) {
	const num = 1000
	addresses := make([]common.Address, num)
	for i := 0; i < num; i++ {
		var buf [20]byte
		binary.BigEndian.PutUint32(buf[16:], uint32(i))
		addresses[i] = common.BytesToAddress(buf[:])
	}
	filters := evmrpc.EncodeFilters(addresses, nil)
	var bloom ethtypes.Bloom
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evmrpc.MatchFilters(bloom, filters)
	}
}
