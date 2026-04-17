package verifier

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// DecodedKV is a single memiavl-format (key, value) pair derived from a raw
// FlatKV exporter row. Account rows may decode into up to two entries
// (nonce + codehash), hence the slice return from DecodeFlatKVNode.
type DecodedKV struct {
	Module string // e.g. "evm"
	Key    []byte // memiavl-format key within the module
	Value  []byte
}

// DecodeFlatKVNode converts one raw-physical FlatKV exporter node into the
// memiavl-format entries it represents. Mirrors addFlatKVNodeToMap in
// sei-cosmos/storev2/rootmulti/flatkv_helpers_test.go but without a testing.T
// dependency, so it can be reused by the production oracle and by the offline
// acceptance CLI.
//
// Returns the decoded entries (typically 1, up to 2 for account rows) and an
// error if the row is malformed.
func DecodeFlatKVNode(node *sctypes.SnapshotNode) ([]DecodedKV, error) {
	if node == nil {
		return nil, fmt.Errorf("nil snapshot node")
	}

	moduleName, innerKey, err := ktype.StripModulePrefix(node.Key)
	if err != nil {
		return nil, fmt.Errorf("strip module prefix for key %x: %w", node.Key, err)
	}

	if moduleName != keys.EVMStoreKey {
		ld, err := vtype.DeserializeLegacyData(node.Value)
		if err != nil {
			return nil, fmt.Errorf("deserialize LegacyData for key %x: %w", node.Key, err)
		}
		if ld.IsDelete() {
			return nil, nil
		}
		return []DecodedKV{{
			Module: moduleName,
			Key:    bytes.Clone(innerKey),
			Value:  bytes.Clone(ld.GetValue()),
		}}, nil
	}

	kind, strippedKey := keys.ParseEVMKey(innerKey)
	switch kind {
	case keys.EVMKeyNonce:
		acct, err := vtype.DeserializeAccountData(node.Value)
		if err != nil {
			return nil, fmt.Errorf("deserialize AccountData for key %x: %w", node.Key, err)
		}
		var out []DecodedKV
		if !acct.IsDelete() {
			nonceBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(nonceBuf, acct.GetNonce())
			out = append(out, DecodedKV{
				Module: keys.EVMStoreKey,
				Key:    keys.BuildEVMKey(keys.EVMKeyNonce, strippedKey),
				Value:  nonceBuf,
			})
		}
		if codeHash := acct.GetCodeHash(); *codeHash != (vtype.CodeHash{}) {
			out = append(out, DecodedKV{
				Module: keys.EVMStoreKey,
				Key:    keys.BuildEVMKey(keys.EVMKeyCodeHash, strippedKey),
				Value:  bytes.Clone(codeHash[:]),
			})
		}
		return out, nil

	case keys.EVMKeyStorage:
		sd, err := vtype.DeserializeStorageData(node.Value)
		if err != nil {
			return nil, fmt.Errorf("deserialize StorageData for key %x: %w", node.Key, err)
		}
		if sd.IsDelete() {
			return nil, nil
		}
		v := sd.GetValue()
		return []DecodedKV{{
			Module: keys.EVMStoreKey,
			Key:    bytes.Clone(innerKey),
			Value:  bytes.Clone(v[:]),
		}}, nil

	case keys.EVMKeyCode:
		cd, err := vtype.DeserializeCodeData(node.Value)
		if err != nil {
			return nil, fmt.Errorf("deserialize CodeData for key %x: %w", node.Key, err)
		}
		if cd.IsDelete() {
			return nil, nil
		}
		return []DecodedKV{{
			Module: keys.EVMStoreKey,
			Key:    bytes.Clone(innerKey),
			Value:  bytes.Clone(cd.GetBytecode()),
		}}, nil

	case keys.EVMKeyLegacy:
		ld, err := vtype.DeserializeLegacyData(node.Value)
		if err != nil {
			return nil, fmt.Errorf("deserialize LegacyData for key %x: %w", node.Key, err)
		}
		if ld.IsDelete() {
			return nil, nil
		}
		return []DecodedKV{{
			Module: keys.EVMStoreKey,
			Key:    bytes.Clone(innerKey),
			Value:  bytes.Clone(ld.GetValue()),
		}}, nil

	case keys.EVMKeyEmpty:
		return nil, nil

	default:
		return nil, fmt.Errorf("unexpected EVM key kind %v (key=%x)", kind, node.Key)
	}
}
