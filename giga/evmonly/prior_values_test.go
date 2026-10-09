package evmonly

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// Every change a block reports carries the value the state held before the block, whether the block
// runs sequentially or under OCC.
func TestChangeSetCarriesPriorValues(t *testing.T) {
	chainID := big.NewInt(testChainID)
	token := testAddress(0xe1)
	from, to := testAddress(0xe2), testAddress(0xe3)
	fromSlot, toSlot := common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())

	const txCount = 6
	var rawTxs [][]byte
	var senders []common.Address
	for i := range txCount {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		senders = append(senders, crypto.PubkeyToAddress(key.PublicKey))
		target := common.BigToAddress(big.NewInt(int64(50_000 + i)))
		if i == 0 {
			target = token
		}
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, key, chainID, 3, &target, big.NewInt(int64(i)), nil, 200_000, big.NewInt(1)))
	}
	newState := func() *MemoryState {
		state := NewMemoryState()
		for i, sender := range senders {
			state.SetBalance(sender, big.NewInt(int64(1_000_000_000+i)))
			state.SetNonce(sender, 3)
		}
		state.SetCode(token, erc20TransferRuntime(fromSlot, toSlot, from, to, 7))
		state.SetState(token, fromSlot, common.BigToHash(big.NewInt(1000)))
		return state
	}

	var results []*BlockResult
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			before := newState()
			executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: workers}, withTestState(newState()))
			defer executor.Close()
			result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: rawTxs})
			require.NoError(t, err)
			require.Equal(t, workers > 1, result.OCCStats.Attempted)
			changes := result.ChangeSet

			require.Len(t, changes.Nonces, txCount)
			require.Len(t, changes.Storage, 2)
			for _, change := range changes.Balances {
				require.Equal(t, common.BigToHash(before.GetBalance(change.Address)), change.Prior, "balance of %s", change.Address)
			}
			for _, change := range changes.Nonces {
				require.Equal(t, before.GetNonce(change.Address), change.Prior, "nonce of %s", change.Address)
			}
			for _, change := range changes.Storage {
				require.Equal(t, before.GetState(change.Address, change.Key), change.Prior, "slot %s of %s", change.Key, change.Address)
			}
			results = append(results, result)
		})
	}
	require.Len(t, results, 2)
	require.Equal(t, results[0].ChangeSet, results[1].ChangeSet, "both paths report the same changes and priors")
}

// historyRecordingStore is a MemoryStore that keeps its history as an undo log would, recording the
// prior values each commit hands it.
type historyRecordingStore struct {
	*MemoryStore
	priors [][]*proto.KVPair
}

func (*historyRecordingStore) NeedsPriorValues() bool { return true }

func (s *historyRecordingStore) CommitStateChangesWithPrior(blockNum int64, changeset []*proto.NamedChangeSet, prior []*proto.KVPair) error {
	s.priors = append(s.priors, prior)
	return s.CommitStateChanges(blockNum, changeset)
}

// A store that keeps history as an undo log receives, with each block, the value every key the block
// changed held before it; a store that does not receives none.
func TestExecutorHandsPriorValuesToAHistoryStore(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xe9)
	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(1_000_000_000_000))
	state.SetNonce(sender, 4)

	store := &historyRecordingStore{MemoryStore: NewMemoryStore(state)}
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)}, WithStore(store, store.EncodeChangeSet))
	defer executor.Close()
	raw := signLegacyTxWithGasPrice(t, key, chainID, 4, &recipient, big.NewInt(7), nil, 21_000, big.NewInt(1))
	_, err = executor.ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: [][]byte{raw}})
	require.NoError(t, err)

	require.Len(t, store.priors, 1)
	got := map[string]string{}
	for _, pair := range store.priors[0] {
		value := "<absent>"
		if !pair.Delete {
			value = common.Bytes2Hex(pair.Value)
		}
		got[common.Bytes2Hex(pair.Key)] = value
	}
	balanceKey := func(addr common.Address) string {
		return common.Bytes2Hex(keys.BuildEVMKey(keys.EVMKeyBalance, addr[:]))
	}
	require.Equal(t, common.Bytes2Hex(common.BigToHash(big.NewInt(1_000_000_000_000)).Bytes()), got[balanceKey(sender)])
	require.Equal(t, "<absent>", got[balanceKey(recipient)])
	require.Equal(t, "0000000000000004", got[common.Bytes2Hex(keys.BuildEVMKey(keys.EVMKeyNonce, sender[:]))])
}
