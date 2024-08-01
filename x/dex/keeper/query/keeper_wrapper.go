package query

import (
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
)

type KeeperWrapper struct {
	*keeper.Keeper
}
