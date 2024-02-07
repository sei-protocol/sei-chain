package gov_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/precompiles/gov"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestVoteDeposit(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	content := govtypes.ContentFromProposalType("title", "description", govtypes.ProposalTypeText, false)
	proposal, err := testApp.GovKeeper.SubmitProposal(ctx, content)
	require.Nil(t, err)
	k := &testApp.EvmKeeper
	abi := gov.GetABI()

	// deposit
	args, err := abi.Pack("deposit", proposal.ProposalId, big.NewInt(10000000))
	require.Nil(t, err)

	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	addr := common.HexToAddress(gov.GovAddress)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      20000000,
		To:       &addr,
		Value:    big.NewInt(0),
		Data:     args,
		Nonce:    0,
	}
	chainID := k.ChainID(ctx)
	evmParams := k.GetParams(ctx)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)

	_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
	amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
	require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, evmAddr[:], amt))

	msgServer := keeper.NewMsgServerImpl(k)

	ante.Preprocess(ctx, req, k.GetParams(ctx))
	res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
	require.Nil(t, err)
	require.Empty(t, res.VmError)

	proposal, _ = testApp.GovKeeper.GetProposal(ctx, proposal.ProposalId)
	require.Equal(t, govtypes.StatusVotingPeriod, proposal.Status)

	// vote
	for _, opt := range []govtypes.VoteOption{govtypes.OptionYes, govtypes.OptionNo, govtypes.OptionAbstain} {
		args, err := abi.Pack("vote", proposal.ProposalId, opt)
		require.Nil(t, err)

		privKey := testkeeper.MockPrivateKey()
		testPrivHex := hex.EncodeToString(privKey.Bytes())
		key, _ := crypto.HexToECDSA(testPrivHex)
		addr := common.HexToAddress(gov.GovAddress)
		txData := ethtypes.LegacyTx{
			GasPrice: big.NewInt(1000000000000),
			Gas:      20000000,
			To:       &addr,
			Value:    big.NewInt(0),
			Data:     args,
			Nonce:    0,
		}
		chainID := k.ChainID(ctx)
		evmParams := k.GetParams(ctx)
		chainCfg := evmParams.GetChainConfig()
		ethCfg := chainCfg.EthereumConfig(chainID)
		blockNum := big.NewInt(ctx.BlockHeight())
		signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
		tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
		require.Nil(t, err)
		txwrapper, err := ethtx.NewLegacyTx(tx)
		require.Nil(t, err)
		req, err := types.NewMsgEVMTransaction(txwrapper)
		require.Nil(t, err)

		_, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
		amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
		require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))))
		require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, evmAddr[:], amt))

		msgServer := keeper.NewMsgServerImpl(k)

		ante.Preprocess(ctx, req, k.GetParams(ctx))
		res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
		require.Nil(t, err)
		require.Empty(t, res.VmError)

		v, found := testApp.GovKeeper.GetVote(ctx, proposal.ProposalId, evmAddr[:])
		require.True(t, found)
		require.Equal(t, 1, len(v.Options))
		require.Equal(t, opt, v.Options[0].Option)
		require.Equal(t, sdk.OneDec(), v.Options[0].Weight)
	}
}
