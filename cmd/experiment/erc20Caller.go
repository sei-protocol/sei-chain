package main

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/loadtest/contracts/evm/bindings/erc20"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

type mockTx struct {
	msgs    []sdk.Msg
	signers []sdk.AccAddress
}

func (tx mockTx) GetMsgs() []sdk.Msg                              { return tx.msgs }
func (tx mockTx) ValidateBasic() error                            { return nil }
func (tx mockTx) GetSigners() []sdk.AccAddress                    { return tx.signers }
func (tx mockTx) GetPubKeys() ([]cryptotypes.PubKey, error)       { return nil, nil }
func (tx mockTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return nil, nil }

func main() {
	// setup
	k, ctx := testkeeper.MockEVMKeeper()
	sei1, eth1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, sei1, eth1)

	// deploy the contract to the keeper
	code, err := os.ReadFile("example/contracts/erc20/ERC20.bin")
	checkErr(err)
	bz, err := hex.DecodeString(string(code))
	checkErr(err)
	// Deploy code by sending tx through the msg server
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
	chainCfg := types.DefaultChainConfig()

	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	checkErr(err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	checkErr(err)
	req, err := types.NewMsgEVMTransaction(txwrapper)
	checkErr(err)

	// fund the account
	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000)))
	k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(1000000))))
	k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, evmAddr[:], amt)

	msgServer := keeper.NewMsgServerImpl(k)

	ante.Preprocess(ctx, req)
	ctx, err = ante.NewEVMFeeCheckDecorator(k).AnteHandle(ctx, mockTx{msgs: []sdk.Msg{req}}, false, func(sdk.Context, sdk.Tx, bool) (sdk.Context, error) {
		return ctx, nil
	})
	checkErr(err)

	// Submit the transaction to the msgServer / keeper
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	checkErr(err)
	fmt.Println("tx response = ", res)

	// Get the contract address
	receipt, err := k.GetReceipt(ctx, common.HexToHash(res.Hash))
	checkErr(err)
	contractAddr := common.HexToAddress(receipt.ContractAddress)
	fmt.Println("contractAddr = ", contractAddr)

	// do a static call to the contract
	abi, err := erc20.Erc20MetaData.GetAbi()
	checkErr(err)
	bz, err = abi.Pack("totalSupply")
	checkErr(err)

	res2, err := k.StaticCallEVM(ctx, sei1, &contractAddr, bz)
	checkErr(err)
	fmt.Println("res2 = ", res2)
	unpacked, err := abi.Unpack("totalSupply", res2)
	checkErr(err)
	fmt.Println("totalSupply = ", unpacked[0].(*big.Int))
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}
