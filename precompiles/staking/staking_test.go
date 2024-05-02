package staking_test

import (
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	crptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestStaking(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	valPub1 := secp256k1.GenPrivKey().PubKey()
	valPub2 := secp256k1.GenPrivKey().PubKey()
	val := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub1)
	val2 := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub2)

	// delegate
	abi := staking.GetABI()
	args, err := abi.Pack("delegate", val.String())
	require.Nil(t, err)

	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	addr := common.HexToAddress(staking.StakingAddress)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(100_000_000_000_000),
		Data:     args,
		Nonce:    0,
	}
	chainID := k.ChainID(ctx)
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

	seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, seiAddr, evmAddr)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
	require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))

	msgServer := keeper.NewMsgServerImpl(k)

	ante.Preprocess(ctx, req)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	d, found := testApp.StakingKeeper.GetDelegation(ctx, seiAddr, val)
	require.True(t, found)
	require.Equal(t, int64(100), d.Shares.RoundInt().Int64())

	// redelegate
	args, err = abi.Pack("redelegate", val.String(), val2.String(), big.NewInt(50))
	require.Nil(t, err)
	txData = ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     args,
		Nonce:    1,
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err = evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	ante.Preprocess(ctx, req)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	d, found = testApp.StakingKeeper.GetDelegation(ctx, seiAddr, val)
	require.True(t, found)
	require.Equal(t, int64(50), d.Shares.RoundInt().Int64())

	// undelegate
	args, err = abi.Pack("undelegate", val.String(), big.NewInt(30))
	require.Nil(t, err)
	txData = ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     args,
		Nonce:    2,
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err = evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	ante.Preprocess(ctx, req)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	d, found = testApp.StakingKeeper.GetDelegation(ctx, seiAddr, val)
	require.True(t, found)
	require.Equal(t, int64(20), d.Shares.RoundInt().Int64())
}

func TestStakingError(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	valPub1 := secp256k1.GenPrivKey().PubKey()
	valPub2 := secp256k1.GenPrivKey().PubKey()
	val := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub1)
	val2 := setupValidator(t, ctx, testApp, stakingtypes.Unbonded, valPub2)

	abi := staking.GetABI()
	args, err := abi.Pack("undelegate", val.String(), big.NewInt(100))
	require.Nil(t, err)

	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	addr := common.HexToAddress(staking.StakingAddress)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     args,
		Nonce:    0,
	}
	chainID := k.ChainID(ctx)
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
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
	require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, evmAddr[:], amt))

	msgServer := keeper.NewMsgServerImpl(k)

	ante.Preprocess(ctx, req)
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.NotEmpty(t, res.VmError)

	// redelegate
	args, err = abi.Pack("redelegate", val.String(), val2.String(), big.NewInt(50))
	require.Nil(t, err)
	txData = ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     args,
		Nonce:    1,
	}
	tx, err = ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err = ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err = evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	ante.Preprocess(ctx, req)
	res, err = msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.NotEmpty(t, res.VmError)
}

func setupValidator(t *testing.T, ctx sdk.Context, a *app.App, bondStatus stakingtypes.BondStatus, valPub crptotypes.PubKey) sdk.ValAddress {
	valAddr := sdk.ValAddress(valPub.Address())
	bondDenom := a.StakingKeeper.GetParams(ctx).BondDenom
	selfBond := sdk.NewCoins(sdk.Coin{Amount: sdk.NewInt(100), Denom: bondDenom})

	err := a.BankKeeper.MintCoins(ctx, minttypes.ModuleName, selfBond)
	require.NoError(t, err)

	err = a.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, sdk.AccAddress(valAddr), selfBond)
	require.NoError(t, err)

	sh := teststaking.NewHelper(t, ctx, a.StakingKeeper)
	msg := sh.CreateValidatorMsg(valAddr, valPub, selfBond[0].Amount)
	sh.Handle(msg, true)

	val, found := a.StakingKeeper.GetValidator(ctx, valAddr)
	require.True(t, found)

	val = val.UpdateStatus(bondStatus)
	a.StakingKeeper.SetValidator(ctx, val)

	consAddr, err := val.GetConsAddr()
	require.NoError(t, err)

	signingInfo := slashingtypes.NewValidatorSigningInfo(
		consAddr,
		ctx.BlockHeight(),
		0,
		time.Unix(0, 0),
		false,
		0,
	)
	a.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, signingInfo)

	return valAddr
}
