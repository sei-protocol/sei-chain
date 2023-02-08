package oracle

import (
	"context"
	"fmt"
	"time"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"google.golang.org/grpc"
)

const (
	jailCacheIntervalBlocks = int64(50)
)

type JailCache struct {
	isJailed         bool
	lastUpdatedBlock int64
}

func (jailCache *JailCache) Update(currentBlockHeight int64, isJailed bool) {
	jailCache.lastUpdatedBlock = currentBlockHeight
	jailCache.isJailed = isJailed
}

func (jailCache *JailCache) IsOutdated(currentBlockHeight int64) bool {
	if currentBlockHeight < jailCacheIntervalBlocks {
		return false
	}

	return (currentBlockHeight - jailCache.lastUpdatedBlock) > jailCacheIntervalBlocks
}

func (o *Oracle) GetCachedJailedState(ctx context.Context, currentBlockHeight int64) (bool, error) {
	if !o.jailCache.IsOutdated(currentBlockHeight) {
		return o.jailCache.isJailed, nil
	}

	isJailed, err := o.GetJailedState(ctx)
	if err != nil {
		return false, err
	}

	o.jailCache.Update(currentBlockHeight, isJailed)
	return isJailed, nil
}

// GetJailedState returns the current on-chain jailing state of the validator
func (o *Oracle) GetJailedState(ctx context.Context) (bool, error) {
	grpcConn, err := grpc.Dial(
		o.oracleClient.GRPCEndpoint,
		// the Cosmos SDK doesn't support any transport security mechanism
		grpc.WithInsecure(),
		grpc.WithContextDialer(dialerFunc),
	)
	if err != nil {
		return false, fmt.Errorf("failed to dial Cosmos gRPC service: %w", err)
	}

	defer grpcConn.Close()
	queryClient := stakingtypes.NewQueryClient(grpcConn)

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	queryResponse, err := queryClient.Validator(ctx, &stakingtypes.QueryValidatorRequest{ValidatorAddr: o.oracleClient.ValidatorAddrString})
	if err != nil {
		return false, fmt.Errorf("failed to get staking validator: %w", err)
	}

	return queryResponse.Validator.Jailed, nil
}
