package mempool

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestConfig() *Config {
	cfg := DefaultConfig()
	cfg.CacheSize = 1000
	cfg.DropUtilisationThreshold = 0.0
	// Disable TTL purging in tests.
	cfg.TTLNumBlocks = utils.None[int64]()
	cfg.TTLDuration = utils.None[time.Duration]()
	return cfg
}

// InsertReadyTxForTest adds tx to the gossip list without CheckTx.
func (txmp *TxMempool) InsertReadyTxForTest(tx types.Tx) error {
	wtx := &WrappedTx{
		hashedTx:  newHashedTx(tx),
		timestamp: time.Now().UTC(),
		height:    txmp.height,
	}
	if err := txmp.txStore.Insert(wtx); err != nil {
		return err
	}
	txmp.notifyTxsAvailable()
	return nil
}
