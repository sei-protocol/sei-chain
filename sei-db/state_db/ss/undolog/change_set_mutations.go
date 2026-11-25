package undolog

import (
	"encoding/binary"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// changeSetMutations returns a block's change set as the mutations the undo log records, each a logical EVM key
// with its previous value: one per key the block changed, and a code-hash and a code mutation per code change. A
// zero previous value is absent, except code, which is absent only when empty. A storage clear yields nothing.
func changeSetMutations(changes *gigatypes.StateChangeSet) []mutation.Mutation {
	if changes == nil {
		return nil
	}
	mutations := make([]mutation.Mutation, 0, mutationCount(changes))
	for i := range changes.Accounts {
		change := &changes.Accounts[i]
		addr, prior := change.Address[:], &change.Prior
		if change.BalanceChanged() {
			balance := make([]byte, common.HashLength)
			if prior.Balance != nil {
				prior.Balance.FillBytes(balance)
			}
			mutations = append(mutations, evmMutation(keys.EVMKeyBalance, addr, nonZero(balance)))
		}
		if change.NonceChanged() {
			nonce := binary.BigEndian.AppendUint64(nil, prior.Nonce)
			mutations = append(mutations, evmMutation(keys.EVMKeyNonce, addr, nonZero(nonce)))
		}
		if change.CodeChanged() {
			var code, codeHash []byte
			if len(prior.Code) != 0 {
				code, codeHash = prior.Code, crypto.Keccak256(prior.Code)
			}
			mutations = append(mutations,
				evmMutation(keys.EVMKeyCodeHash, addr, codeHash),
				evmMutation(keys.EVMKeyCode, addr, code))
		}
	}
	for i := range changes.Storage {
		change := &changes.Storage[i]
		key := slices.Concat(change.Address[:], change.Key[:])
		mutations = append(mutations, evmMutation(keys.EVMKeyStorage, key, nonZero(change.PriorValue[:])))
	}
	return mutations
}

// mutationCount returns how many mutations changeSetMutations returns for changes.
func mutationCount(changes *gigatypes.StateChangeSet) int {
	count := len(changes.Storage)
	for i := range changes.Accounts {
		change := &changes.Accounts[i]
		if change.BalanceChanged() {
			count++
		}
		if change.NonceChanged() {
			count++
		}
		if change.CodeChanged() {
			count += 2
		}
	}
	return count
}

// evmMutation returns the mutation of the logical EVM key of kind for keyBytes, holding prior as its previous value.
func evmMutation(kind keys.EVMKeyKind, keyBytes, prior []byte) mutation.Mutation {
	return mutation.Mutation{Key: keys.BuildEVMKey(kind, keyBytes), PreviousValue: prior}
}

// nonZero returns value, or nil when it is all zeros.
func nonZero(value []byte) []byte {
	for _, b := range value {
		if b != 0 {
			return value
		}
	}
	return nil
}
