package undolog

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// checkRecord returns an error for a key without a module prefix, or an EVM account or storage value of the wrong
// length.
func checkRecord(key, value []byte) error {
	module, innerKey, err := ktype.SplitModulePrefix(key)
	if err != nil {
		return err
	}
	if len(module) == 0 {
		return errors.New("empty module prefix")
	}
	if value == nil || string(module) != keys.EVMStoreKey {
		return nil
	}
	// Must classify keys exactly as the live state store routes them.
	switch kind, _ := keys.ParseEVMKey(innerKey); kind {
	case ktype.EVMKeyAccount, keys.EVMKeyCodeHash, keys.EVMKeyBalance:
		if _, err := vtype.ParseAccountRow(value); err != nil {
			return fmt.Errorf("account value: %w", err)
		}
	case keys.EVMKeyStorage:
		if _, err := vtype.ParseStorageRow(value); err != nil {
			return fmt.Errorf("storage value: %w", err)
		}
	}
	return nil
}
