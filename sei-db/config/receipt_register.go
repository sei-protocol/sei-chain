package config

import (
	"github.com/sei-protocol/sei-chain/config/registry"
)

// ReceiptStoreSectionName is this section's name in the configuration key space.
const ReceiptStoreSectionName = "receipt-store"

// Registration puts this section in the configuration registry.
//
// Three of the struct's fields carry the tag that excludes a field from configuration, so they declare
// no key. KeepRecent is assigned from the global min-retain-blocks flag at the app layer, after this
// reader has returned, and ExternalPruning by whatever constructs the garbage collector. A key for
// either would be one an operator can write that the assignment then discards.
//
// EnableReadWriteMetrics is still read by ReadReceiptConfig so it can be set without appearing in
// app.toml. The reader also resolves the retired spelling of the backend, which it answers by
// refusing to start; declaring that one would offer a key whose only outcome is a stopped node.
func init() {
	registry.RegisterSection(ReceiptStoreSectionName, &ReceiptStoreConfig{}, receiptStoreDefaults)
}

// receiptStoreDefaults is what the seid init command writes for a node of this kind.
//
// The database directory resolves to an empty string, and the emptiness carries meaning rather than
// standing in for a path nobody chose. The app layer fills it only while it is empty, and what it fills
// it with depends on the host: a node that already holds the store at its former path keeps using that
// path, and any other node gets the current one. So a caller that renders this value into a file has to
// leave it empty. A path written there is one host's answer, and on a host whose store sits at the other
// path it names an empty directory.
func receiptStoreDefaults(registry.Mode) any { return DefaultReceiptStoreConfig() }
