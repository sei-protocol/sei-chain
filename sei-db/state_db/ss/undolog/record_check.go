package undolog

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// checkRecord returns an error for a key without a module prefix, an EVM key other than a well-formed account, code
// or storage key, or an EVM account or storage value of the wrong length.
func checkRecord(key, value []byte) error {
	module, innerKey, err := ktype.SplitModulePrefix(key)
	if err != nil {
		return err
	}
	if len(module) == 0 {
		return errors.New("empty module prefix")
	}
	if string(module) != keys.EVMStoreKey {
		return nil
	}
	kind, _ := keys.ParseEVMKey(innerKey)
	if kind != ktype.EVMKeyAccount && kind != keys.EVMKeyCode && kind != keys.EVMKeyStorage {
		return errors.New("not a well-formed EVM account, code or storage key")
	}
	if value == nil {
		return nil
	}
	switch kind {
	case ktype.EVMKeyAccount:
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
