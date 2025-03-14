package wasm_test

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"regexp"
	"strings"
	"testing"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/example/contracts/simplestorage"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/client/wasm"
	"github.com/sei-protocol/sei-chain/x/evm/client/wasm/bindings"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestERC721TransferPayload(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
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
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	h := wasm.NewEVMQueryHandler(k)
	res, err := h.HandleERC721ApprovePayload(ctx, addr1.String(), "1")
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestERC721ApproveAllPayload(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	h := wasm.NewEVMQueryHandler(k)
	res, err := h.HandleERC721SetApprovalAllPayload(ctx, addr1.String(), true)
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestERC20TransferPayload(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	h := wasm.NewEVMQueryHandler(k)
	value := sdk.NewInt(500)
	res, err := h.HandleERC20TransferPayload(ctx, addr1.String(), &value)
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestHandleERC20TokenInfo(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc20/ERC20.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	tokenInfo, err := h.HandleERC20TokenInfo(ctx, contractAddr.String(), addr1.String())
	require.Nil(t, err)
	require.Equal(t, string(tokenInfo), "{\"name\":\"ERC20\",\"symbol\":\"ERC20\",\"decimals\":18,\"total_supply\":\"1000000000000000000000000\"}")
}

func TestERC20TransferFromPayload(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	addr1, e1 := testkeeper.MockAddressPair()
	addr2, e2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	k.SetAddressMapping(ctx, addr2, e2)
	h := wasm.NewEVMQueryHandler(k)
	value := sdk.NewInt(500)
	res, err := h.HandleERC20TransferFromPayload(ctx, addr1.String(), addr2.String(), &value)
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestERC20ApprovePayload(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	h := wasm.NewEVMQueryHandler(k)
	value := sdk.NewInt(500)
	res, err := h.HandleERC20ApprovePayload(ctx, addr1.String(), &value)
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestHandleERC20Balance(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc20/ERC20.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC20Balance(ctx, contractAddr.String(), addr1.String())
	require.Nil(t, err)
	require.Equal(t, string(res2), "{\"balance\":\"0\"}")
	require.NotEmpty(t, res2)
}

func TestHandleERC721Owner(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc721/DummyERC721.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC721Owner(ctx, addr1.String(), contractAddr.String(), "1")
	require.Nil(t, err)
	require.NotEmpty(t, res2)
}

func TestHandleERC20Allowance(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc20/ERC20.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	addr2, e2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	k.SetAddressMapping(ctx, addr2, e2)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC20Allowance(ctx, contractAddr.String(), addr1.String(), addr2.String())
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	require.Equal(t, string(res2), "{\"allowance\":\"0\"}")
}

func TestHandleERC721Approved(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc721/DummyERC721.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	addr2, e2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	k.SetAddressMapping(ctx, addr2, e2)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC721Approved(ctx, addr1.String(), contractAddr.String(), "1")
	require.Nil(t, err)
	require.NotEmpty(t, res2)
}

func TestHandleERC721IsApprovedForAll(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc721/DummyERC721.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	addr2, e2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	k.SetAddressMapping(ctx, addr2, e2)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC721IsApprovedForAll(ctx, addr1.String(), contractAddr.String(), addr2.String(), addr2.String())
	require.Nil(t, err)
	require.NotEmpty(t, res2)
}

func TestHandleERC721TotalSupply(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc721/DummyERC721.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC721TotalSupply(ctx, addr1.String(), contractAddr.String())
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	require.Equal(t, string(res2), "{\"supply\":\"101\"}")
}

func TestHandleERC721NameSymbol(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc721/DummyERC721.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC721NameSymbol(ctx, addr1.String(), contractAddr.String())
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	require.Equal(t, string(res2), "{\"name\":\"DummyERC721\",\"symbol\":\"DUMMY\"}")
}

func TestHandleERC721TokenURI(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc721/DummyERC721.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC721Uri(ctx, addr1.String(), contractAddr.String(), "1")
	require.Nil(t, err)
	require.NotEmpty(t, res2)
}

func TestHandleERC721RoyaltyInfo(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc721/DummyERC721.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	value := sdk.NewInt(100)
	res2, err := h.HandleERC721RoyaltyInfo(ctx, addr1.String(), contractAddr.String(), "1", &value)
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	match, _ := regexp.MatchString(`{"receiver":"sei\w{39}","royalty_amount":"5"}`, string(res2))
	require.True(t, match)
}

// 1155
func TestERC1155TransferPayload(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	addr1, e1 := testkeeper.MockAddressPair()
	addr2, e2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	k.SetAddressMapping(ctx, addr2, e2)
	h := wasm.NewEVMQueryHandler(k)
	value := sdk.NewInt(5)
	res, err := h.HandleERC1155TransferPayload(ctx, addr1.String(), addr2.String(), "1", &value)
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestERC1155BatchTransferPayload(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	addr1, e1 := testkeeper.MockAddressPair()
	addr2, e2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	k.SetAddressMapping(ctx, addr2, e2)
	h := wasm.NewEVMQueryHandler(k)
	value := sdk.NewInt(5)
	res, err := h.HandleERC1155BatchTransferPayload(ctx, addr1.String(), addr2.String(), []string{"0", "1", "2"}, []*sdk.Int{&value, &value, &value})
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestERC1155ApproveAllPayload(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	h := wasm.NewEVMQueryHandler(k)
	res, err := h.HandleERC1155SetApprovalAllPayload(ctx, addr1.String(), true)
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestHandleERC1155IsApprovedForAll(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc1155/ERC1155Example.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	addr2, e2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	k.SetAddressMapping(ctx, addr2, e2)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC1155IsApprovedForAll(ctx, addr1.String(), contractAddr.String(), addr2.String(), addr2.String())
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	require.Equal(t, `{"is_approved":false}`, string(res2))
}

func TestHandleERC1155BalanceOf(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc1155/ERC1155Example.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC1155BalanceOf(ctx, addr1.String(), contractAddr.String(), addr1.String(), "1")
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	match, _ := regexp.MatchString(`{"balance":"\d+"}`, string(res2))
	require.True(t, match)
}

func TestHandleERC1155BalanceOfBatch(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc1155/ERC1155Example.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC1155BalanceOfBatch(ctx, addr1.String(), contractAddr.String(), []string{addr1.String(), addr1.String()}, []string{"1", "2"})
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	match, _ := regexp.MatchString(`{"balances":\["\d+","\d+"\]}`, string(res2))
	require.True(t, match)
}

func TestHandleERC1155TotalSupply(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc1155/ERC1155Example.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC1155TotalSupply(ctx, addr1.String(), contractAddr.String())
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	require.Equal(t, "{\"supply\":\"150\"}", string(res2))
}

func TestHandleERC1155TotalSupplyForToken(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc1155/ERC1155Example.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC1155TotalSupplyForToken(ctx, addr1.String(), contractAddr.String(), "4")
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	require.Equal(t, "{\"supply\":\"24\"}", string(res2))
}

func TestHandleERC1155TokenExists(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc1155/ERC1155Example.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC1155TokenExists(ctx, addr1.String(), contractAddr.String(), "4")
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	require.Equal(t, "{\"exists\":true}", string(res2))
	res3, err := h.HandleERC1155TokenExists(ctx, addr1.String(), contractAddr.String(), "10")
	require.Nil(t, err)
	require.NotEmpty(t, res3)
	require.Equal(t, "{\"exists\":false}", string(res3))
}

func TestHandleERC1155NameSymbol(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc1155/ERC1155Example.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC1155NameSymbol(ctx, addr1.String(), contractAddr.String())
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	require.Equal(t, "{\"name\":\"DummyERC1155\",\"symbol\":\"DUMMY\"}", string(res2))
}

func TestHandleERC1155Uri(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc1155/ERC1155Example.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	res2, err := h.HandleERC1155Uri(ctx, addr1.String(), contractAddr.String(), "1")
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	require.Equal(t, "{\"uri\":\"https://example.com/{id}\"}", string(res2))
}

func TestHandleERC1155RoyaltyInfo(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	res, _ := deployContract(t, ctx, k, "../../../../example/contracts/erc1155/ERC1155Example.bin", privKey)
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	h := wasm.NewEVMQueryHandler(k)
	value := sdk.NewInt(100)
	res2, err := h.HandleERC1155RoyaltyInfo(ctx, addr1.String(), contractAddr.String(), "1", &value)
	require.Nil(t, err)
	require.NotEmpty(t, res2)
	match, _ := regexp.MatchString(`{"receiver":"sei\w{39}","royalty_amount":"5"}`, string(res2))
	require.True(t, match)
}

func TestGetAddress(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
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

type mockTx struct {
	msgs    []sdk.Msg
	signers []sdk.AccAddress
}

func (tx mockTx) GetMsgs() []sdk.Msg                              { return tx.msgs }
func (tx mockTx) ValidateBasic() error                            { return nil }
func (tx mockTx) GetSigners() []sdk.AccAddress                    { return tx.signers }
func (tx mockTx) GetPubKeys() ([]cryptotypes.PubKey, error)       { return nil, nil }
func (tx mockTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return nil, nil }
func (tx mockTx) GetGasEstimate() uint64                          { return 0 }

func deployContract(t *testing.T, ctx sdk.Context, k *keeper.Keeper, path string, privKey cryptotypes.PrivKey) (*evmtypes.MsgEVMTransactionResponse, evmtypes.MsgServer) {
	code, err := os.ReadFile(path)
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      4000000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     bz,
		Nonce:    0,
	}
	chainID := k.ChainID(ctx)
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)

	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(100000000)))
	k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(100000000))))
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, evmAddr[:], amt)

	msgServer := keeper.NewMsgServerImpl(k)

	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)

	require.NoError(t, k.FlushTransientReceipts(ctx))
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	if receipt.Status != 1 {
		t.Fatalf("receipt status is not 1, got %d, vmerror = %s", receipt.Status, receipt.VmError)
	}
	return res, msgServer
}

