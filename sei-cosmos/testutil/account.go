package testutil

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type TestAccount struct {
	Name    string
	Address types.AccAddress
}

func CreateKeyringAccounts(t *testing.T, kr keyring.Keyring, num int) []TestAccount {
	accounts := make([]TestAccount, num)
	for i := range accounts {
		record, _, err := kr.NewMnemonic(
			fmt.Sprintf("key-%d", i),
			keyring.English,
			types.FullFundraiserPath,
			keyring.DefaultBIP39Passphrase,
			hd.Secp256k1)
		assert.NoError(t, err)

		addr := record.GetAddress()

		accounts[i] = TestAccount{Name: record.GetName(), Address: addr}
	}

	return accounts
}
