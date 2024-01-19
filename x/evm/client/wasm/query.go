package wasm

import (
	"encoding/base64"
	"encoding/json"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/native"
	"github.com/sei-protocol/sei-chain/x/evm/client/wasm/bindings"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type EVMQueryHandler struct {
	k *keeper.Keeper
}

func NewEVMQueryHandler(k *keeper.Keeper) *EVMQueryHandler {
	return &EVMQueryHandler{k: k}
}

func (h *EVMQueryHandler) HandleStaticCall(ctx sdk.Context, from string, to string, data []byte) ([]byte, error) {
	fromAddr := sdk.MustAccAddressFromBech32(from)
	var toAddr *common.Address
	if to != "" {
		toSeiAddr := h.k.SeiAddrToEvmAddr(ctx, sdk.MustAccAddressFromBech32(to))
		toAddr = &toSeiAddr
	}
	return h.k.StaticCallEVM(ctx, fromAddr, toAddr, data)
}

func (h *EVMQueryHandler) HandleERC20TransferPayload(ctx sdk.Context, recipient string, amount *sdk.Int) ([]byte, error) {
	addr, err := sdk.AccAddressFromBech32(recipient)
	if err != nil {
		return nil, err
	}
	abi, err := native.NativeMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	evmAddr, found := h.k.GetEVMAddress(ctx, addr)
	if !found {
		evmAddr = common.Address{}
		evmAddr.SetBytes(addr)
	}
	bz, err := abi.Pack("transfer", evmAddr, amount.BigInt())
	if err != nil {
		return nil, err
	}
	res := bindings.ERC20TransferPayloadResponse{EncodedPayload: base64.StdEncoding.EncodeToString(bz)}
	return json.Marshal(res)
}
