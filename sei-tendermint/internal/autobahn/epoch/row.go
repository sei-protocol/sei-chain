package epoch

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/hashable"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
)

// DefaultEpochLength is the default number of road blocks per epoch.
const DefaultEpochLength types.RoadHeight = 108_000

// Decode returns the epoch registry a row holds.
func Decode(row []byte) (*types.EpochRegistry, error) {
	raw, err := protoutils.Unmarshal[*pb.EpochRegistry](row)
	if err != nil {
		return nil, fmt.Errorf("unmarshal epoch registry: %w", err)
	}
	registry, err := types.EpochRegistryConv.DecodeReq(raw)
	if err != nil {
		return nil, fmt.Errorf("epoch registry: %w", err)
	}
	return registry, nil
}

// Encode returns the canonical row of registry.
func Encode(registry *types.EpochRegistry) []byte {
	return hashable.MarshalCanonical(types.EpochRegistryConv.Encode(registry))
}
