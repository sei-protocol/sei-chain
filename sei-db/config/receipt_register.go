package config

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// ReceiptStoreSectionName is this section's name in the configuration key space.
const ReceiptStoreSectionName = "receipt-store"

// Registration puts this section in the configuration registry.
//
// The owning package registers its own section, so the struct, the defaults and the keys all come
// from one place and cannot drift apart. Two of the struct's fields carry no key: KeepRecent and
// ExternalPruning are set by the code that constructs the store rather than read from
// configuration, and mapstructure's dash is what says so.
func init() {
	registry.RegisterSection(ReceiptStoreSectionName, &ReceiptStoreConfig{}, receiptStoreBaseline)
}

// receiptStoreBaseline is what this section resolves to for a node that has written nothing.
//
// The same values for every mode. How receipts are stored is an operator's choice about disk and
// query load, and no node mode implies one.
func receiptStoreBaseline(registry.Mode) any { return DefaultReceiptStoreConfig() }
