package evmonlyapp

import (
	"encoding/binary"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
)

// EpochRegistryRowForTesting returns the module, key and value of the FlatKV row that holds
// registry. Intended for use in tests only.
func EpochRegistryRowForTesting(registry *atypes.EpochRegistry) (string, []byte, []byte) {
	return epochRegistryModule, []byte(epochRegistryKey), epoch.Encode(registry)
}

// CursorRowForTesting returns the module, key and value of the cursor row that the application
// commits with block height. Intended for use in tests only.
func CursorRowForTesting(height int64) (string, []byte, []byte) {
	return evmOnlyCursorModule, []byte(evmOnlyCursorKey), evmOnlyCursor{height: height}.encode()
}

// ChainIDRowForTesting returns the module, key and value of the chain ID row that the application
// commits with every block on chain chainID. Intended for use in tests only.
func ChainIDRowForTesting(chainID uint64) (string, []byte, []byte) {
	return evmOnlyCursorModule, []byte(evmOnlyChainIDKey), binary.BigEndian.AppendUint64(nil, chainID)
}
