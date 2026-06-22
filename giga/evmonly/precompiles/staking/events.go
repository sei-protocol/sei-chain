package staking

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/util"
)

func (p *Precompile) emit(ctx *precompiles.Context, name string, indexed common.Address, args ...interface{}) {
	event, ok := p.abi.Events[name]
	if !ok {
		return
	}
	util.EmitEvent(ctx.Logs, p.address, event, indexed, args...)
}
