package evmonly

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

func TestExecutorEmptyBlock(t *testing.T) {
	executor := NewExecutor(Config{})

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{})

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecutorTransferTx(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := common.HexToAddress("0x00000000000000000000000000000000000000a1")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))

	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(7), nil)
	executor := NewExecutor(Config{}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})

	require.NoError(t, err)
	require.Len(t, result.Txs, 1)
	require.Len(t, result.Receipts, 1)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
	require.Equal(t, uint64(21_000), result.GasUsed)
	require.NotEmpty(t, result.ChangeSet.Balances)

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(7), state.GetBalance(recipient))
	require.Equal(t, uint64(1), state.GetNonce(sender))
}

func TestExecutorDynamicFeeTx(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := common.HexToAddress("0x00000000000000000000000000000000000000a2")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))

	rawTx := signDynamicFeeTx(t, key, chainID, 0, &recipient, big.NewInt(11), nil)
	executor := NewExecutor(Config{}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})

	require.NoError(t, err)
	require.Len(t, result.Txs, 1)
	require.Equal(t, uint8(ethtypes.DynamicFeeTxType), result.Receipts[0].Type)

	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(11), state.GetBalance(recipient))
}

func TestExecutorFinalisesAfterEachTx(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	contract := common.HexToAddress("0x00000000000000000000000000000000000000c1")
	beneficiary := common.HexToAddress("0x00000000000000000000000000000000000000b1")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(500_000_000_000_000))
	state.SetCode(contract, selfDestructCode(beneficiary))

	firstCall := signLegacyTx(t, key, chainID, 0, &contract, big.NewInt(0), nil)
	secondCall := signLegacyTx(t, key, chainID, 1, &contract, big.NewInt(5), nil)
	executor := NewExecutor(Config{
		ChainConfig: legacySelfDestructChainConfig(chainID),
	}, WithState(state))

	result, err := executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{firstCall, secondCall},
	})

	require.NoError(t, err)
	require.Len(t, result.Receipts, 2)

	state.ApplyChangeSet(result.ChangeSet)
	require.Empty(t, state.GetCode(contract))
	require.Equal(t, big.NewInt(5), state.GetBalance(contract))
	require.Equal(t, big.NewInt(0), state.GetBalance(beneficiary))
}

func TestPrepareClearsTransientStorage(t *testing.T) {
	stateDB := newNativeStateDB(NewMemoryState())
	addr := common.HexToAddress("0x00000000000000000000000000000000000000a3")
	key := common.HexToHash("0x01")
	value := common.HexToHash("0x02")

	stateDB.SetTransientState(addr, key, value)
	require.Equal(t, value, stateDB.GetTransientState(addr, key))

	stateDB.Prepare(params.Rules{}, addr, common.Address{}, nil, nil, nil)

	require.Equal(t, common.Hash{}, stateDB.GetTransientState(addr, key))
}

func TestSnapshotRevertRestoresBaseState(t *testing.T) {
	addr := common.HexToAddress("0x00000000000000000000000000000000000000a4")
	key := common.HexToHash("0x01")
	value := common.HexToHash("0x02")

	state := NewMemoryState()
	state.SetState(addr, key, value)
	stateDB := newNativeStateDB(state)
	stateDB.GetBalance(addr)

	snapshot := stateDB.Snapshot()
	require.Equal(t, value, stateDB.GetState(addr, key))
	stateDB.RevertToSnapshot(snapshot)

	require.Empty(t, stateDB.ChangeSet().Storage)
}

func TestFinaliseClearsRefund(t *testing.T) {
	stateDB := newNativeStateDB(NewMemoryState())
	stateDB.AddRefund(12)

	stateDB.Finalise(true)

	require.Zero(t, stateDB.GetRefund())
}

func TestExecutorCustomPrecompilePlaceholder(t *testing.T) {
	chainID := big.NewInt(713715)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	customAddr := common.HexToAddress("0x0000000000000000000000000000000000001001")

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(200_000_000_000_000))

	rawTx := signLegacyTx(t, key, chainID, 0, &customAddr, big.NewInt(0), []byte{0x01})
	executor := NewExecutor(Config{
		CustomPrecompiles: staticPrecompileRegistry{addr: customAddr},
	}, WithState(state))

	_, err = executor.ExecuteBlock(context.Background(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, precompiles.ErrCustomPrecompilesOpen))
}

func signLegacyTx(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int, nonce uint64, to *common.Address, value *big.Int, data []byte) []byte {
	t.Helper()
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      100_000,
		To:       to,
		Value:    value,
		Data:     data,
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw
}

func signDynamicFeeTx(t *testing.T, key *ecdsa.PrivateKey, chainID *big.Int, nonce uint64, to *common.Address, value *big.Int, data []byte) []byte {
	t.Helper()
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(1_000_000_000),
		Gas:       100_000,
		To:        to,
		Value:     value,
		Data:      data,
	})
	signed, err := ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw
}

func blockContext(chainID *big.Int) BlockContext {
	return BlockContext{
		Number:   1,
		Time:     1,
		GasLimit: 30_000_000,
		ChainID:  chainID,
		BaseFee:  big.NewInt(0),
		Coinbase: common.HexToAddress("0x00000000000000000000000000000000000000cb"),
	}
}

func legacySelfDestructChainConfig(chainID *big.Int) *params.ChainConfig {
	return &params.ChainConfig{
		ChainID:             chainID,
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      false,
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
	}
}

func selfDestructCode(beneficiary common.Address) []byte {
	code := append([]byte{0x73}, beneficiary.Bytes()...)
	return append(code, 0xff)
}

type staticPrecompileRegistry struct {
	addr common.Address
}

func (r staticPrecompileRegistry) Get(addr common.Address) (precompiles.Contract, bool) {
	return nil, addr == r.addr
}

func (r staticPrecompileRegistry) Addresses() []common.Address {
	return []common.Address{r.addr}
}
