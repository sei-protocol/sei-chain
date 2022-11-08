package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	tmrpcclient "github.com/tendermint/tendermint/rpc/client"
	tmtypes "github.com/tendermint/tendermint/types"
)

var (
	errParseEventDataNewBlockHeader = errors.New("error parsing EventDataNewBlockHeader")
	queryEventNewBlockHeader        = tmtypes.QueryForEvent(tmtypes.EventNewBlockHeaderValue)
)

// ChainHeight is used to cache the chain height of the
// current node which is being updated each time the
// node sends an event of EventNewBlockHeader.
// It starts a goroutine to subscribe to blockchain new block event and update the cached height.
type ChainHeight struct {
	Logger zerolog.Logger

	mtx               sync.RWMutex
	errGetChainHeight error
	lastChainHeight   int64
}

// NewChainHeight returns a new ChainHeight struct that
// starts a new goroutine subscribed to EventNewBlockHeader.
func NewChainHeight(
	ctx context.Context,
	rpcClient tmrpcclient.Client,
	logger zerolog.Logger,
	initialHeight int64,
) (*ChainHeight, error) {
	if initialHeight < 1 {
		return nil, fmt.Errorf("expected positive initial block height")
	}

	if err := rpcClient.Start(ctx); err != nil {
		return nil, err
	}

	chainHeight := &ChainHeight{
		Logger:            logger.With().Str("oracle_client", "chain_height").Logger(),
		errGetChainHeight: nil,
		lastChainHeight:   initialHeight,
	}

	go chainHeight.subscribe(ctx, rpcClient)

	return chainHeight, nil
}

// updateChainHeight receives the data to be updated thread safe.
func (chainHeight *ChainHeight) updateChainHeight(blockHeight int64, err error) {
	chainHeight.mtx.Lock()
	defer chainHeight.mtx.Unlock()

	chainHeight.lastChainHeight = blockHeight
	chainHeight.errGetChainHeight = err
}

// subscribe listens to new blocks being made
// and updates the chain height.
func (chainHeight *ChainHeight) subscribe(
	ctx context.Context,
	eventsClient tmrpcclient.EventsClient,
) {
	for {
		eventData, err := tmrpcclient.WaitForOneEvent(ctx, eventsClient, queryEventNewBlockHeader.String())
		if err != nil {
			chainHeight.Logger.Err(err)
			chainHeight.updateChainHeight(chainHeight.lastChainHeight, err)
		}
		eventDataNewBlockHeader, ok := eventData.(tmtypes.EventDataNewBlockHeader)
		if !ok {
			chainHeight.Logger.Err(errParseEventDataNewBlockHeader)
			chainHeight.updateChainHeight(chainHeight.lastChainHeight, errParseEventDataNewBlockHeader)
			continue
		}
		if eventDataNewBlockHeader.Header.Height != chainHeight.lastChainHeight {
			chainHeight.updateChainHeight(eventDataNewBlockHeader.Header.Height, nil)
			chainHeight.Logger.Debug().Msg(fmt.Sprintf("New Chain Height: %d", eventDataNewBlockHeader.Header.Height))
		}
		// sleep 20 ms before checking for new block
		time.Sleep(20 * time.Millisecond)
	}
}

// GetChainHeight returns the last chain height available.
func (chainHeight *ChainHeight) GetChainHeight() (int64, error) {
	chainHeight.mtx.RLock()
	defer chainHeight.mtx.RUnlock()

	return chainHeight.lastChainHeight, chainHeight.errGetChainHeight
}
