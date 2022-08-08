package abci

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
	otrace "go.opentelemetry.io/otel/trace"
)

func (w KeeperWrapper) HandleBBNewBlock(sdkCtx sdk.Context, contractAddr string, epoch int64, tracer *otrace.Tracer) error {
	msg := wasm.SudoNewBlockMsg{
		NewBlock: wasm.NewBlockRequest{Epoch: epoch},
	}
	_, err := utils.CallContractSudo(sdkCtx, w.Keeper, contractAddr, msg, tracer)
	return err
}
