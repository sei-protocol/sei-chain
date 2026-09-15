package evmonlyapp

import (
	"crypto/ecdsa"
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

const evmOnlyTestChainID uint64 = 713715

func signedEVMOnlyTestTx(t *testing.T, chainID uint64, nonce uint64) ([]byte, common.Address) {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	return signedEVMOnlyTestTxFrom(t, key, chainID, nonce), crypto.PubkeyToAddress(key.PublicKey)
}

func signedEVMOnlyTestTxFrom(t *testing.T, key *ecdsa.PrivateKey, chainID uint64, nonce uint64) []byte {
	t.Helper()
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
	return raw
}

func evmOnlyTestInitChain() *abci.RequestInitChain {
	return &abci.RequestInitChain{
		InitialHeight: 1,
		ConsensusParams: &tmproto.ConsensusParams{
			Block: &tmproto.BlockParams{MaxGas: 30_000_000},
		},
	}
}

func newInitializedEVMOnlyTestApp(t *testing.T) abci.Application {
	t.Helper()
	app := newEVMOnlyTestApp(t, nil)
	_, err := app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	return app
}

func newEVMOnlyTestApp(t *testing.T, validators []abci.ValidatorUpdate) abci.Application {
	t.Helper()
	storage := openEVMOnlyTestStorage(t, t.TempDir())
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, validators, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()))
	require.NoError(t, err)
	return app
}

func openEVMOnlyTestStorage(t *testing.T, home string) *bootstrap.GigaStorageManager {
	t.Helper()
	storageConfig, err := evmonly.NewValidatorStorageConfig(home)
	require.NoError(t, err)
	storage, err := bootstrap.NewGigaStorageManager(t.Context(), storageConfig)
	require.NoError(t, err)
	return storage
}

// reopenEVMOnlyTestApp closes storage and constructs a fresh application over
// the same home, the way a restarted process does.
func reopenEVMOnlyTestApp(t *testing.T, storage *bootstrap.GigaStorageManager, home string) (abci.Application, *bootstrap.GigaStorageManager) {
	t.Helper()
	require.NoError(t, storage.Close())
	reopened := openEVMOnlyTestStorage(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, reopened, evmonly.NewFlatKVChangeSetEncoder(reopened.SC()))
	require.NoError(t, err)
	return app, reopened
}

func evmOnlyTestBlock(height int64, txs ...[]byte) *abci.RequestFinalizeBlock {
	return &abci.RequestFinalizeBlock{
		Txs:  txs,
		Hash: crypto.Keccak256(binary.BigEndian.AppendUint64([]byte("block-"), uint64(height))), //nolint:gosec // G115: test heights are positive.
		Header: &tmproto.Header{
			Height: height,
			Time:   time.Unix(1_700_000_000+height, 0),
		},
	}
}

func finalizeAndCommitEVMOnlyTestBlock(t *testing.T, app abci.Application, req *abci.RequestFinalizeBlock) []byte {
	t.Helper()
	response, err := app.FinalizeBlock(t.Context(), req)
	require.NoError(t, err)
	_, err = app.Commit(t.Context())
	require.NoError(t, err)
	return response.AppHash
}

func TestEVMOnlyApplicationExecutesRawEthereumBlock(t *testing.T) {
	home := t.TempDir()
	storage := openEVMOnlyTestStorage(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()))
	require.NoError(t, err)
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
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

	// Receipt writes are queued behind the block; closing the storage drains
	// them, so the reopened store is where the receipt is guaranteed to be.
	_, storage = reopenEVMOnlyTestApp(t, storage, home)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	receiptCtx := sdk.NewContext(nil, tmproto.Header{Height: 1}, false).WithContext(t.Context())
	receipt, err := storage.ReceiptDB().GetReceipt(receiptCtx, tx.Hash())
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

// A zero priority fee is valid EIP-1559, and admitting on tx.GasPrice() — the
// fee cap on a dynamic-fee tx — let one through that the executor then refused.
// An executor refusal is a node panic rather than a failed receipt, so this has
// to be caught here.
func TestEVMOnlyApplicationRefusesATxTheExecutorWouldRefuse(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	chainID := new(big.Int).SetUint64(evmOnlyTestChainID)
	// Fee cap far above the minimum, tip cap zero. At a zero base fee the
	// effective gas price is the tip, so block validity refuses it.
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: new(big.Int),
		GasFeeCap: big.NewInt(100 * evmOnlyMinGasPrice),
		Gas:       21_000,
		To:        &recipient,
		Value:     big.NewInt(1),
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)

	response := app.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: raw})

	require.True(t, response.IsErr())
}

