package state

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

type State struct {
	LastProcessedHeight int64   `json:"last_processed_height"`
	BlocksMissingTxs    []int64 `json:"blocks_missing_txs"`
}

// WriteState write the state to a JSON file.
func WriteState(dir string, s State) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}
	filename := filepath.Join(dir, "tx-scanner-state.json")
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	return err
}

// ReadState reads the state from a JSON file.
func ReadState(dir string) (State, error) {
	state := State{}
	filename := filepath.Join(dir, "tx-scanner-state.json")
	file, err := os.Open(filename)
	if err != nil {
		return State{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}
