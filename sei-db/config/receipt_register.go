package config

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// ReceiptStoreSectionName is this section's name in the configuration key space.
const ReceiptStoreSectionName = "receipt-store"

// Registration puts this section in the configuration registry.
//
// Two of the struct's fields carry the tag that excludes a field from configuration, so they declare no
// key: KeepRecent is derived from the global min-retain-blocks flag at the app layer, and ExternalPruning
// is set by whatever constructs the garbage collector. Declaring a key for either would put a default over
// the value that code assigns.
func init() {
	registry.RegisterSection(ReceiptStoreSectionName, &ReceiptStoreConfig{}, receiptStoreDefaults)
}

// receiptStoreDefaults is what this section resolves to for a node that has written nothing.
func receiptStoreDefaults(registry.Mode) any { return DefaultReceiptStoreConfig() }