func createSigner(k *keeper.Keeper, ctx sdk.Context) ethtypes.Signer {
	chainID := k.ChainID(ctx)
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	return signer
}

func TestHandleStaticCall(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	privKey := testkeeper.MockPrivateKey()
	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	signer := createSigner(k, ctx)
	res, msgServer := deployContract(t, ctx, k, "../../../../example/contracts/simplestorage/SimpleStorage.bin", privKey)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NotEmpty(t, res.ReturnData)
	require.NotEmpty(t, res.Hash)
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)

	// send transaction to the contract
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	bz, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       &contractAddr,
		Value:    big.NewInt(0),
		Data:     bz,
		Nonce:    1,
	}
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)
	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)

	abibz, err := os.ReadFile("../../../../example/contracts/simplestorage/SimpleStorage.abi")
	require.Nil(t, err)
	parsedABI, err := ethabi.JSON(strings.NewReader(string(abibz)))
	require.Nil(t, err)
	input, err := parsedABI.Pack("get")
	require.Nil(t, err)
	output, err := wasm.NewEVMQueryHandler(k).HandleStaticCall(ctx, sdk.AccAddress(evmAddr[:]).String(), contractAddr.Hex(), input)
	require.Nil(t, err)
	unmarshaledOutput := &bindings.StaticCallResponse{}
	require.Nil(t, json.Unmarshal(output, unmarshaledOutput))
	outputbz, err := base64.StdEncoding.DecodeString(unmarshaledOutput.EncodedData)
	require.Nil(t, err)
	decoded, err := parsedABI.Unpack("get", outputbz)
	require.Nil(t, err)
	require.Equal(t, big.NewInt(20), decoded[0].(*big.Int))
}
