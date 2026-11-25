package undolog

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

var (
	alice    = common.HexToAddress("0xa000000000000000000000000000000000000000")
	contract = common.HexToAddress("0x000000000000000000000000000000000000dead")
)

func nonceValue(nonce uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, nonce)
}

func evmKey(kind keys.EVMKeyKind, parts ...[]byte) string {
	return string(keys.BuildEVMKey(kind, bytes.Join(parts, nil)))
}

// collect returns each mutation's previous value keyed by its logical key, and fails on a key returned twice.
func collect(t *testing.T, changes *gigatypes.StateChangeSet) map[string][]byte {
	t.Helper()
	mutations := changeSetMutations(changes)
	require.Len(t, mutations, mutationCount(changes))
	priors := map[string][]byte{}
	for _, m := range mutations {
		_, dup := priors[string(m.Key)]
		require.False(t, dup, "key %x returned twice", m.Key)
		priors[string(m.Key)] = m.PreviousValue
	}
	return priors
}

func TestChangeSetMutationsCoverEveryFamily(t *testing.T) {
	slot := common.Hash{31: 1}
	code := []byte{0x60, 0x00}
	changes := &gigatypes.StateChangeSet{
		Accounts: []gigatypes.AccountChange{{
			Address: alice,
			Prior:   gigatypes.AccountState{Balance: big.NewInt(5), Nonce: 2},
			Account: gigatypes.AccountState{Balance: big.NewInt(6), Nonce: 3},
		}, {
			Address: contract,
			Prior:   gigatypes.AccountState{Code: code},
			Account: gigatypes.AccountState{Code: []byte{0x60, 0x01}},
		}},
		Storage: []gigatypes.StorageChange{{Address: contract, Key: slot, PriorValue: common.Hash{31: 7}, Value: common.Hash{31: 8}}},
	}
	require.Equal(t, map[string][]byte{
		evmKey(keys.EVMKeyBalance, alice[:]):             common.Hash{31: 5}.Bytes(),
		evmKey(keys.EVMKeyNonce, alice[:]):               nonceValue(2),
		evmKey(keys.EVMKeyCodeHash, contract[:]):         crypto.Keccak256(code),
		evmKey(keys.EVMKeyCode, contract[:]):             code,
		evmKey(keys.EVMKeyStorage, contract[:], slot[:]): common.Hash{31: 7}.Bytes(),
	}, collect(t, changes), "a code change records its code hash too")
}

// A field the block left alone has no mutation, and a zero prior is no value, except for code, which is
// none only when empty.
func TestChangeSetMutationsCoverOnlyChangedFieldsAndAbsentZeros(t *testing.T) {
	slot := common.Hash{31: 1}
	changes := &gigatypes.StateChangeSet{
		Accounts: []gigatypes.AccountChange{{
			Address: alice,
			Prior:   gigatypes.AccountState{Balance: big.NewInt(9), Nonce: 0},
			Account: gigatypes.AccountState{Balance: big.NewInt(9), Nonce: 1, Code: []byte{0x00}},
		}, {
			Address: contract,
			Prior:   gigatypes.AccountState{Code: []byte{0x00}},
			Account: gigatypes.AccountState{Balance: big.NewInt(1), Code: []byte{0x00, 0x00}},
		}},
		Storage: []gigatypes.StorageChange{{Address: contract, Key: slot, Value: common.Hash{31: 1}}},
	}
	require.Equal(t, map[string][]byte{
		evmKey(keys.EVMKeyNonce, alice[:]):               nil,
		evmKey(keys.EVMKeyCodeHash, alice[:]):            nil,
		evmKey(keys.EVMKeyCode, alice[:]):                nil,
		evmKey(keys.EVMKeyBalance, contract[:]):          nil,
		evmKey(keys.EVMKeyCodeHash, contract[:]):         crypto.Keccak256([]byte{0x00}),
		evmKey(keys.EVMKeyCode, contract[:]):             {0x00},
		evmKey(keys.EVMKeyStorage, contract[:], slot[:]): nil,
	}, collect(t, changes))
}

func TestNilChangeSetHasNoMutations(t *testing.T) {
	require.Nil(t, changeSetMutations(nil))
}
