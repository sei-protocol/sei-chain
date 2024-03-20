package cmd

import (
	"github.com/ethereum/go-ethereum/common"
	ethtests "github.com/ethereum/go-ethereum/tests"
)

var emptyBlockTest = &ethtests.BlockTest{
	Json: ethtests.BtJSON{
		Blocks:     nil,
		Genesis:    emptyBtHeader,           // BtHeader              `json:"genesisBlockHeader"`
		Pre:        nil,                     // core.GenesisAlloc     `json:"pre"`
		Post:       nil,                     // core.GenesisAlloc     `json:"postState"`
		BestBlock:  common.UnprefixedHash{}, // common.UnprefixedHash `json:"lastblockhash"`
		Network:    "Shanghai",              // string                `json:"network"`
		SealEngine: "",                      // string                `json:"sealEngine"`
	},
}

var emptyBtHeader = ethtests.BtHeader{}

// type BtHeader struct {
// 	Bloom                 types.Bloom
// 	Coinbase              common.Address
// 	MixHash               common.Hash
// 	Nonce                 types.BlockNonce
// 	Number                *big.Int
// 	Hash                  common.Hash
// 	ParentHash            common.Hash
// 	ReceiptTrie           common.Hash
// 	StateRoot             common.Hash
// 	TransactionsTrie      common.Hash
// 	UncleHash             common.Hash
// 	ExtraData             []byte
// 	Difficulty            *big.Int
// 	GasLimit              uint64
// 	GasUsed               uint64
// 	Timestamp             uint64
// 	BaseFeePerGas         *big.Int
// 	WithdrawalsRoot       *common.Hash
// 	BlobGasUsed           *uint64
// 	ExcessBlobGas         *uint64
// 	ParentBeaconBlockRoot *common.Hash
// }
