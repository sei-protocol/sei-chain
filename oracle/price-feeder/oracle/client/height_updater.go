package client

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	tmrpcclient "github.com/tendermint/tendermint/rpc/client"
	tmtypes "github.com/tendermint/tendermint/types"
)

var (
	started                  = false
	queryEventNewBlockHeader = tmtypes.QueryForEvent(tmtypes.EventNewBlockHeaderValue)
	queryInterval            = 20 * time.Millisecond
)

// HeightUpdater is used to provide the updates of the latest chain
// It starts a goroutine to subscribe to new block event and send the latest block height to the channel
type HeightUpdater struct {
	Logger        zerolog.Logger
	LastHeight    int64
	ChBlockHeight chan int64
}

// Start will start a new goroutine subscribed to EventNewBlockHeader.
func (heightUpdater HeightUpdater) Start(
	ctx context.Context,
	rpcClient tmrpcclient.Client,
	logger zerolog.Logger,
) error {
	if !started {
		if err := rpcClient.Start(ctx); err != nil {
			return err
		}
		go heightUpdater.subscribe(ctx, rpcClient, logger)
		started = true
	}
	return nil
}

// subscribe listens to new blocks being made
// and updates the chain height.
func (heightUpdater HeightUpdater) subscribe(
	ctx context.Context,
	eventsClient tmrpcclient.EventsClient,
	logger zerolog.Logger,
) {
	for {
		eventData, err := tmrpcclient.WaitForOneEvent(ctx, eventsClient, queryEventNewBlockHeader.String())
		if err != nil {
			logger.Debug().Err(err).Msg("Failed to query EventNewBlockHeader")
		}
		eventDataNewBlockHeader, ok := eventData.(tmtypes.EventDataNewBlockHeader)
		if !ok {
			logger.Err(err).Msg("Failed to parse event from eventDataNewBlockHeader")
			continue
		}
		eventHeight := eventDataNewBlockHeader.Header.Height
		if eventHeight > heightUpdater.LastHeight {
			logger.Info().Msg(fmt.Sprintf("Received new Chain Height: %d", eventHeight))
			heightUpdater.LastHeight = eventHeight
			if len(heightUpdater.ChBlockHeight) < 1 {
				heightUpdater.ChBlockHeight <- eventHeight
			} else {
				// skip this block height since price feeder is still sending previous transaction
				logger.Info().Msg(fmt.Sprintf("Skipped Block Height: %d due to in progress tx", eventHeight))
			}
		}

		time.Sleep(queryInterval)
	}
}
