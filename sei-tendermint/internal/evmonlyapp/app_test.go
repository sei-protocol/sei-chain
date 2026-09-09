package evmonlyapp

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

const evmOnlyTestChainID uint64 = 713715

func signedEVMOnlyTestTx(t *testing.T, chainID uint64, nonce uint64) ([]byte, common.Address) {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(evmOnlyMinGasPrice),
		Gas:      21_000,
		To:       &recipient,
		Value:    big.NewInt(1),
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw, crypto.PubkeyToAddress(key.PublicKey)
}

func newInitializedEVMOnlyTestApp(t *testing.T) abci.Application {
	t.Helper()
	app := newEVMOnlyTestApp(t, nil)
	_, err := app.InitChain(&abci.RequestInitChain{
		InitialHeight: 1,
		ConsensusParams: &tmproto.ConsensusParams{
			Block: &tmproto.BlockParams{MaxGas: 30_000_000},
		},
	})
	require.NoError(t, err)
	return app
}

func newEVMOnlyTestApp(t *testing.T, validators []abci.ValidatorUpdate) abci.Application {
	t.Helper()
	storageConfig, err := seidbconfig.DefaultGigaStorageConfig(t.TempDir())
	require.NoError(t, err)
	storage, err := bootstrap.NewGigaStorageManager(t.Context(), storageConfig.WithFullNodeMode())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	return NewEVMOnlyApplication(evmOnlyTestChainID, validators, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()))
}

func TestEVMOnlyApplicationExecutesRawEthereumBlock(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	raw, sender := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	tx := new(ethtypes.Transaction)
	require.NoError(t, tx.UnmarshalBinary(raw))
	check := app.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: raw})
	require.True(t, check.IsOK())
	require.True(t, check.IsEVM)
	require.Equal(t, sender, check.EVMSenderAddress)
	require.Equal(t, uint64(0), app.EvmNonce(sender))
	gotBalance := app.EvmBalance(sender, nil)
	require.Equal(t, evmOnlyBaseBalance, gotBalance.ToBig())

	response, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Txs:  [][]byte{raw},
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	})
	require.NoError(t, err)
	require.Len(t, response.AppHash, common.HashLength)
	require.Len(t, response.TxResults, 1)
	require.Positive(t, response.TxResults[0].GasUsed)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), app.LastBlockHeight())
	require.Equal(t, uint64(1), app.EvmNonce(sender))
	wantBalance := new(big.Int).Sub(
		new(big.Int).Sub(new(big.Int).Set(evmOnlyBaseBalance), big.NewInt(1)),
		new(big.Int).Mul(big.NewInt(evmOnlyMinGasPrice), big.NewInt(response.TxResults[0].GasUsed)),
	)
	gotBalance = app.EvmBalance(sender, nil)
	require.Equal(t, wantBalance, gotBalance.ToBig())
	require.Equal(t, response.AppHash, app.Info().LastBlockAppHash)
	receiptCtx := sdk.NewContext(nil, tmproto.Header{Height: 1}, false).WithContext(t.Context())
	receipt, err := app.(*evmOnlyApplication).storage.ReceiptDB().GetReceipt(receiptCtx, tx.Hash())
	require.NoError(t, err)
	require.Equal(t, tx.Hash().Hex(), receipt.TxHashHex)
	require.Equal(t, uint64(1), receipt.BlockNumber)
}

func TestEVMOnlyApplicationRejectsWrongChain(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID+1, 0)

	response := app.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: raw})

	require.True(t, response.IsErr())
}

func TestEVMOnlyApplicationProducesDeterministicRoot(t *testing.T) {
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	request := &abci.RequestFinalizeBlock{
		Txs:  [][]byte{raw},
		Hash: crypto.Keccak256([]byte("same-block")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	}
	first := newInitializedEVMOnlyTestApp(t)
	second := newInitializedEVMOnlyTestApp(t)

	firstResponse, err := first.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)
	secondResponse, err := second.FinalizeBlock(t.Context(), request)
	require.NoError(t, err)

	require.Equal(t, firstResponse.AppHash, secondResponse.AppHash)
}

func TestEVMOnlyApplicationRequiresInitChain(t *testing.T) {
	app := newEVMOnlyTestApp(t, nil)

	_, err := app.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{
		Hash: crypto.Keccak256([]byte("block-1")),
		Header: &tmproto.Header{
			Height: 1,
			Time:   time.Unix(1_700_000_001, 0),
		},
	})

	require.Error(t, err)
}

func TestEVMOnlyApplicationReturnsConfiguredValidators(t *testing.T) {
	configured := []abci.ValidatorUpdate{{Power: 7}}
	app := newEVMOnlyTestApp(t, configured)
	configured[0].Power = 11

	first := app.GetValidators()
	require.Equal(t, []abci.ValidatorUpdate{{Power: 7}}, first)
	first[0].Power = 13
	require.Equal(t, []abci.ValidatorUpdate{{Power: 7}}, app.GetValidators())
}
