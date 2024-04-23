package evm_test

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/aclmapping/evm"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/suite"
)

type KeeperTestSuite struct {
	apptesting.KeeperTestHelper

	queryClient  types.QueryClient
	msgServer    types.MsgServer
	preprocessor sdk.AnteDecorator

	sender          *ecdsa.PrivateKey
	associatedAcc   common.Address
	unassociatedAcc common.Address
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

// Runs before each test case
func (suite *KeeperTestSuite) SetupTest() {
	suite.Setup()
}

// Explicitly only run once during setup
func (suite *KeeperTestSuite) PrepareTest() {
	pk := testkeeper.MockPrivateKey()
	key, err := crypto.HexToECDSA(hex.EncodeToString(pk.Bytes()))
	if err != nil {
		panic(err)
	}
	suite.sender = key
	_, suite.unassociatedAcc = testkeeper.MockAddressPair()
	seiAddr, associatedAcc := testkeeper.MockAddressPair()
	suite.App.EvmKeeper.SetAddressMapping(suite.Ctx, seiAddr, associatedAcc)
	suite.associatedAcc = associatedAcc
	suite.msgServer = keeper.NewMsgServerImpl(&suite.App.EvmKeeper)
	suite.preprocessor = ante.NewEVMPreprocessDecorator(&suite.App.EvmKeeper, &suite.App.AccountKeeper)
	amt := sdk.NewCoins(sdk.NewCoin(suite.App.EvmKeeper.GetBaseDenom(suite.Ctx), sdk.NewInt(1000000)))
	suite.App.EvmKeeper.BankKeeper().MintCoins(suite.Ctx, types.ModuleName, amt)
	suite.App.EvmKeeper.BankKeeper().SendCoinsFromModuleToAccount(suite.Ctx, types.ModuleName, sdk.AccAddress(pk.PubKey().Address()), amt)

	msgValidator := sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap)
	suite.Ctx = suite.Ctx.WithMsgValidator(msgValidator)
}

func cacheTxContext(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
	ms := ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	return ctx.WithMultiStore(msCache), msCache
}

func (suite *KeeperTestSuite) buildSendMsgTo(to common.Address, amt *big.Int) *types.MsgEVMTransaction {
	txData := ethtypes.DynamicFeeTx{
		Nonce:     0,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       30000,
		To:        &to,
		Value:     amt,
		Data:      []byte(""),
		ChainID:   suite.App.EvmKeeper.ChainID(suite.Ctx),
	}
	ethCfg := types.DefaultChainConfig().EthereumConfig(suite.App.EvmKeeper.ChainID(suite.Ctx))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(suite.Ctx.BlockHeight()), uint64(suite.Ctx.BlockTime().Unix()))
	tx := ethtypes.NewTx(&txData)
	tx, err := ethtypes.SignTx(tx, signer, suite.sender)
	if err != nil {
		panic(err)
	}
	typedTxData, err := ethtx.NewTxDataFromTx(tx)
	if err != nil {
		panic(err)
	}
	msg, err := types.NewMsgEVMTransaction(typedTxData)
	if err != nil {
		panic(err)
	}

	return msg
}

func (suite *KeeperTestSuite) TestMsgEVMTransaction() {
	suite.PrepareTest()

	tests := []struct {
		name string
		msg  *types.MsgEVMTransaction
	}{
		{
			name: "associated to",
			msg:  suite.buildSendMsgTo(suite.associatedAcc, big.NewInt(1000000000000)),
		},
		{
			name: "unassociated to",
			msg:  suite.buildSendMsgTo(suite.unassociatedAcc, big.NewInt(1000000000000)),
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			handlerCtx, cms := cacheTxContext(suite.Ctx)
			tx := suite.App.GetTxConfig().NewTxBuilder()
			err := tx.SetMsgs(tc.msg)
			suite.Require().Nil(err)
			ctx, err := suite.preprocessor.AnteHandle(handlerCtx, tx.GetTx(), false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil })
			suite.Require().Nil(err)
			cms.ResetEvents()
			_, err = suite.msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), tc.msg)
			suite.Require().Nil(err)

			dependencies, _ := evm.TransactionDependencyGenerator(
				suite.App.AccessControlKeeper,
				suite.App.EvmKeeper,
				handlerCtx,
				tc.msg,
			)

			missing := handlerCtx.MsgValidator().ValidateAccessOperations(dependencies, cms.GetEvents())
			suite.Require().Empty(missing)
		})
	}
}
