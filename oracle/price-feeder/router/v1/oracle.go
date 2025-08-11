package v1

import (
	"time"

	sdk "github.com/sei-protocol/sei-chain/cosmos-sdk/types"
)

// Oracle defines the Oracle interface contract that the v1 router depends on.
type Oracle interface {
	GetLastPriceSyncTimestamp() time.Time
	GetPrices() sdk.DecCoins
}
