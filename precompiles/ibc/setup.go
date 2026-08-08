package ibc

import (
	"embed"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

// The retired precompile still uses the ABI active at the traced height so
// historical method selectors are decoded before returning the retirement
// reason. Several releases share the same ABI, but keeping the files at their
// archived paths makes the version mapping explicit and mechanically checkable.
//
//go:embed legacy/*/abi.json
var legacyABIs embed.FS

var legacyABIByVersion = map[string]string{
	"v5.5.2": "legacy/v552/abi.json",
	"v5.5.5": "legacy/v555/abi.json",
	"v5.6.2": "legacy/v562/abi.json",
	"v5.8.0": "legacy/v580/abi.json",
	"v6.0.1": "legacy/v601/abi.json",
	"v6.0.3": "legacy/v603/abi.json",
	"v6.0.5": "legacy/v605/abi.json",
	"v6.0.6": "legacy/v606/abi.json",
	"v6.1.0": "legacy/v610/abi.json",
	"v6.1.4": "legacy/v614/abi.json",
	"v6.2.0": "legacy/v620/abi.json",
	"v6.3.0": "legacy/v630/abi.json",
	"v6.4.0": "legacy/v640/abi.json",
	"v6.5":   "legacy/v65/abi.json",
}

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	versioned := make(utils.VersionedPrecompiles, len(legacyABIByVersion)+1)
	versioned[latestUpgrade] = newRetiredPrecompile(pcommon.MustGetABI(currentABI, "abi.json"), keepers)
	for version, filename := range legacyABIByVersion {
		versioned[version] = newRetiredPrecompile(pcommon.MustGetABI(legacyABIs, filename), keepers)
	}
	return versioned
}
