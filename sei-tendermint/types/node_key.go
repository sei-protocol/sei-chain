package types

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/jsontypes"
	tmos "github.com/sei-protocol/sei-chain/sei-tendermint/libs/os"
)

//------------------------------------------------------------------------------
// Persistent peer ID
// TODO: encrypt on disk

// NodeKey is the persistent peer key.
// It contains the nodes private key for authentication.
type NodeKey crypto.PrivKey

type nodeKeyJSON struct {
	ID      NodeID          `json:"id"`
	PrivKey json.RawMessage `json:"priv_key"`
}

func (nk NodeKey) ID() NodeID { return NodeIDFromPubKey(nk.PubKey()) }

func (nk NodeKey) MarshalJSON() ([]byte, error) {
	pk, err := jsontypes.Marshal(crypto.PrivKey(nk))
	if err != nil {
		return nil, err
	}
	return json.Marshal(nodeKeyJSON{ID: nk.ID(), PrivKey: pk})
}

func (nk *NodeKey) UnmarshalJSON(data []byte) error {
	var nkjson nodeKeyJSON
	if err := json.Unmarshal(data, &nkjson); err != nil {
		return err
	}
	return jsontypes.Unmarshal(nkjson.PrivKey, (*crypto.PrivKey)(nk))
}

// PubKey returns the peer's PubKey
func (nk NodeKey) PubKey() crypto.PubKey {
	return crypto.PrivKey(nk).Public()
}

// SaveAs persists the NodeKey to filePath.
func (nk NodeKey) SaveAs(filePath string) error {
	jsonBytes, err := nk.MarshalJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, jsonBytes, 0600)
}

// LoadOrGenNodeKey attempts to load the NodeKey from the given filePath. If
// the file does not exist, it generates and saves a new NodeKey.
func LoadOrGenNodeKey(filePath string) (NodeKey, error) {
	if tmos.FileExists(filePath) {
		nodeKey, err := LoadNodeKey(filePath)
		if err != nil {
			return NodeKey{}, err
		}
		return nodeKey, nil
	}

	nodeKey := GenNodeKey()

	if err := nodeKey.SaveAs(filePath); err != nil {
		return NodeKey{}, err
	}

	return nodeKey, nil
}

// GenNodeKey generates a new node key.
func GenNodeKey() NodeKey {
	return NodeKey(ed25519.GenerateSecretKey())
}

// LoadNodeKey loads NodeKey located in filePath.
func LoadNodeKey(filePath string) (NodeKey, error) {
	jsonBytes, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return NodeKey{}, err
	}
	nodeKey := NodeKey{}
	err = json.Unmarshal(jsonBytes, &nodeKey)
	if err != nil {
		return NodeKey{}, err
	}
	return nodeKey, nil
}
