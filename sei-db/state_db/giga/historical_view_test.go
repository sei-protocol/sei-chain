package giga

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// An account exists at a past height exactly when the state commit store would have held a row for
// it: when its balance, nonce or code hash is non-zero. A stored zero nonce alone is no account.
func TestHistoricalViewAccountExistsWhenAFieldIsNonZero(t *testing.T) {
	addr := common.Address{0x42}
	codeHash := common.Hash{0xc0}
	for _, tc := range []struct {
		name   string
		stored map[keys.EVMKeyKind][]byte
		want   gigatypes.Account
		exists bool
	}{
		{"nothing stored", nil, gigatypes.Account{}, false},
		{"zero nonce alone", map[keys.EVMKeyKind][]byte{keys.EVMKeyNonce: nonceValue(0)}, gigatypes.Account{}, false},
		{"balance", map[keys.EVMKeyKind][]byte{keys.EVMKeyBalance: common.Hash{31: 5}.Bytes(), keys.EVMKeyNonce: nonceValue(0)},
			gigatypes.Account{Balance: common.Hash{31: 5}, CodeHash: gigatypes.EmptyCodeHash}, true},
		{"nonce", map[keys.EVMKeyKind][]byte{keys.EVMKeyNonce: nonceValue(3)},
			gigatypes.Account{Nonce: 3, CodeHash: gigatypes.EmptyCodeHash}, true},
		{"code hash", map[keys.EVMKeyKind][]byte{keys.EVMKeyCodeHash: codeHash.Bytes()},
			gigatypes.Account{CodeHash: codeHash}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := map[string][]byte{}
			for kind, value := range tc.stored {
				stored[string(keys.BuildEVMKey(kind, addr[:]))] = value
			}
			view := &historicalView{
				height: 7,
				read: func(_ string, key []byte) ([]byte, error) {
					return stored[string(key)], nil
				},
				release: func() {},
			}
			account, exists := view.ReadAccount(addr)
			require.Equal(t, tc.exists, exists)
			require.Equal(t, tc.want, account)
			require.Equal(t, tc.exists, view.AccountExists(addr))
		})
	}
}
