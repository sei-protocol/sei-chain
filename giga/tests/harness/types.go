package harness

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// StateTestTransaction represents a transaction in state test JSON
type StateTestTransaction struct {
	GasPrice             string                 `json:"gasPrice"`
	MaxFeePerGas         string                 `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string                 `json:"maxPriorityFeePerGas"`
	Nonce                string                 `json:"nonce"`
	To                   string                 `json:"to"`
	Data                 []string               `json:"data"`
	AccessLists          []*ethtypes.AccessList `json:"accessLists,omitempty"`
	GasLimit             []string               `json:"gasLimit"`
	Value                []string               `json:"value"`
	PrivateKey           hexutil.Bytes          `json:"secretKey"`
	Sender               common.Address         `json:"sender"`
}

// StateTestEnv represents the block environment in a state test
type StateTestEnv struct {
	Coinbase   common.Address `json:"currentCoinbase"`
	Difficulty string         `json:"currentDifficulty"`
	GasLimit   string         `json:"currentGasLimit"`
	Number     string         `json:"currentNumber"`
	Timestamp  string         `json:"currentTimestamp"`
	BaseFee    string         `json:"currentBaseFee"`
	Random     *common.Hash   `json:"currentRandom"`
}

// StateTestPost represents expected post-state for a subtest
type StateTestPost struct {
	Root            common.Hash           `json:"hash"`
	Logs            common.Hash           `json:"logs"`
	TxBytes         hexutil.Bytes         `json:"txbytes"`
	ExpectException string                `json:"expectException"`
	State           ethtypes.GenesisAlloc `json:"state"`
	Indexes         struct {
		Data  int `json:"data"`
		Gas   int `json:"gas"`
		Value int `json:"value"`
	} `json:"indexes"`
}

// StateTestJSON is the root structure of a state test file
type StateTestJSON struct {
	Pre         ethtypes.GenesisAlloc        `json:"pre"`
	Env         StateTestEnv                 `json:"env"`
	Transaction StateTestTransaction         `json:"transaction"`
	Post        map[string][]StateTestPost   `json:"post"`
}
