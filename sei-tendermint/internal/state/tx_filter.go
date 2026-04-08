package state

import (
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func cachingStateFetcher(store Store) func() (State, error) {
	const ttl = time.Second

	var (
		last  time.Time
		mutex = &sync.Mutex{}
		cache State
		err   error
	)

	return func() (State, error) {
		mutex.Lock()
		defer mutex.Unlock()

		if time.Since(last) < ttl && cache.ChainID != "" {
			return cache, nil
		}

		cache, err = store.Load()
		if err != nil {
			return State{}, err
		}
		last = time.Now()

		return cache, nil
	}

}

// TxStateFetcherFromStore returns the precomputed consensus-derived mempool limits for the
// current persisted state.
func TxStateFetcherFromStore(store Store) mempool.TxStateFetcher {
	fetch := cachingStateFetcher(store)

	return func() (mempool.TxConstraints, error) {
		state, err := fetch()
		if err != nil {
			return mempool.TxConstraints{}, err
		}

		return TxStateFetcherForState(state)()
	}
}

func TxStateFetcherForState(state State) mempool.TxStateFetcher {
	return func() (mempool.TxConstraints, error) {
		return mempool.TxConstraints{
			MaxDataBytes: types.MaxDataBytesNoEvidence(
				state.ConsensusParams.Block.MaxBytes,
				state.Validators.Size(),
			),
			MaxGas: state.ConsensusParams.Block.MaxGas,
		}, nil
	}
}
