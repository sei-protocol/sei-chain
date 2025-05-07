package mev

import (
	"math"
	"sync"

	types "github.com/SiloMEV/silo-mev-protobuf-go/mev/v1"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const minHeightNotSet = math.MaxInt64

type Keeper struct {
	cdc         codec.BinaryCodec
	ephemeralMu sync.RWMutex

	// Bundles are always ordered by value
	// block height -> []bundle
	ephemeral map[int64][]*types.Bundle

	// minHeight is the lowest block height key currently stored in the ephemeral map
	// helps to limit puring since map is not ordered
	minHeight int64
}

func NewKeeper(
	cdc codec.BinaryCodec,
	_ sdk.StoreKey, // keep parameter to maintain compatibility but don't use it
) *Keeper {
	return &Keeper{
		cdc:       cdc,
		ephemeral: make(map[int64][]*types.Bundle),
		minHeight: minHeightNotSet,
	}
}

func (k *Keeper) SetBundles(height int64, bundles []*types.Bundle) bool {
	k.ephemeralMu.Lock()
	defer k.ephemeralMu.Unlock()

	k.ephemeral[height] = bundles

	if height < k.minHeight {
		k.minHeight = height
	}

	return true
}

func (k *Keeper) PendingBundles(height int64) []*types.Bundle {
	k.ephemeralMu.RLock()
	defer k.ephemeralMu.RUnlock()

	return k.ephemeral[height]
}

func (k *Keeper) DropBundlesAtAndBelow(height int64) {
	k.ephemeralMu.Lock()
	defer k.ephemeralMu.Unlock()

	if k.minHeight == minHeightNotSet {
		// no bundles at all
		return
	}

	for i := height; i >= k.minHeight; i-- {
		delete(k.ephemeral, i)
	}

	if len(k.ephemeral) == 0 {
		k.minHeight = minHeightNotSet
	} else {
		k.minHeight = height
	}

}