// A tip that clears the minimum still has to be admitted, or the fix has
// rejected every dynamic-fee transaction rather than the inexecutable ones.
func TestEVMOnlyApplicationAdmitsADynamicFeeTxThatClearsTheMinimum(t *testing.T) {
	app := newInitializedEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	chainID := new(big.Int).SetUint64(evmOnlyTestChainID)
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: big.NewInt(evmOnlyMinGasPrice),
		GasFeeCap: big.NewInt(100 * evmOnlyMinGasPrice),
		Gas:       21_000,
		To:        &recipient,
		Value:     big.NewInt(1),
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)

	response := app.CheckTx(t.Context(), &abci.RequestCheckTxV2{Tx: raw})

	require.True(t, response.IsOK())
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

// A restarted node must resume from the height and app hash its storage holds,
// and continue executing without an InitChain. The reference app runs the same
// blocks without restarting, so the resumed chain has to match it hash for hash.
func TestEVMOnlyApplicationResumesFromStorageAfterRestart(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	const blocks = 3
	block := func(height int64) *abci.RequestFinalizeBlock {
		return evmOnlyTestBlock(height, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, uint64(height-1))) //nolint:gosec // G115: test heights are positive.
	}

	reference := newInitializedEVMOnlyTestApp(t)
	wantHashes := make([][]byte, 0, blocks+1)
	for height := range int64(blocks + 1) {
		wantHashes = append(wantHashes, finalizeAndCommitEVMOnlyTestBlock(t, reference, block(height+1)))
	}

	home := t.TempDir()
	storage := openEVMOnlyTestStorage(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()))
	require.NoError(t, err)
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	for height := range int64(blocks) {
		require.Equal(t, wantHashes[height], finalizeAndCommitEVMOnlyTestBlock(t, app, block(height+1)))
	}

	app, storage = reopenEVMOnlyTestApp(t, storage, home)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	info := app.Info()
	require.Equal(t, int64(blocks), info.LastBlockHeight)
	require.Equal(t, wantHashes[blocks-1], info.LastBlockAppHash)
	require.Equal(t, uint64(blocks), app.EvmNonce(sender))
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.Error(t, err)
	require.Equal(t, wantHashes[blocks], finalizeAndCommitEVMOnlyTestBlock(t, app, block(blocks+1)))
	require.Equal(t, int64(blocks+1), app.LastBlockHeight())
}

// State is committed by FinalizeBlock, so a crash before Commit leaves the
// finalized block durable. The restarted node must report it rather than
// execute it a second time.
func TestEVMOnlyApplicationResumesFromBlockFinalizedButNotCommitted(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	block := func(height int64) *abci.RequestFinalizeBlock {
		return evmOnlyTestBlock(height, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, uint64(height-1))) //nolint:gosec // G115: test heights are positive.
	}

	home := t.TempDir()
	storage := openEVMOnlyTestStorage(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()))
	require.NoError(t, err)
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	finalizeAndCommitEVMOnlyTestBlock(t, app, block(1))
	finalized, err := app.FinalizeBlock(t.Context(), block(2))
	require.NoError(t, err)

	app, storage = reopenEVMOnlyTestApp(t, storage, home)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	info := app.Info()
	require.Equal(t, int64(2), info.LastBlockHeight)
	require.Equal(t, finalized.AppHash, info.LastBlockAppHash)
	finalizeAndCommitEVMOnlyTestBlock(t, app, block(3))
	require.Equal(t, int64(3), app.LastBlockHeight())
}

// InitChain with an initial height above one seeds the store before any block
// exists. Crashing there must still allow InitChain to run again.
func TestEVMOnlyApplicationRepeatsInitChainAfterSeedingOnly(t *testing.T) {
	home := t.TempDir()
	storage := openEVMOnlyTestStorage(t, home)
	app, err := NewEVMOnlyApplication(evmOnlyTestChainID, nil, storage, evmonly.NewFlatKVChangeSetEncoder(storage.SC()))
	require.NoError(t, err)
	init := evmOnlyTestInitChain()
	init.InitialHeight = 5
	_, err = app.InitChain(init)
	require.NoError(t, err)

	app, storage = reopenEVMOnlyTestApp(t, storage, home)
	t.Cleanup(func() { require.NoError(t, storage.Close()) })

	require.Equal(t, int64(0), app.Info().LastBlockHeight)
	_, err = app.InitChain(init)
	require.NoError(t, err)
	require.Equal(t, int64(4), app.Info().LastBlockHeight)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(5))
	require.Equal(t, int64(5), app.LastBlockHeight())
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
