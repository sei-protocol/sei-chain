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

// collect returns every pair priors yields, keyed by logical key, and fails on a key yielded twice.
func collect(t *testing.T, changes *gigatypes.StateChangeSet) map[string][]byte {
	t.Helper()
	priors := priorsOf(changes)
	pairs := map[string][]byte{}
	for key, prior := range priors.All() {
		_, dup := pairs[string(key)]
		require.False(t, dup, "key %x yielded twice", key)
		pairs[string(key)] = bytes.Clone(prior)
	}
	require.Len(t, pairs, priors.Len())
	return pairs
}

func TestChangeSetPriorsYieldEveryFamily(t *testing.T) {
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

// A field the block left alone yields nothing, and a zero prior is no value, except for code, which is
// none only when empty.
func TestChangeSetPriorsYieldOnlyChangedFieldsAndAbsentZeros(t *testing.T) {
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

func TestChangeSetPriorsStopWhenTheConsumerDoes(t *testing.T) {
	changes := &gigatypes.StateChangeSet{
		Storage: []gigatypes.StorageChange{{Address: contract, Key: common.Hash{1}}, {Address: contract, Key: common.Hash{2}}},
	}
	seen := 0
	for range priorsOf(changes).All() {
		seen++
		break
	}
	require.Equal(t, 1, seen)
}

func TestNilChangeSetHasNoPriors(t *testing.T) {
	require.Nil(t, priorsOf(nil))
}
