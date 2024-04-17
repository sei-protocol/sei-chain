package wasm_test

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
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
	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

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
	value := sdk.NewInt(500)
	res, err := h.HandleERC20TransferPayload(ctx, addr1.String(), &value)
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
	value := sdk.NewInt(500)
	res, err := h.HandleERC20TransferFromPayload(ctx, addr1.String(), addr2.String(), &value)
	require.Nil(t, err)
	require.NotEmpty(t, res)
}

func TestERC20ApprovePayload(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	addr1, e1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, addr1, e1)
	h := wasm.NewEVMQueryHandler(k)
	value := sdk.NewInt(500)
	res, err := h.HandleERC20ApprovePayload(ctx, addr1.String(), &value)
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

type mockTx struct {
	msgs    []sdk.Msg
	signers []sdk.AccAddress
}

func (tx mockTx) GetMsgs() []sdk.Msg                              { return tx.msgs }
func (tx mockTx) ValidateBasic() error                            { return nil }
func (tx mockTx) GetSigners() []sdk.AccAddress                    { return tx.signers }
func (tx mockTx) GetPubKeys() ([]cryptotypes.PubKey, error)       { return nil, nil }
func (tx mockTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return nil, nil }

func TestHandleStaticCall(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	code, err := os.ReadFile("../../../../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     bz,
		Nonce:    0,
	}
	chainID := k.ChainID()
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000)))
	k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000))))
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, evmAddr[:], amt)

	msgServer := keeper.NewMsgServerImpl(k)

	// Deploy Simple Storage contract
	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.LessOrEqual(t, res.GasUsed, uint64(200000))
	require.Empty(t, res.VmError)
	require.NotEmpty(t, res.ReturnData)
	require.NotEmpty(t, res.Hash)
	require.Equal(t, uint64(1000000)-res.GasUsed, k.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), "usei").Amount.Uint64())
	require.Equal(t, res.GasUsed, k.BankKeeper().GetBalance(ctx, state.GetCoinbaseAddress(ctx.TxIndex()), k.GetBaseDenom(ctx)).Amount.Uint64())
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	require.Nil(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), receipt.Status)

	// send transaction to the contract
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	bz, err = abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	txData = ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       &contractAddr,
		Value:    big.NewInt(0),
		Data:     bz,
		Nonce:    1,
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err = evmtypes.NewMsgEVMTransaction(txwrapper)
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
