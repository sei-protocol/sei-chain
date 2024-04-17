package wasm_test

import (
	"encoding/hex"
	"encoding/json"

	// "math/big"
	"os"
	"testing"

	"github.com/cosmos/cosmos-sdk/types"
	// sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	// ethtypes "github.com/ethereum/go-ethereum/core/types"
	// "github.com/ethereum/go-ethereum/crypto"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/client/wasm"
	"github.com/sei-protocol/sei-chain/x/evm/client/wasm/bindings"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"

	// evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	// "github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func deployERC20ToAddr(t *testing.T, ctx types.Context, k *keeper.Keeper, to common.Address) {
	code, err := os.ReadFile("../../../../example/contracts/erc20/ERC20.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	k.SetCode(ctx, to, bz)
	// privKey := testkeeper.MockPrivateKey()
	// testPrivHex := hex.EncodeToString(privKey.Bytes())
	// key, _ := crypto.HexToECDSA(testPrivHex)
	// txData := ethtypes.LegacyTx{
	// 	GasPrice: big.NewInt(1000000000000),
	// 	Gas:      500000,
	// 	To:       nil,
	// 	Value:    big.NewInt(0),
	// 	Data:     bz,
	// 	Nonce:    0,
	// }
	// chainID := k.ChainID()
	// chainCfg := evmtypes.DefaultChainConfig()
	// ethCfg := chainCfg.EthereumConfig(chainID)
	// blockNum := big.NewInt(ctx.BlockHeight())
	// signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	// tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	// require.Nil(t, err)
	// txwrapper, err := ethtx.NewLegacyTx(tx)
	// require.Nil(t, err)
	// req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	// require.Nil(t, err)

	// msgServer := keeper.NewMsgServerImpl(k)

	// // Deploy Simple Storage contract
	// // ante.Preprocess(ctx, req)
	// // ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
	// // 	return ctx, nil
	// // })
	// require.Nil(t, err)
	// res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	// require.Nil(t, err)
	// // require.LessOrEqual(t, res.GasUsed, uint64(200000))
	// // require.Empty(t, res.VmError)
	// // require.NotEmpty(t, res.ReturnData)
	// // require.NotEmpty(t, res.Hash)
	// // require.Equal(t, uint64(1000000)-res.GasUsed, k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), "usei").Amount.Uint64())
	// // require.Equal(t, res.GasUsed, k.BankKeeper().GetBalance(ctx, state.GetCoinbaseAddress(ctx.TxIndex()), k.GetBaseDenom(ctx)).Amount.Uint64())
	// receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	// require.Nil(t, err)
	// require.NotNil(t, receipt)
	// require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)
}

func TestHandleStaticCall(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	from, _ := testkeeper.MockAddressPair()
	_, to := testkeeper.MockAddressPair()
	h := wasm.NewEVMQueryHandler(k)
	deployERC20ToAddr(t, ctx, k, to)
	res, err := h.HandleStaticCall(ctx, from.String(), to.String(), []byte("123"))
	require.Nil(t, err)
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

// type KeeperDummy struct {

// }

// func (k *KeeperDummy) StaticCallEVM(ctx sdk.Context, from sdk.AccAddress, to *common.Address, data []byte) ([]byte, error) {
// 	return nil, nil
// }
// GetEVMAddress(ctx sdk.Context, addr sdk.AccAddress) (common.Address, bool)
// GetSeiAddressOrDefault(ctx sdk.Context, addr common.Address) sdk.AccAddress

// TODO: getting execution reverted
func TestHandleERC20TokenInfo(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	addr1, contract := testkeeper.MockAddressPair()
	caller, sei2 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, contract)
	k.SetAddressMapping(ctx, caller, sei2)
	h := wasm.NewEVMQueryHandler(k)
	// deployERC20ToAddr(t, ctx, k, contract)
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

// TODO: getting execution reverted
func TestHandleERC20Balance(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	_, contractAddr := testkeeper.MockAddressPair()
	addr, seiAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr, seiAddr)
	deployERC20ToAddr(t, ctx, k, contractAddr)
	h := wasm.NewEVMQueryHandler(k)
	res, err := h.HandleERC20Balance(ctx, contractAddr.String(), addr.String())
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

// TODO: getting execution reverted
func TestHandleERC721Owner(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	caller, _ := testkeeper.MockAddressPair()
	_, contractAddr := testkeeper.MockAddressPair()
	h := wasm.NewEVMQueryHandler(k)
	deployERC20ToAddr(t, ctx, k, contractAddr)
	res, err := h.HandleERC721Owner(ctx, caller.String(), contractAddr.String(), "1")
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

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
