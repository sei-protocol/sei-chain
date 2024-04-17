package wasm_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/client/wasm"
	"github.com/sei-protocol/sei-chain/x/evm/client/wasm/bindings"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/stretchr/testify/require"
)

func deployERC20ToAddr(t *testing.T, ctx types.Context, k *keeper.Keeper, to common.Address) {
	code, err := os.ReadFile("../../../../example/contracts/erc20/ERC20.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	k.SetCode(ctx, to, bz)
}

func TestHandleStaticCall(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	from, _ := testkeeper.MockAddressPair()
	_, to := testkeeper.MockAddressPair()
	h := wasm.NewEVMQueryHandler(k)
	deployERC20ToAddr(t, ctx, k, to)
	res, err := h.HandleStaticCall(ctx, from.String(), to.String(), []byte("123"))
	require.Nil(nil, err)
	require.NotNil(t, res)
}

func TestERC721TransferPayload(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	addr1, e1 := testkeeper.MockAddressPair()
	addr2, e2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	k.SetAddressMapping(ctx, addr2, e2)
	h := wasm.NewEVMQueryHandler(k)
	res, err := h.HandleERC721TransferPayload(ctx, addr1.String(), addr2.String(), "1")
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestERC721ApprovePayload(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	h := wasm.NewEVMQueryHandler(k)
	res, err := h.HandleERC721ApprovePayload(ctx, addr1.String(), "1")
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestERC721ApproveAllPayload(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	h := wasm.NewEVMQueryHandler(k)
	res, err := h.HandleERC721SetApprovalAllPayload(ctx, addr1.String(), true)
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestERC20TransferPayload(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	h := wasm.NewEVMQueryHandler(k)
	value := types.NewInt(500)
	res, err := h.HandleERC20TransferPayload(ctx, addr1.String(), &value)
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestHandleERC20TokenInfo(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, contract := testkeeper.MockAddressPair()
	caller, _ := testkeeper.MockAddressPair()
	h := wasm.NewEVMQueryHandler(k)
	deployERC20ToAddr(t, ctx, k, contract)
	res, err := h.HandleERC20TokenInfo(ctx, contract.String(), caller.String())
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestERC20TransferFromPayload(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	addr1, e1 := testkeeper.MockAddressPair()
	addr2, e2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	k.SetAddressMapping(ctx, addr2, e2)
	h := wasm.NewEVMQueryHandler(k)
	value := types.NewInt(500)
	res, err := h.HandleERC20TransferFromPayload(ctx, addr1.String(), addr2.String(), &value)
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestERC20ApprovePayload(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	h := wasm.NewEVMQueryHandler(k)
	value := types.NewInt(500)
	res, err := h.HandleERC20ApprovePayload(ctx, addr1.String(), &value)
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

// func TestHandleERC20Balance(t *testing.T) {
// 	k, ctx := testkeeper.MockEVMKeeper()
// 	_, contractAddr := testkeeper.MockAddressPair()
// 	addr, _ := testkeeper.MockAddressPair()
// 	h := wasm.NewEVMQueryHandler(k)
// 	res, err := h.HandleERC20Balance(ctx, contractAddr.String(), addr.String())
// 	require.Nil(t, err)
// 	require.NotEmpty(t, res)
// }

// func TestHandleERC721Owner(t *testing.T) {
// 	k, ctx := testkeeper.MockEVMKeeper()
// 	caller, _ := testkeeper.MockAddressPair()
// 	_, contractAddr := testkeeper.MockAddressPair()
// 	h := wasm.NewEVMQueryHandler(k)
// 	_, err := h.HandleERC721Owner(ctx, caller.String(), contractAddr.String(), "1")
// 	require.Nil(t, err)
//  require.NotEmpty(t, res)
// }

func TestGetAddress(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	seiAddr1, evmAddr1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr1, evmAddr1)
	seiAddr2, evmAddr2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr2, evmAddr2)
	h := wasm.NewEVMQueryHandler(k)
	getEvmAddrResp := &bindings.GetEvmAddressResponse{}
	res, err := h.HandleGetEvmAddress(ctx, seiAddr1.String())
	require.Nil(t, err)
	require.Nil(t, json.Unmarshal(res, getEvmAddrResp))
	require.True(t, getEvmAddrResp.Associated)
	require.Equal(t, evmAddr1.Hex(), getEvmAddrResp.EvmAddress)
	getEvmAddrResp = &bindings.GetEvmAddressResponse{}
	res, err = h.HandleGetEvmAddress(ctx, seiAddr2.String())
	require.Nil(t, err)
	require.Nil(t, json.Unmarshal(res, getEvmAddrResp))
	require.True(t, getEvmAddrResp.Associated)
	getSeiAddrResp := &bindings.GetSeiAddressResponse{}
	res, err = h.HandleGetSeiAddress(ctx, evmAddr1.Hex())
	require.Nil(t, err)
	require.Nil(t, json.Unmarshal(res, getSeiAddrResp))
	require.True(t, getSeiAddrResp.Associated)
	require.Equal(t, seiAddr1.String(), getSeiAddrResp.SeiAddress)
	getSeiAddrResp = &bindings.GetSeiAddressResponse{}
	res, err = h.HandleGetSeiAddress(ctx, evmAddr2.Hex())
	require.Nil(t, err)
	require.Nil(t, json.Unmarshal(res, getSeiAddrResp))
	require.True(t, getSeiAddrResp.Associated)
}
