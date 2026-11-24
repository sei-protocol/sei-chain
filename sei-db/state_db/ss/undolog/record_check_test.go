package undolog

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

func TestCheckRecordAcceptsWhatTheLiveStateStoreWrites(t *testing.T) {
	addr := gigatypes.Address{0x42}
	accountKey := ktype.BuildPhysicalEVMKey(ktype.EVMKeyAccount, addr[:])
	storageKey := ktype.BuildPhysicalEVMKey(keys.EVMKeyStorage, append(addr[:], make([]byte, 32)...))
	codeKey := ktype.BuildPhysicalEVMKey(keys.EVMKeyCode, addr[:])

	for name, record := range map[string]struct{ key, value []byte }{
		"compact account":          {accountKey, accountValue(1, 1)},
		"account with a code hash": {accountKey, vtype.NewAccountData().SetCodeHash(&vtype.CodeHash{1}).Serialize()},
		"absent account":           {accountKey, nil},
		"storage":                  {storageKey, vtype.NewStorageData().SetValue(&[32]byte{31: 7}).Serialize()},
		"code of any length":       {codeKey, vtype.NewCodeData().SetBytecode([]byte{0x60, 0x00}).Serialize()},
		"other module":             {ktype.BuildPhysicalKey("bank", []byte("supply")), []byte("any length")},
	} {
		require.NoError(t, checkRecord(record.key, record.value), name)
	}
}

func TestCheckRecordRefusesWhatTheLiveStateStoreCannotWrite(t *testing.T) {
	addr := gigatypes.Address{0x42}
	accountKey := ktype.BuildPhysicalEVMKey(ktype.EVMKeyAccount, addr[:])
	storageKey := ktype.BuildPhysicalEVMKey(keys.EVMKeyStorage, append(addr[:], make([]byte, 32)...))
	storageValue := vtype.NewStorageData().SetValue(&[32]byte{31: 7}).Serialize()
	storagePrefix, _ := keys.EVMKeyPrefixByte(keys.EVMKeyStorage)
	shortStorageKey := ktype.BuildPhysicalKey(keys.EVMStoreKey, []byte{storagePrefix, 0x01})
	balanceKey := ktype.BuildPhysicalKey(keys.EVMStoreKey, keys.BuildEVMKey(keys.EVMKeyBalance, addr[:]))
	addressMappingKey := ktype.BuildPhysicalKey(keys.EVMStoreKey, append([]byte{0x01}, addr[:]...))

	for name, record := range map[string]struct {
		key, value []byte
		err        string
	}{
		"no module prefix":           {[]byte("supply"), []byte{1}, "module prefix"},
		"absent, no module prefix":   {[]byte("supply"), nil, "module prefix"},
		"empty module":               {[]byte("/supply"), []byte{1}, "empty module"},
		"short storage key":          {shortStorageKey, storageValue, "well-formed"},
		"absent, short storage key":  {shortStorageKey, nil, "well-formed"},
		"long storage key":           {append(bytes.Clone(storageKey), 0), storageValue, "well-formed"},
		"separate balance key":       {balanceKey, make([]byte, vtype.BalanceLength), "well-formed"},
		"other EVM key":              {addressMappingKey, []byte("sei1..."), "well-formed"},
		"short account value":        {accountKey, accountValue(1, 1)[:40], "account value"},
		"bare balance as account":    {accountKey, make([]byte, vtype.BalanceLength), "account value"},
		"long storage value":         {storageKey, append(storageValue, 0), "storage value"},
		"bare slot as storage value": {storageKey, make([]byte, vtype.StorageValueLength), "storage value"},
	} {
		require.ErrorContains(t, checkRecord(record.key, record.value), record.err, name)
	}
}
