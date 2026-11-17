package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// EVMEnvironment combines node RPC services with an EVM application.
type EVMEnvironment struct {
	*Environment
	*proxy.EVMProxy
}

// BlockHash returns the hash Block reports for the block at height, without reading its
// transactions. It returns the height errors Block returns.
func (env *EVMEnvironment) BlockHash(ctx context.Context, height int64) (common.Hash, error) {
	giga, ok := env.gigaRouter().Get()
	if !ok {
		return common.Hash{}, errors.New("block hash lookup requires Autobahn")
	}
	height, err := env.autobahnCheckAndGetHeight(&height)
	if err != nil {
		return common.Hash{}, err
	}
	n, ok := utils.SafeCast[atypes.RoadIndex](height)
	if !ok {
		return common.Hash{}, fmt.Errorf("invalid height %d", height)
	}
	return giga.BlockHash(ctx, n)
}
