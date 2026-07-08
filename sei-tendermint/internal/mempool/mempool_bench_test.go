package mempool

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func BenchmarkTxMempool_CheckTx(b *testing.B) {
	ctx := b.Context()

	client := kvstore.NewApplication()
	proxyClient := proxy.New(client, proxy.NewMetrics())

	// setup the cache and the mempool number for hitting GetEvictableTxs during the
	// benchmark. 5000 is the current default mempool size in the TM config.
	cfg := TestConfig()
	cfg.CacheSize = 10000
	cfg.Size = 5000
	txmp := setup(cfg, proxyClient, NopTxConstraintsFetcher)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	const peerID = 1

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		b.StopTimer()
		prefix := make([]byte, 20)
		_, err := rng.Read(prefix)
		require.NoError(b, err)

		priority := int64(rng.Intn(9999-1000) + 1000)
		tx := []byte(fmt.Sprintf("sender-%d-%d=%X=%d", n, peerID, prefix, priority))

		b.StartTimer()

		_, err = txmp.CheckTx(ctx, tx)
		require.NoError(b, err)
	}
}
