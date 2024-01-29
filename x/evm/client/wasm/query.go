package wasm

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"

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
	abi, err := native.NativeMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	evmAddr := h.k.GetEVMAddressFromBech32OrDefault(ctx, recipient)
	bz, err := abi.Pack("transfer", evmAddr, amount.BigInt())
	if err != nil {
		return nil, err
	}
	res := bindings.ERCPayloadResponse{EncodedPayload: base64.StdEncoding.EncodeToString(bz)}
	return json.Marshal(res)
}

func (h *EVMQueryHandler) HandleERC20TokenInfo(ctx sdk.Context, contractAddress string, caller string) ([]byte, error) {
	callerAddr, err := sdk.AccAddressFromBech32(caller)
	if err != nil {
		return nil, err
	}
	contract := common.HexToAddress(contractAddress)
	abi, err := native.NativeMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	response := bindings.ERC20TokenInfoResponse{}

	bz, err := abi.Pack("totalSupply")
	if err != nil {
		return nil, err
	}
	res, err := h.k.StaticCallEVM(ctx, callerAddr, &contract, bz)
	if err != nil {
		return nil, err
	}
	unpacked, err := abi.Unpack("totalSupply", res)
	if err != nil {
		return nil, err
	}
	totalSupply := sdk.NewIntFromBigInt(unpacked[0].(*big.Int))
	response.TotalSupply = &totalSupply

	bz, err = abi.Pack("name")
	if err != nil {
		return nil, err
	}
	res, err = h.k.StaticCallEVM(ctx, callerAddr, &contract, bz)
	if err != nil {
		return nil, err
	}
	unpacked, err = abi.Unpack("name", res)
	if err != nil {
		return nil, err
	}
	response.Name = unpacked[0].(string)

	bz, err = abi.Pack("symbol")
	if err != nil {
		return nil, err
	}
	res, err = h.k.StaticCallEVM(ctx, callerAddr, &contract, bz)
	if err != nil {
		return nil, err
	}
	unpacked, err = abi.Unpack("symbol", res)
	if err != nil {
		return nil, err
	}
	response.Symbol = unpacked[0].(string)

	bz, err = abi.Pack("decimals")
	if err != nil {
		return nil, err
	}
	res, err = h.k.StaticCallEVM(ctx, callerAddr, &contract, bz)
	if err != nil {
		return nil, err
	}
	unpacked, err = abi.Unpack("decimals", res)
	if err != nil {
		return nil, err
	}
	response.Decimals = unpacked[0].(byte)

	return json.Marshal(response)
}

func (h *EVMQueryHandler) HandleERC20Balance(ctx sdk.Context, contractAddress string, account string) ([]byte, error) {
	addr, err := sdk.AccAddressFromBech32(account)
	if err != nil {
		return nil, err
	}
	evmAddr := h.k.GetEVMAddressOrDefault(ctx, addr)
	contract := common.HexToAddress(contractAddress)
	abi, err := native.NativeMetaData.GetAbi()
	if err != nil {
		return nil, err
	}

	bz, err := abi.Pack("balanceOf", evmAddr)
	if err != nil {
		return nil, err
	}
	res, err := h.k.StaticCallEVM(ctx, addr, &contract, bz)
	if err != nil {
		return nil, err
	}
	unpacked, err := abi.Unpack("balanceOf", res)
	if err != nil {
		return nil, err
	}
	balance := sdk.NewIntFromBigInt(unpacked[0].(*big.Int))
	return json.Marshal(bindings.ERC20BalanceResponse{Balance: &balance})
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
		owner = h.k.GetSeiAddressOrDefault(ctx, typedOwner).String()
	}
	response := bindings.ERC721OwnerResponse{Owner: owner}
	return json.Marshal(response)
}

