package genesis

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"

	"github.com/tendermint/tendermint/types"
)

//go:embed chains/*.json
var embeddedGenesisFS embed.FS

func WellKnownChainIDs() ([]string, error) {
	entries, err := fs.ReadDir(embeddedGenesisFS, "chains")
	if err != nil {
		return nil, fmt.Errorf("reading embedded chains dir: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if before, ok := strings.CutSuffix(name, ".json"); ok {
			ids = append(ids, before)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func IsWellKnown(chainID string) bool {
	ids, err := WellKnownChainIDs()
	if err != nil {
		return false
	}
	return slices.Contains(ids, chainID)
}

func EmbeddedGenesis(chainID string) ([]byte, error) {
	ids, err := WellKnownChainIDs()
	if err != nil {
		return nil, err
	}
	if !slices.Contains(ids, chainID) {
		return nil, fmt.Errorf("unknown chain-id %q (well-known: %v)", chainID, ids)
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
	if genDoc.ChainID != chainID {
		return nil, fmt.Errorf("embedded genesis %s.json has chain_id %q (expected %q)", chainID, genDoc.ChainID, chainID)
	}
	return genDoc, nil
}

// GenesisDocDigest returns the SHA-256 hash of genesis JSON after parsing into
// GenesisDoc and re-marshaling. Used to compare two genesis files for semantic equality
// (formatting and key order do not affect the digest).
func GenesisDocDigest(genesisJSON []byte) ([]byte, error) {
	genDoc, err := types.GenesisDocFromJSON(genesisJSON)
	if err != nil {
		return nil, fmt.Errorf("parsing genesis: %w", err)
	}
	ser, err := json.Marshal(genDoc)
	if err != nil {
		return nil, fmt.Errorf("marshaling genesis doc: %w", err)
	}
	hash := sha256.Sum256(ser)
	return hash[:], nil
}
