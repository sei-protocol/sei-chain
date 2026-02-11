package genesis

import (
	"embed"
	"fmt"
	"slices"

	"github.com/tendermint/tendermint/types"
)

//go:embed chains/*.json
var embeddedGenesisFS embed.FS

var wellKnownChainIDs = []string{"arctic-1", "atlantic-2", "pacific-1"}

func WellKnownChainIDs() []string {
	return append([]string(nil), wellKnownChainIDs...)
}

func IsWellKnown(chainID string) bool {
	return slices.Contains(wellKnownChainIDs, chainID)
}

func EmbeddedGenesis(chainID string) ([]byte, error) {
	if !IsWellKnown(chainID) {
		return nil, fmt.Errorf("unknown chain-id %q (well-known: %v)", chainID, wellKnownChainIDs)
	}
	filename := "chains/" + chainID + ".json"
	data, err := embeddedGenesisFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("reading embedded genesis for %s: %w", chainID, err)
	}
	return data, nil
}

func EmbeddedGenesisDoc(chainID string) (*types.GenesisDoc, error) {
	data, err := EmbeddedGenesis(chainID)
	if err != nil {
		return nil, err
	}
	genDoc, err := types.GenesisDocFromJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parsing embedded genesis for %s: %w", chainID, err)
	}
	return genDoc, nil
}
