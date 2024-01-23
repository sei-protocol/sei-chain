package wasm

import (
	"encoding/base64"
	"encoding/json"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/artifacts/cw721"
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
		toSeiAddr := common.HexToAddress(to)
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
	res := bindings.ERCPayloadResponse{EncodedPayload: base64.StdEncoding.EncodeToString(bz)}
	return json.Marshal(res)
}

func (h *EVMQueryHandler) HandleERC721Owner(ctx sdk.Context, caller string, contractAddress string, tokenId string) ([]byte, error) {
	callerAddr, err := sdk.AccAddressFromBech32(caller)
	if err != nil {
		return nil, err
	}
	contract := common.HexToAddress(contractAddress)
	abi, err := cw721.Cw721MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	t, ok := sdk.NewIntFromString(tokenId)
	if !ok {
		return nil, errors.New("invalid token ID for ERC20, must be a big Int")
	}
	bz, err := abi.Pack("ownerOf", t.BigInt())
	if err != nil {
		return nil, err
	}
	res, err := h.k.StaticCallEVM(ctx, callerAddr, &contract, bz)
	if err != nil {
		return nil, err
	}
	typed, err := abi.Unpack("ownerOf", res)
	if err != nil {
		return nil, err
	}
	typedOwner := typed[0].(common.Address)
	owner := ""
	if (typedOwner != common.Address{}) {
		ownerSeiAddr, found := h.k.GetSeiAddress(ctx, typedOwner)
		if !found {
			ownerSeiAddr = sdk.AccAddress(typedOwner[:])
		}
		owner = ownerSeiAddr.String()
	}
	response := bindings.ERC721OwnerResponse{Owner: owner}
	return json.Marshal(response)
}

func (h *EVMQueryHandler) HandleERC721TransferPayload(ctx sdk.Context, from string, recipient string, tokenId string) ([]byte, error) {
	fromAddr, err := sdk.AccAddressFromBech32(from)
	if err != nil {
		return nil, err
	}
	addr, err := sdk.AccAddressFromBech32(recipient)
	if err != nil {
		return nil, err
	}
	abi, err := cw721.Cw721MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	fromEvmAddr, found := h.k.GetEVMAddress(ctx, fromAddr)
	if !found {
		fromEvmAddr = common.Address{}
		fromEvmAddr.SetBytes(fromAddr)
	}
	evmAddr, found := h.k.GetEVMAddress(ctx, addr)
	if !found {
		evmAddr = common.Address{}
		evmAddr.SetBytes(addr)
	}
	t, ok := sdk.NewIntFromString(tokenId)
	if !ok {
		return nil, errors.New("invalid token ID for ERC20, must be a big Int")
	}
	bz, err := abi.Pack("transferFrom", fromEvmAddr, evmAddr, t.BigInt())
	if err != nil {
		return nil, err
	}
	res := bindings.ERCPayloadResponse{EncodedPayload: base64.StdEncoding.EncodeToString(bz)}
	return json.Marshal(res)
}

func (h *EVMQueryHandler) HandleERC721ApprovePayload(ctx sdk.Context, spender string, tokenId string) ([]byte, error) {
	spenderEvmAddr := common.Address{} // empty address if approval should be revoked (i.e. spender string is empty)
	if spender != "" {
		spenderAddr, err := sdk.AccAddressFromBech32(spender)
		if err != nil {
			return nil, err
		}
		spenderEvmAddr, found := h.k.GetEVMAddress(ctx, spenderAddr)
		if !found {
			spenderEvmAddr = common.Address{}
			spenderEvmAddr.SetBytes(spenderAddr)
		}
	}
	abi, err := cw721.Cw721MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	t, ok := sdk.NewIntFromString(tokenId)
	if !ok {
		return nil, errors.New("invalid token ID for ERC20, must be a big Int")
	}
	bz, err := abi.Pack("approve", spenderEvmAddr, t.BigInt())
	if err != nil {
		return nil, err
	}
	res := bindings.ERCPayloadResponse{EncodedPayload: base64.StdEncoding.EncodeToString(bz)}
	return json.Marshal(res)
}

func (h *EVMQueryHandler) HandleERC721SetApprovalAllPayload(ctx sdk.Context, to string, approved bool) ([]byte, error) {
	addr, err := sdk.AccAddressFromBech32(to)
	if err != nil {
		return nil, err
	}
	evmAddr, found := h.k.GetEVMAddress(ctx, addr)
	if !found {
		evmAddr = common.Address{}
		evmAddr.SetBytes(addr)
	}
	abi, err := cw721.Cw721MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	bz, err := abi.Pack("setApprovalForAll", evmAddr, approved)
	if err != nil {
		return nil, err
	}
	res := bindings.ERCPayloadResponse{EncodedPayload: base64.StdEncoding.EncodeToString(bz)}
	return json.Marshal(res)
}
