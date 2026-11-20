package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestPrepareDecodedBlock(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := testAddress(1)
	raw := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
	tx := decodeTx(t, raw)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	executor := NewExecutor(Config{ParseWorkers: 2})
	defer executor.Close()
	for _, senders := range [][]utils.Option[common.Address]{nil, {utils.Some(sender)}} {
		prepared, err := executor.PrepareDecodedBlock(t.Context(), blockContext(chainID), []*ethtypes.Transaction{tx}, senders)
		require.NoError(t, err)
		require.Len(t, prepared.Txs, 1)
		require.Same(t, tx, prepared.Txs[0].Tx)
		require.Equal(t, sender, prepared.Txs[0].Sender)
	}
	_, err = executor.PrepareDecodedBlock(t.Context(), blockContext(big.NewInt(testChainID+1)), []*ethtypes.Transaction{tx}, []utils.Option[common.Address]{utils.Some(sender)})
	require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
	_, err = executor.PrepareDecodedBlock(t.Context(), blockContext(chainID), []*ethtypes.Transaction{ethtypes.NewTx(&ethtypes.BlobTx{})}, nil)
	require.ErrorIs(t, err, errUnsupportedBlobTx)
	_, err = executor.PrepareDecodedBlock(t.Context(), blockContext(chainID), []*ethtypes.Transaction{tx}, []utils.Option[common.Address]{utils.Some(sender), utils.Some(sender)})
	require.Error(t, err)
}