func (h *EVMQueryHandler) HandleERC721TransferPayload(ctx sdk.Context, from string, recipient string, tokenId string) ([]byte, error) {
	abi, err := cw721.Cw721MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	fromEvmAddr := h.k.GetEVMAddressFromBech32OrDefault(ctx, from)
	toEvmAddr := h.k.GetEVMAddressFromBech32OrDefault(ctx, recipient)
	t, ok := sdk.NewIntFromString(tokenId)
	if !ok {
		return nil, errors.New("invalid token ID for ERC20, must be a big Int")
	}
	bz, err := abi.Pack("transferFrom", fromEvmAddr, toEvmAddr, t.BigInt())
	if err != nil {
		return nil, err
	}
	res := bindings.ERCPayloadResponse{EncodedPayload: base64.StdEncoding.EncodeToString(bz)}
	return json.Marshal(res)
}

func (h *EVMQueryHandler) HandleERC721ApprovePayload(ctx sdk.Context, spender string, tokenId string) ([]byte, error) {
	spenderEvmAddr := common.Address{} // empty address if approval should be revoked (i.e. spender string is empty)
	if spender != "" {
		spenderEvmAddr = h.k.GetEVMAddressFromBech32OrDefault(ctx, spender)
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
	evmAddr := h.k.GetEVMAddressFromBech32OrDefault(ctx, to)
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

func (h *EVMQueryHandler) HandleERC20TransferFromPayload(ctx sdk.Context, owner string, recipient string, amount *sdk.Int) ([]byte, error) {
	abi, err := native.NativeMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	ownerEvmAddr := h.k.GetEVMAddressFromBech32OrDefault(ctx, owner)
	recipientEvmAddr := h.k.GetEVMAddressFromBech32OrDefault(ctx, recipient)
	bz, err := abi.Pack("transferFrom", ownerEvmAddr, recipientEvmAddr, amount.BigInt())
	if err != nil {
		return nil, err
	}
	res := bindings.ERCPayloadResponse{EncodedPayload: base64.StdEncoding.EncodeToString(bz)}
	return json.Marshal(res)
}

func (h *EVMQueryHandler) HandleERC20ApprovePayload(ctx sdk.Context, spender string, amount *sdk.Int) ([]byte, error) {
	abi, err := native.NativeMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	spenderEvmAddr := h.k.GetEVMAddressFromBech32OrDefault(ctx, spender)

	bz, err := abi.Pack("approve", spenderEvmAddr, amount.BigInt())
	if err != nil {
		return nil, err
	}
	res := bindings.ERCPayloadResponse{EncodedPayload: base64.StdEncoding.EncodeToString(bz)}
	return json.Marshal(res)
}

func (h *EVMQueryHandler) HandleERC20Allowance(ctx sdk.Context, contractAddress string, owner string, spender string) ([]byte, error) {
	// Get the evm address of the owner
	ownerAddr := sdk.MustAccAddressFromBech32(owner)
	ownerEvmAddr := h.k.GetEVMAddressOrDefault(ctx, ownerAddr)

	// Get the evm address of spender
	spenderEvmAddr := h.k.GetEVMAddressFromBech32OrDefault(ctx, spender)

	// Fetch the contract ABI
	contract := common.HexToAddress(contractAddress)
	abi, err := native.NativeMetaData.GetAbi()
	if err != nil {
		return nil, err
	}

	// Make the query to allowance(owner, spender)
	bz, err := abi.Pack("allowance", ownerEvmAddr, spenderEvmAddr)
	if err != nil {
		return nil, err
	}
	res, err := h.k.StaticCallEVM(ctx, ownerAddr, &contract, bz)
	if err != nil {
		return nil, err
	}

	// Parse the response (Should be of type uint256 if successful)
	typed, err := abi.Unpack("allowance", res)
	if err != nil {
		return nil, err
	}
	allowance := typed[0].(*big.Int)
	allowanceSdk := sdk.NewIntFromBigInt(allowance)
	response := bindings.ERC20AllowanceResponse{Allowance: &allowanceSdk}
	return json.Marshal(response)
}

func (h *EVMQueryHandler) HandleERC721Approved(ctx sdk.Context, caller string, contractAddress string, tokenId string) ([]byte, error) {
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
	bz, err := abi.Pack("getApproved", t.BigInt())
	if err != nil {
		return nil, err
	}
	res, err := h.k.StaticCallEVM(ctx, callerAddr, &contract, bz)
	if err != nil {
		return nil, err
	}
	typed, err := abi.Unpack("getApproved", res)
	if err != nil {
		return nil, err
	}
	approved := typed[0].(common.Address)
	a := ""
	if (approved != common.Address{}) {
		a = h.k.GetSeiAddressOrDefault(ctx, approved).String()
	}
	response := bindings.ERC721ApprovedResponse{Approved: a}
	return json.Marshal(response)
}

func (h *EVMQueryHandler) HandleERC721IsApprovedForAll(ctx sdk.Context, caller string, contractAddress string, owner string, operator string) ([]byte, error) {
	callerAddr, err := sdk.AccAddressFromBech32(caller)
	if err != nil {
		return nil, err
	}
	ownerEvmAddr := h.k.GetEVMAddressFromBech32OrDefault(ctx, owner)
	operatorEvmAddr := h.k.GetEVMAddressFromBech32OrDefault(ctx, operator)
	contract := common.HexToAddress(contractAddress)
	abi, err := cw721.Cw721MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	bz, err := abi.Pack("isApprovedForAll", ownerEvmAddr, operatorEvmAddr)
	if err != nil {
		return nil, err
	}
	res, err := h.k.StaticCallEVM(ctx, callerAddr, &contract, bz)
	if err != nil {
		return nil, err
	}
	typed, err := abi.Unpack("isApprovedForAll", res)
	if err != nil {
		return nil, err
	}
	response := bindings.ERC721IsApprovedForAllResponse{IsApproved: typed[0].(bool)}
	return json.Marshal(response)
}

func (h *EVMQueryHandler) HandleERC721NameSymbol(ctx sdk.Context, caller string, contractAddress string) ([]byte, error) {
	callerAddr, err := sdk.AccAddressFromBech32(caller)
	if err != nil {
		return nil, err
	}
	contract := common.HexToAddress(contractAddress)
	abi, err := cw721.Cw721MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	bz, err := abi.Pack("name")
	if err != nil {
		return nil, err
	}
	res, err := h.k.StaticCallEVM(ctx, callerAddr, &contract, bz)
	if err != nil {
		return nil, err
	}
	typed, err := abi.Unpack("name", res)
	if err != nil {
		return nil, err
	}
	name := typed[0].(string)
	bz, err = abi.Pack("symbol")
	if err != nil {
		return nil, err
	}
	res, err = h.k.StaticCallEVM(ctx, callerAddr, &contract, bz)
	if err != nil {
		return nil, err
	}
	typed, err = abi.Unpack("symbol", res)
	if err != nil {
		return nil, err
	}
	symbol := typed[0].(string)
	response := bindings.ERC721NameSymbolResponse{Name: name, Symbol: symbol}
	return json.Marshal(response)
}

func (h *EVMQueryHandler) HandleERC721Uri(ctx sdk.Context, caller string, contractAddress string, tokenId string) ([]byte, error) {
	callerAddr, err := sdk.AccAddressFromBech32(caller)
	if err != nil {
		return nil, err
	}
	t, ok := sdk.NewIntFromString(tokenId)
	if !ok {
		return nil, errors.New("invalid token ID for ERC20, must be a big Int")
	}
	contract := common.HexToAddress(contractAddress)
	abi, err := cw721.Cw721MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	bz, err := abi.Pack("tokenURI", t.BigInt())
	if err != nil {
		return nil, err
	}
	res, err := h.k.StaticCallEVM(ctx, callerAddr, &contract, bz)
	if err != nil {
		return nil, err
	}
	typed, err := abi.Unpack("tokenURI", res)
	if err != nil {
		return nil, err
	}
	response := bindings.ERC721UriResponse{Uri: typed[0].(string)}
	return json.Marshal(response)
}
