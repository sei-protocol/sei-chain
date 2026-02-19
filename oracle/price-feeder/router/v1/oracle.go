package v1

import (
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// Oracle defines the Oracle interface contract that the v1 router depends on.
type Oracle interface {
	GetLastPriceSyncTimestamp() time.Time
	GetPrices() sdk.DecCoins
}
