package mev

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/credentials"

	types "github.com/SiloMEV/silo-mev-protobuf-go/mev/v1"
	"github.com/tendermint/tendermint/libs/log"
	"google.golang.org/grpc"
)

type Poller struct {
	client            types.BundleProviderClient
	keeper            *Keeper
	lastBlockProvider func() int64
	logger            log.Logger
	ctx               context.Context
}

func (p *Poller) run() {

	lastHeight := p.lastBlockProvider()

	bundles, err := p.client.GetBundles(context.Background(), &types.GetBundlesRequest{MinBlockHeight: uint64(lastHeight)})
	if err != nil {
		p.logger.Error("Error while querying bundle server", "err", err)
		return
	}
	for height, bundles := range bundles.Bundles {
		p.keeper.SetBundles(int64(height), bundles.Bundles)
	}

	p.keeper.DropBundlesAtAndBelow(lastHeight - 1)
}

func NewPoller(ctx context.Context, logger log.Logger, config Config, keeper *Keeper, lastBlockProvider func() int64) (*Poller, error) {

	logger.Info("Starting bundle provider poller")

	if config.CertFile == "" && !config.Insecure {
		return nil, fmt.Errorf("either certFile or insecure must be set")
	}

	var option grpc.DialOption

	if config.CertFile != "" {
		creds, err := credentials.NewClientTLSFromFile(config.CertFile, "")
		if err != nil {
			return nil, fmt.Errorf("error while loading TLS certificate: %w", err)
		}
		option = grpc.WithTransportCredentials(creds)
	} else {
		option = grpc.WithInsecure()
	}

	grpcConn, err := grpc.DialContext(ctx, config.ServerAddr, option)
	if err != nil {
		return nil, err
	}

	bundleProviderClient := types.NewBundleProviderClient(grpcConn)

	p := &Poller{
		client:            bundleProviderClient,
		keeper:            keeper,
		lastBlockProvider: lastBlockProvider,
		logger:            logger,
		ctx:               ctx,
	}

	ticker := time.NewTicker(config.PollInterval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.run()
			}
		}
	}()

	return p, nil
}
