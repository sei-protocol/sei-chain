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
	Pre         ethtypes.GenesisAlloc      `json:"pre"`
	Env         StateTestEnv               `json:"env"`
	Transaction StateTestTransaction       `json:"transaction"`
	Post        map[string][]StateTestPost `json:"post"`
}

// ============================================================================
// BlockchainTests Types
// ============================================================================

// BlockchainTestJSON is the root structure of a blockchain test file
type BlockchainTestJSON struct {
	Info               BlockchainTestInfo    `json:"_info"`
	Blocks             []BlockchainTestBlock `json:"blocks"`
	Config             BlockchainTestConfig  `json:"config"`
	GenesisBlockHeader BlockHeader           `json:"genesisBlockHeader"`
	GenesisRLP         string                `json:"genesisRLP"`
	LastBlockHash      string                `json:"lastblockhash"`
	Network            string                `json:"network"`
	PostState          ethtypes.GenesisAlloc `json:"postState"`
	Pre                ethtypes.GenesisAlloc `json:"pre"`
	SealEngine         string                `json:"sealEngine"`
}

// BlockchainTestInfo contains metadata about the test
type BlockchainTestInfo struct {
	Comment            string `json:"comment"`
	FillingRPCServer   string `json:"filling-rpc-server"`
	FillingToolVersion string `json:"filling-tool-version"`
	FixtureFormat      string `json:"fixture-format"`
	Hash               string `json:"hash"`
	LLLCVersion        string `json:"lllcversion"`
	Repo               string `json:"repo"`
	Solidity           string `json:"solidity"`
	Source             string `json:"source"`
	SourceHash         string `json:"sourceHash"`
}

// BlockchainTestConfig contains chain configuration
type BlockchainTestConfig struct {
	ChainID      string `json:"chainid"`
	Network      string `json:"network"`
	BlobSchedule map[string]struct {
		BaseFeeUpdateFraction string `json:"baseFeeUpdateFraction"`
		Max                   string `json:"max"`
		Target                string `json:"target"`
	} `json:"blobSchedule,omitempty"`
}

// BlockHeader represents a full Ethereum block header
type BlockHeader struct {
	BaseFeePerGas         string `json:"baseFeePerGas"`
	BlobGasUsed           string `json:"blobGasUsed,omitempty"`
	Bloom                 string `json:"bloom"`
	Coinbase              string `json:"coinbase"`
	Difficulty            string `json:"difficulty"`
	ExcessBlobGas         string `json:"excessBlobGas,omitempty"`
	ExtraData             string `json:"extraData"`
	GasLimit              string `json:"gasLimit"`
	GasUsed               string `json:"gasUsed"`
	Hash                  string `json:"hash"`
	MixHash               string `json:"mixHash"`
	Nonce                 string `json:"nonce"`
	Number                string `json:"number"`
	ParentBeaconBlockRoot string `json:"parentBeaconBlockRoot,omitempty"`
	ParentHash            string `json:"parentHash"`
	ReceiptTrie           string `json:"receiptTrie"`
	RequestsHash          string `json:"requestsHash,omitempty"`
	StateRoot             string `json:"stateRoot"`
	Timestamp             string `json:"timestamp"`
	TransactionsTrie      string `json:"transactionsTrie"`
	UncleHash             string `json:"uncleHash"`
	WithdrawalsRoot       string `json:"withdrawalsRoot,omitempty"`
}

// BlockchainTestBlock represents a block in blockchain tests
type BlockchainTestBlock struct {
	BlockHeader  BlockHeader                 `json:"blockHeader"`
	BlockNumber  string                      `json:"blocknumber"`
	ChainName    string                      `json:"chainname"`
	RLP          string                      `json:"rlp"`
	Transactions []BlockchainTestTransaction `json:"transactions"`
	UncleHeaders []BlockHeader               `json:"uncleHeaders"`
	Withdrawals  []BlockchainTestWithdrawal  `json:"withdrawals,omitempty"`
}

// BlockchainTestTransaction represents a pre-signed transaction in blockchain tests
type BlockchainTestTransaction struct {
	Data                 string `json:"data"`
	GasLimit             string `json:"gasLimit"`
	GasPrice             string `json:"gasPrice,omitempty"`
	MaxFeePerGas         string `json:"maxFeePerGas,omitempty"`
	MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas,omitempty"`
	Nonce                string `json:"nonce"`
	R                    string `json:"r"`
	S                    string `json:"s"`
	Sender               string `json:"sender"`
	To                   string `json:"to"`
	V                    string `json:"v"`
	Value                string `json:"value"`
	// Access list for EIP-2930/EIP-1559 transactions
	AccessList *ethtypes.AccessList `json:"accessList,omitempty"`
}

// BlockchainTestWithdrawal represents a validator withdrawal (Shanghai+)
type BlockchainTestWithdrawal struct {
	Address        string `json:"address"`
	Amount         string `json:"amount"`
	Index          string `json:"index"`
	ValidatorIndex string `json:"validatorIndex"`
}
