package ibc

import (
	"embed"
	"strings"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
)

// The retired precompile still uses the ABI active at the traced height so
// historical method selectors are decoded before returning the retirement reason.
// The versions manifest is frozen with the module and remains the source of truth
// for every ABI that must be available after future upgrades.
//
//go:embed versions legacy/*/abi.json
var retiredAssets embed.FS

func GetVersioned(latestUpgrade string, keepers utils.Keepers) utils.VersionedPrecompiles {
	historicalVersions := getHistoricalVersions()
	versioned := make(utils.VersionedPrecompiles, len(historicalVersions)+1)
	for _, version := range historicalVersions {
		filename := "legacy/" + strings.ReplaceAll(version, ".", "") + "/abi.json"
		versioned[version] = newRetiredPrecompile(pcommon.MustGetABI(retiredAssets, filename), keepers)
	}
	versioned[latestUpgrade] = newRetiredPrecompile(pcommon.MustGetABI(currentABI, "abi.json"), keepers)
	return versioned
}

func getHistoricalVersions() []string {
	manifest, err := retiredAssets.ReadFile("versions")
	if err != nil {
		panic(err)
	}
	return strings.Fields(string(manifest))
}
