package utils

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	BlocksPerMinute = uint64(75)
	BlocksPerHour   = BlocksPerMinute * 60
	BlocksPerDay    = BlocksPerHour * 24
	BlocksPerWeek   = BlocksPerDay * 7
	BlocksPerMonth  = BlocksPerDay * 30
	BlocksPerYear   = BlocksPerDay * 365
)

func IsPeriodLastBlock(ctx sdk.Context, blocksPerPeriod uint64) bool {
	return ((uint64)(ctx.BlockHeight())+1)%blocksPerPeriod == 0
}
